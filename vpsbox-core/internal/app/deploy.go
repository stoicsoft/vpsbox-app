package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stoicsoft/vpsbox/internal/registry"
	"github.com/stoicsoft/vpsbox/internal/templates"
)

// deployedMarkerDir is where a successful install records what it installed, so
// the sandbox can be asked later what is supposed to be running on it. It lives
// in the sandbox user's home (no sudo needed) and rolls back with a snapshot
// restore, which is the honest answer: restored box, app gone.
const deployedMarkerDir = "$HOME/.vpsbox/deployed"

// LiveApp is one app currently published by the sandbox: a template we
// installed, or any other container publishing a port (an app deployed through
// Coolify or Dokploy, for instance).
type LiveApp struct {
	TemplateID string // empty for apps we did not install ourselves
	Name       string
	Icon       string
	Port       int // the external port on the sandbox host
	URL        string
	Running    bool   // something is listening on Port, on a non-loopback address
	Container  string // docker container publishing the port, when there is one
}

// Deploy installs a curated app template inside the named sandbox by SSHing in
// and running the template's install script. status receives coarse progress
// messages, output receives the installer's own lines as they arrive; either
// may be nil.
func (m *Manager) Deploy(ctx context.Context, name, templateID string, status, output func(string)) error {
	template, ok := templates.Templates[templateID]
	if !ok {
		return fmt.Errorf("unknown template %q (try `vpsbox deploy --list`)", templateID)
	}
	instance, err := m.requireRunningInstance(ctx, name)
	if err != nil {
		return err
	}
	if status != nil {
		status(fmt.Sprintf("Installing %s on %s (this may take a minute)…", template.Name, instance.Name))
	}
	script := strings.TrimRight(template.Install, "\n") + "\n" + markerScript(template.ID)
	tail, err := m.streamRemoteOn(ctx, instance, script, output)
	if err != nil {
		return fmt.Errorf("install failed: %w\n%s", err, tail)
	}
	if status != nil {
		status(fmt.Sprintf("%s is up on :%d", template.Name, template.Port))
	}
	return nil
}

// markerScript records a finished install. It runs after the template script,
// so `set -e` means it only lands when the install actually succeeded.
func markerScript(id string) string {
	return "mkdir -p \"" + deployedMarkerDir + "\"\n" +
		": > \"" + deployedMarkerDir + "/" + id + "\"\n"
}

// liveAppsScript collects the three things that answer "what is published here":
// the install markers, the listening TCP sockets, and the docker containers with
// published ports. Sections are fenced so the output parses deterministically.
const liveAppsScript = `echo "@@markers"
ls -1 "` + deployedMarkerDir + `" 2>/dev/null || true
echo "@@ports"
ss -ltn 2>/dev/null || true
echo "@@containers"
sudo docker ps --format '{{.Names}}|{{.Ports}}' 2>/dev/null || true
`

// LiveApps reports the apps reachable on the sandbox right now: everything
// `vpsbox deploy` installed, plus any other container publishing a port.
func (m *Manager) LiveApps(ctx context.Context, name string) ([]LiveApp, error) {
	instance, err := m.requireRunningInstance(ctx, name)
	if err != nil {
		return nil, err
	}
	stdout, stderr, err := m.runRemoteOn(ctx, instance, liveAppsScript)
	if err != nil {
		return nil, fmt.Errorf("inspect sandbox: %w\n%s", err, strings.TrimSpace(stderr))
	}
	return parseLiveApps(stdout, appHost(instance)), nil
}

func appHost(instance *registry.Instance) string {
	if instance.Host != "" {
		return instance.Host
	}
	return instance.Hostname
}

// parseLiveApps turns the liveAppsScript output into the app list. Kept pure so
// the port/container parsing is testable without a sandbox.
func parseLiveApps(out, host string) []LiveApp {
	var (
		section    string
		markers    = map[string]bool{}
		listening  = map[int]bool{}
		containers = map[int]string{}
	)
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			section = strings.TrimPrefix(line, "@@")
			continue
		}
		switch section {
		case "markers":
			markers[line] = true
		case "ports":
			if port, ok := listeningPort(line); ok {
				listening[port] = true
			}
		case "containers":
			name, ports, found := strings.Cut(line, "|")
			if !found {
				continue
			}
			for _, port := range publishedPorts(ports) {
				if _, taken := containers[port]; !taken {
					containers[port] = name
				}
			}
		}
	}

	apps := make([]LiveApp, 0, len(markers)+len(containers))
	claimed := map[int]bool{}
	// Installed templates first, in catalog order, so the list reads the same
	// way the Deploy catalog does.
	for _, template := range templates.List() {
		if !markers[template.ID] {
			continue
		}
		claimed[template.Port] = true
		apps = append(apps, LiveApp{
			TemplateID: template.ID,
			Name:       template.Name,
			Icon:       template.Icon,
			Port:       template.Port,
			URL:        appURL(host, template.Port),
			Running:    listening[template.Port],
			Container:  containers[template.Port],
		})
	}
	// Then anything else publishing a port — apps deployed through a platform
	// we installed, which we could not have known about up front.
	extra := make([]int, 0, len(containers))
	for port := range containers {
		if !claimed[port] {
			extra = append(extra, port)
		}
	}
	sort.Ints(extra)
	for _, port := range extra {
		apps = append(apps, LiveApp{
			Name:      containers[port],
			Icon:      "📦",
			Port:      port,
			URL:       appURL(host, port),
			Running:   listening[port],
			Container: containers[port],
		})
	}
	return apps
}

