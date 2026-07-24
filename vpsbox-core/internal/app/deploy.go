package app

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stoicsoft/vpsbox/internal/templates"
)

// Deploy installs a curated app template inside the named sandbox by SSHing
// in and running the template's install script.
func (m *Manager) Deploy(ctx context.Context, name, templateID string, progress func(string)) error {
	template, ok := templates.Templates[templateID]
	if !ok {
		return fmt.Errorf("unknown template %q (try `vpsbox deploy --list`)", templateID)
	}
	instance, err := m.requireRunningInstance(ctx, name)
	if err != nil {
		return err
	}
	if progress != nil {
		progress(fmt.Sprintf("Installing %s on %s (this may take a minute)…", template.Name, instance.Name))
	}
	stdout, stderr, err := m.runRemoteOn(ctx, instance, template.Install)
	if err != nil {
		return fmt.Errorf("install failed: %w\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	return nil
}

func newDeployCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:   "deploy [template] [name]",
		Short: "Install a starter app inside the sandbox (e.g. nodejs-hello, static-html, wordpress)",
		Long: "Curated, version-pinned installers for common starter apps. The first time you\n" +
			"see something work on your sandbox is the fastest way to learn what a deploy\n" +
			"actually looks like. Run `vpsbox deploy --list` to see what's available.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if err := manager.Deploy(ctx, instanceName, templateID, func(s string) { fmt.Println("  " + s) }); err != nil {
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
			fmt.Println("Roll back the install with: vpsbox undo")
			return nil
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "list available templates and exit")
	return cmd
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
