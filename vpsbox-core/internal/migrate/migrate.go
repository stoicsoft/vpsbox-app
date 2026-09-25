// Package migrate moves the *work* on one machine to another: the packages
// that were installed, the app and config files that were written, the
// services that were enabled, and the docker volumes and compose projects that
// hold running apps. It deliberately does not clone the machine itself —
// kernels, bootloaders, network configuration, SSH server settings, and
// host keys stay put, because copying those between a local VM and a real VPS
// is how you lose access to one of them.
//
// The package is pure: it builds shell scripts and parses their output, and it
// decides what should move. Running the scripts over SSH is the caller's job
// (internal/app), which keeps everything here unit-testable without a VM.
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Target is a real machine reachable over SSH — the destination of a push or
// the source of an import.
type Target struct {
	User    string
	Host    string
	Port    int
	KeyPath string
}

// ParseTarget reads a "user@host" spec. The port and key arrive as separate
// flags — a ":port" suffix is ambiguous once IPv6 addresses show up.
func ParseTarget(spec string) (Target, error) {
	spec = strings.TrimSpace(spec)
	user, host, found := strings.Cut(spec, "@")
	if !found || user == "" || host == "" {
		return Target{}, fmt.Errorf("target must look like user@host (got %q)", spec)
	}
	if strings.ContainsAny(spec, " \t\n'\"") {
		return Target{}, fmt.Errorf("target %q contains characters that cannot be part of an address", spec)
	}
	return Target{User: user, Host: host, Port: 22}, nil
}

// Label is how the machine is named in progress messages and plans.
func (t Target) Label() string {
	return t.User + "@" + t.Host
}

// OSInfo is the /etc/os-release identity of a machine.
type OSInfo struct {
	ID         string // "ubuntu", "debian"
	VersionID  string // "24.04"
	PrettyName string
}

// User is a human account (uid >= 1000) that owns files under /home.
type User struct {
	Name string
	UID  int
}

// PathInfo is one candidate path that exists on the source, with its size so
// the plan can say how much data will move. Rel is relative to / — that is the
// form tar wants.
type PathInfo struct {
	Rel    string
	SizeKB int64
}

func (p PathInfo) Abs() string { return "/" + p.Rel }

// VolumeInfo is one docker volume and its size.
type VolumeInfo struct {
	Name   string
	SizeKB int64
}

// ComposeProject is one docker compose project known to the source.
type ComposeProject struct {
	Name        string
	ConfigFiles []string
	Running     bool
}

// Resources is what the machine has and uses, for sizing an import sandbox.
type Resources struct {
	CPUs       int
	MemoryMB   int
	UsedDiskKB int64
}

// Manifest is everything captured from one machine in a single SSH round trip.
type Manifest struct {
	Privilege  string // "root", "sudo", or "none"
	OS         OSInfo
	Resources  Resources
	Users      []User
	Packages   []string // apt-mark showmanual — what a human asked for
	Installed  []string // every installed package — what the destination already has
	Enabled    []string // enabled systemd services
	Paths      []PathInfo
	Volumes    []VolumeInfo
	Compose    []ComposeProject
	Containers []string // running container names, for the "will not restart" warning
}

// Supported rejects machines this engine cannot migrate. Everything downstream
// assumes apt and systemd.
func (m *Manifest) Supported() error {
	switch m.OS.ID {
	case "ubuntu", "debian":
	default:
		name := m.OS.PrettyName
		if name == "" {
			name = "an unknown OS"
		}
		return fmt.Errorf("%s is not supported — vpsbox can only migrate apt-based systems (Ubuntu or Debian)", name)
	}
	if m.Privilege == "none" {
		return errors.New("the SSH user is not root and cannot use passwordless sudo — migration needs one or the other")
	}
	return nil
}

// candidatePaths is the curated set of places where deployed work lives.
// aptPaths must be synced (and apt updated) before packages install, so
// packages from third-party repositories — docker-ce above all — resolve.
var aptPaths = []string{
	"etc/apt/sources.list.d",
	"etc/apt/keyrings",
	"usr/share/keyrings",
}