func appURL(host string, port int) string {
	if host == "" {
		return ""
	}
	if port == 443 {
		return fmt.Sprintf("https://%s", host)
	}
	if port == 80 {
		return fmt.Sprintf("http://%s", host)
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// listeningPort pulls the port out of one `ss -ltn` row, skipping the header
// and anything bound to loopback only — those are not reachable from the host.
func listeningPort(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, false
	}
	return hostPort(fields[3])
}

// publishedPorts pulls the host-side ports out of a `docker ps` ports column,
// e.g. "0.0.0.0:8086->80/tcp, [::]:8086->80/tcp, 3306/tcp".
func publishedPorts(column string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, mapping := range strings.Split(column, ",") {
		hostSide, _, found := strings.Cut(strings.TrimSpace(mapping), "->")
		if !found {
			continue // container-only port, not published
		}
		port, ok := hostPort(hostSide)
		if !ok || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

// hostPort splits an "address:port" pair and rejects loopback binds. Addresses
// arrive as 0.0.0.0:80, *:80, [::]:80, 127.0.0.1:80 or [::1]:80.
func hostPort(addr string) (int, bool) {
	index := strings.LastIndex(addr, ":")
	if index < 0 {
		return 0, false
	}
	host := strings.Trim(addr[:index], "[]")
	if host == "127.0.0.1" || strings.HasPrefix(host, "127.") || host == "::1" {
		return 0, false
	}
	port, err := strconv.Atoi(strings.TrimSpace(addr[index+1:]))
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

func newDeployCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var list bool
	var apps bool
	cmd := &cobra.Command{
		Use:   "deploy [template] [name]",
		Short: "Install a starter app inside the sandbox (e.g. nodejs-hello, static-html, wordpress)",
		Long: "Curated, version-pinned installers for common starter apps. The first time you\n" +
			"see something work on your sandbox is the fastest way to learn what a deploy\n" +
			"actually looks like. Run `vpsbox deploy --list` to see what's available, or\n" +
			"`vpsbox deploy --apps` to see what is already live on a sandbox.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if apps {
				// With --apps the first argument is the sandbox, not a template.
				name := ""
				if len(args) > 0 {
					name = args[0]
				}
				live, err := manager.LiveApps(ctx, name)
				if err != nil {
					return err
				}
				printLiveApps(live)
				return nil
			}
			if list || len(args) == 0 {
				printTemplateList()
				if !list {
					fmt.Println()
					fmt.Println("Try: vpsbox deploy nodejs-hello")
				}
				return nil
			}
			templateID := args[0]
			instanceName := ""
			if len(args) > 1 {
				instanceName = args[1]
			}
			err := manager.Deploy(ctx, instanceName, templateID,
				func(s string) { fmt.Println("  " + s) },
				func(s string) { fmt.Println("  │ " + s) },
			)
			if err != nil {
				return err
			}
			instance, err := manager.requireInstance(instanceName)
			if err != nil {
				return err
			}
			tpl := templates.Templates[templateID]
			fmt.Printf("\n✓ %s is running on %s\n\n", templateID, instance.Name)
			fmt.Printf("  Open in your browser:  http://%s:%d\n", instance.Host, tpl.Port)
			if instance.Hostname != "" {
				fmt.Printf("  Or:                    http://%s:%d\n", instance.Hostname, tpl.Port)
			}
			fmt.Println()
			// Best effort: the deploy already succeeded, so a failure to
			// re-inspect the sandbox is not worth failing the command over.
			if live, err := manager.LiveApps(ctx, instanceName); err == nil {
				printLiveApps(live)
				fmt.Println()
			}
			fmt.Println("Roll back the install with: vpsbox undo")
			return nil
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "list available templates and exit")
	cmd.Flags().BoolVar(&apps, "apps", false, "list the apps currently live on the sandbox and exit")
	return cmd
}

func printLiveApps(apps []LiveApp) {
	fmt.Println("Live apps:")
	fmt.Println()
	if len(apps) == 0 {
		fmt.Println("  Nothing published yet — install one with `vpsbox deploy <template>`.")
		return
	}
	fmt.Printf("  %-18s %-8s %-9s %s\n", "APP", "PORT", "STATUS", "URL")
	for _, app := range apps {
		status := "stopped"
		if app.Running {
			status = "running"
		}
		fmt.Printf("  %-18s %-8d %-9s %s\n", truncate(app.Name, 18), app.Port, status, app.URL)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

func printTemplateList() {
	var platform, apps []templates.Template
	for _, t := range templates.List() {
		if t.Category == templates.CategoryPlatform {
			platform = append(platform, t)
		} else {
			apps = append(apps, t)
		}
	}
	printTemplateGroup("Deploy platforms:", platform)
	fmt.Println()
	printTemplateGroup("Apps & tools:", apps)
}

func printTemplateGroup(heading string, ts []templates.Template) {
	fmt.Println(heading)
	fmt.Println()
	for _, t := range ts {
		fmt.Printf("  %-14s %s\n", t.ID, t.Summary)
	}
}