var dataPaths = []string{
	"root",
	"home",
	"opt",
	"srv",
	"var/www",
	"usr/local",
	"etc/systemd/system",
	"etc/nginx",
	"etc/caddy",
	"etc/apache2",
	"etc/letsencrypt",
	"etc/cron.d",
	"var/spool/cron/crontabs",
}

// tarExcludes are never copied, in either direction. The first group would
// lock someone out or leak credentials; the second is the machine's identity;
// the third is noise that bloats the stream. `etc/systemd/system/*.wants`
// stays home because enablement symlinks travelling by tar would silently
// enable services — enabling is done deliberately, unit by unit, in
// ServiceScript. The distro's own archive definitions (ubuntu.sources /
// debian.sources live *inside* sources.list.d since 24.04) stay home because
// an Apple Silicon sandbox's file points at ports.ubuntu.com and a different
// release — shipping it would cut an Intel VPS off from its entire archive.
var tarExcludes = []string{
	"*/.ssh",
	"etc/apt/sources.list.d/ubuntu.sources",
	"etc/apt/sources.list.d/debian.sources",
	"etc/systemd/system/*.wants",
	"root/.cache",
	"home/*/.cache",
	"root/snap",
	"home/*/snap",
}

// CandidatePaths returns every path the capture script measures.
func CandidatePaths() []string {
	return append(append([]string{}, aptPaths...), dataPaths...)
}

// SplitPlanPaths separates the apt repository configuration from everything
// else. The apply engine must copy the apt group and run the package install
// before the data group ships — packages from third-party repositories cannot
// resolve until their sources exist on the destination.
func SplitPlanPaths(paths []PathInfo) (apt, data []PathInfo) {
	aptSet := toSet(aptPaths)
	for _, path := range paths {
		if aptSet[path.Rel] {
			apt = append(apt, path)
		} else {
			data = append(data, path)
		}
	}
	return apt, data
}

// skipPackagePrefixes are packages that describe the machine, not the work —
// installing a kernel or bootloader on the destination is at best pointless.
var skipPackagePrefixes = []string{
	"linux-",
	"grub",
	"shim",
	"os-prober",
	"cloud-init",
	"snapd",
	"ubuntu-", // ubuntu-minimal/-server/-standard/-pro-*: distro meta, not work
}

// skipServicePrefixes are services that belong to the machine rather than the
// work: init plumbing, guest agents, and anything whose remote restart could
// cut the connection doing the restarting.
var skipServicePrefixes = []string{
	"ssh",
	"systemd-",
	"cloud-",
	"snap",
	"getty",
	"serial-getty",
	"multipass",
	"qemu-guest",
	"open-vm-tools",
	"walinuxagent",
	"hv-",
	"iscsi",
	"lvm2",
	"multipathd",
	"apparmor",
	"ufw",
	"netplan",
	"networkd-dispatcher",
	"unattended-upgrades",
	"ModemManager",
	"grub",
	"udisks",
}

// Plan is what a migration will do, computed from the two manifests. It is
// shown to the user before anything is touched.
type Plan struct {
	Source string
	Dest   string

	Packages []string // installed on the destination
	Services []string // enabled and restarted on the destination
	Users    []User   // created on the destination if missing
	Paths    []PathInfo
	Volumes  []VolumeInfo
	Compose  []ComposeProject
	Notes    []string // everything that will NOT move, said out loud
}

// EstimatedKB is the data volume of the copy, for progress messages and for
// sizing an import sandbox.
func (p *Plan) EstimatedKB() int64 {
	var total int64
	for _, path := range p.Paths {
		total += path.SizeKB
	}
	for _, volume := range p.Volumes {
		total += volume.SizeKB
	}
	return total
}

// BuildPlan decides what moves from src to dst. Both manifests are needed:
// the destination's installed set keeps the package list to the real delta,
// and its enabled services keep re-enablement from fighting choices the
// destination's admin already made.
func BuildPlan(src, dst *Manifest, srcLabel, dstLabel string) *Plan {
	plan := &Plan{Source: srcLabel, Dest: dstLabel}

	installed := toSet(dst.Installed)
	var skippedPackages []string
	for _, pkg := range src.Packages {
		if installed[pkg] {
			continue
		}
		if hasAnyPrefix(pkg, skipPackagePrefixes) {
			skippedPackages = append(skippedPackages, pkg)
			continue
		}
		plan.Packages = append(plan.Packages, pkg)
	}

	enabled := toSet(dst.Enabled)
	for _, unit := range src.Enabled {
		if enabled[unit] || hasAnyPrefix(unit, skipServicePrefixes) {
			continue
		}
		plan.Services = append(plan.Services, unit)
	}

	dstUsers := map[string]bool{}
	for _, user := range dst.Users {
		dstUsers[user.Name] = true
	}
	for _, user := range src.Users {
		if !dstUsers[user.Name] {
			plan.Users = append(plan.Users, user)
		}
	}

	plan.Paths = append(plan.Paths, src.Paths...)
	plan.Volumes = append(plan.Volumes, src.Volumes...)
	plan.Compose = append(plan.Compose, src.Compose...)

	plan.Notes = append(plan.Notes,
		"SSH server settings, host keys, and .ssh directories never move — you cannot be locked out by a migration",
		"network, firewall, hostname, and bootloader configuration stay on each machine",
	)
	if len(skippedPackages) > 0 {
		plan.Notes = append(plan.Notes, "machine-level packages skipped: "+strings.Join(skippedPackages, ", "))
	}
	if len(plan.Volumes) > 0 || len(plan.Compose) > 0 {
		plan.Notes = append(plan.Notes, "docker images are pulled on the destination, not copied")
	}
	for _, name := range nonComposeContainers(src.Containers, src.Compose) {
		plan.Notes = append(plan.Notes, fmt.Sprintf("container %q is not part of a compose project — its volume data moves, but it will not be restarted on the destination", name))
	}
	if src.OS.ID != dst.OS.ID || src.OS.VersionID != dst.OS.VersionID {
		plan.Notes = append(plan.Notes, fmt.Sprintf("source runs %s, destination runs %s — a package available on one may not exist on the other", pretty(src.OS), pretty(dst.OS)))
	}
	return plan
}

func pretty(os OSInfo) string {
	if os.PrettyName != "" {
		return os.PrettyName
	}
	return strings.TrimSpace(os.ID + " " + os.VersionID)
}

// nonComposeContainers reports running containers that no compose project
// claims. Compose names its containers <project>-<service>-<n> (older
// versions used underscores).
func nonComposeContainers(containers []string, projects []ComposeProject) []string {
	var orphans []string
	for _, name := range containers {
		claimed := false
		for _, project := range projects {
			if strings.HasPrefix(name, project.Name+"-") || strings.HasPrefix(name, project.Name+"_") {
				claimed = true
				break
			}
		}
		if !claimed {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	return orphans
}

// SandboxSize suggests dimensions for a sandbox that will hold an imported
// VPS: enough to run what ran there, clamped so a big VPS cannot flatten the
// laptop, with disk sized from what will actually be copied rather than the
// VPS's disk.
func SandboxSize(res Resources, estimatedKB int64) (cpus, memoryGB, diskGB int) {
	cpus = clamp(res.CPUs, 2, 4)
	memoryGB = clamp((res.MemoryMB+1023)/1024, 2, 8)
	dataGB := int((estimatedKB*2)/(1024*1024)) + 1
	diskGB = clamp(6+dataGB, 10, 200)
	return cpus, memoryGB, diskGB
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ImageForOS picks the Multipass Ubuntu image closest to the source machine.
// Multipass only ships Ubuntu, so a Debian source gets the current LTS and the
// plan carries a note about the difference.
func ImageForOS(os OSInfo) string {
	if os.ID == "ubuntu" {
		switch os.VersionID {
		case "20.04", "22.04", "24.04", "26.04":
			return os.VersionID
		}
	}
	return "24.04"
}

// ParseManifest reads CaptureScript's fenced output.
func ParseManifest(out string) (*Manifest, error) {
	m := &Manifest{}
	section := ""
	var composeJSON []string
	sawEnd := false
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "@@") {
			section = strings.TrimPrefix(line, "@@")
			if section == "end" {
				sawEnd = true
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		switch section {
		case "privilege":
			m.Privilege = strings.TrimSpace(line)
		case "os":
			parts := strings.SplitN(line, "|", 3)
			if len(parts) == 3 {
				m.OS = OSInfo{ID: parts[0], VersionID: parts[1], PrettyName: parts[2]}
			}
		case "resources":
			key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
			switch key {
			case "cpus":
				m.Resources.CPUs, _ = strconv.Atoi(strings.TrimSpace(value))
			case "memmb":
				m.Resources.MemoryMB, _ = strconv.Atoi(strings.TrimSpace(value))
			case "diskkb":
				m.Resources.UsedDiskKB, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			}
		case "users":
			uid, name, found := strings.Cut(strings.TrimSpace(line), " ")
			if found {
				if id, err := strconv.Atoi(uid); err == nil {
					m.Users = append(m.Users, User{Name: name, UID: id})
				}
			}
		case "packages":
			m.Packages = append(m.Packages, strings.TrimSpace(line))
		case "installed":
			m.Installed = append(m.Installed, strings.TrimSpace(line))
		case "services":
			m.Enabled = append(m.Enabled, strings.TrimSpace(line))
		case "paths":
			size, rel, found := strings.Cut(strings.TrimSpace(line), " ")
			if found {
				kb, _ := strconv.ParseInt(size, 10, 64)
				m.Paths = append(m.Paths, PathInfo{Rel: rel, SizeKB: kb})
			}
		case "volumes":
			size, name, found := strings.Cut(strings.TrimSpace(line), " ")
			if found && validVolumeName(name) {
				kb, _ := strconv.ParseInt(size, 10, 64)
				m.Volumes = append(m.Volumes, VolumeInfo{Name: name, SizeKB: kb})
			}
		case "compose":
			composeJSON = append(composeJSON, line)
		case "containers":
			m.Containers = append(m.Containers, strings.TrimSpace(line))
		}
	}
	if !sawEnd {
		return nil, errors.New("the capture script did not finish — the machine may not be a standard Ubuntu/Debian system")
	}
	m.Compose = parseComposeList(strings.Join(composeJSON, "\n"))
	return m, nil
}

// composeListEntry mirrors `docker compose ls --format json` output.
type composeListEntry struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`
	ConfigFiles string `json:"ConfigFiles"`
}

// parseComposeList accepts both shapes docker compose has used: a JSON array,
// and one JSON object per line.
func parseComposeList(raw string) []ComposeProject {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var entries []composeListEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		for _, line := range strings.Split(raw, "\n") {
			var entry composeListEntry
			if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &entry); err == nil {
				entries = append(entries, entry)
			}
		}
	}
	var projects []ComposeProject
	for _, entry := range entries {
		if entry.Name == "" || entry.ConfigFiles == "" {
			continue
		}
		var files []string
		for _, file := range strings.Split(entry.ConfigFiles, ",") {
			if file = strings.TrimSpace(file); file != "" {
				files = append(files, file)
			}
		}
		if len(files) == 0 {
			continue
		}
		projects = append(projects, ComposeProject{
			Name:        entry.Name,
			ConfigFiles: files,
			Running:     strings.HasPrefix(strings.ToLower(entry.Status), "running"),
		})
	}
	return projects
}

var volumeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

func validVolumeName(name string) bool {
	return volumeNamePattern.MatchString(name)
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
