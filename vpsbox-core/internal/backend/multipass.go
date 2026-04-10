package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/executil"
)

type Multipass struct{}

const (
	multipassWaitReadyTimeoutSeconds = 60
	multipassLaunchTimeoutSeconds    = 1200
	multipassStartTimeoutSeconds     = 600
)

func NewMultipass() *Multipass {
	return &Multipass{}
}

func (m *Multipass) Name() string {
	return "multipass"
}

func (m *Multipass) Priority() int {
	return 10
}

func (m *Multipass) Available(context.Context) (bool, error) {
	return executil.LookPath("multipass"), nil
}

func (m *Multipass) EnsureInstalled(ctx context.Context) error {
	ok, err := m.Available(ctx)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	switch runtime.GOOS {
	case "darwin":
		if executil.LookPath("brew") {
			_, err := executil.Run(ctx, "brew", "install", "--cask", "multipass")
			return err
		}
	case "linux":
		// snap is the official Linux distribution channel for multipass.
		if executil.LookPath("snap") {
			_, err := executil.Run(ctx, "sudo", "snap", "install", "multipass", "--classic")
			return err
		}
		if executil.LookPath("brew") {
			_, err := executil.Run(ctx, "brew", "install", "multipass")
			return err
		}
		return fmt.Errorf("multipass is not installed; install snapd then run `sudo snap install multipass --classic`")
	case "windows":
		if executil.LookPath("winget") {
			_, err := executil.Run(ctx, "winget", "install", "--id", "Canonical.Multipass", "-e", "--accept-source-agreements", "--accept-package-agreements")
			return err
		}
	}

	return fmt.Errorf("multipass is not installed; install it first for %s", runtime.GOOS)
}

func (m *Multipass) Create(ctx context.Context, req CreateRequest) error {
	_, _ = executil.Run(ctx, "multipass", "wait-ready", "--timeout", fmt.Sprintf("%d", multipassWaitReadyTimeoutSeconds))

	args := []string{
		"launch",
		req.Image,
		"--name", req.Name,
		"--cpus", fmt.Sprintf("%d", req.CPUs),
		"--memory", fmt.Sprintf("%dG", req.MemoryGB),
		"--disk", fmt.Sprintf("%dG", req.DiskGB),
		"--timeout", fmt.Sprintf("%d", multipassLaunchTimeoutSeconds),
	}
	if req.CloudInitPath != "" {
		args = append(args, "--cloud-init", req.CloudInitPath)
	}

	res, err := executil.Run(ctx, "multipass", args...)
	if err == nil {
		return nil
	}
	if isLaunchContinuing(res.Stdout, res.Stderr) {
		return nil
	}
	return err
}

func (m *Multipass) Start(ctx context.Context, name string) error {
	_, _ = executil.Run(ctx, "multipass", "wait-ready", "--timeout", fmt.Sprintf("%d", multipassWaitReadyTimeoutSeconds))
	_, err := executil.Run(ctx, "multipass", "start", "--timeout", fmt.Sprintf("%d", multipassStartTimeoutSeconds), name)
	return err
}

func (m *Multipass) Stop(ctx context.Context, name string) error {
	_, err := executil.Run(ctx, "multipass", "stop", name)
	return err
}

func (m *Multipass) Delete(ctx context.Context, name string) error {
	if _, err := executil.Run(ctx, "multipass", "delete", name); err != nil {
		return err
	}
	_, err := executil.Run(ctx, "multipass", "purge")
	return err
}

func (m *Multipass) List(ctx context.Context) ([]InstanceInfo, error) {
	res, err := executil.Run(ctx, "multipass", "list", "--format", "json")
	if err != nil {
		return nil, err
	}

	type rawEntry struct {
		Name    string   `json:"name"`
		State   string   `json:"state"`
		IPv4    []string `json:"ipv4"`
		Release string   `json:"release"`
	}

	raw := struct {
		List []rawEntry `json:"list"`
	}{}
	if err := json.Unmarshal([]byte(res.Stdout), &raw); err != nil {
		return nil, err
	}

	out := make([]InstanceInfo, 0, len(raw.List))
	for _, entry := range raw.List {
		out = append(out, InstanceInfo{
			Name:    entry.Name,
			Status:  normalizeState(entry.State),
			IPv4:    entry.IPv4,
			Release: entry.Release,
			Backend: m.Name(),
		})
	}

	return out, nil
}

func (m *Multipass) Info(ctx context.Context, name string) (*InstanceInfo, error) {
	res, err := executil.Run(ctx, "multipass", "info", "--format", "json", name)
	if err != nil {
		return nil, err
	}

	type rawInfo struct {
		State         string   `json:"state"`
		IPv4          []string `json:"ipv4"`
		Release       string   `json:"release"`
		ImageHash     string   `json:"image_hash"`
		CPUCount      string   `json:"cpu_count"`
		SnapshotCount string   `json:"snapshot_count"`
		Memory        struct {
			Total int64 `json:"total"`
			Used  int64 `json:"used"`
		} `json:"memory"`
		Disks map[string]struct {
			Total string `json:"total"`
			Used  string `json:"used"`
		} `json:"disks"`
	}

	raw := struct {
		Info map[string]rawInfo `json:"info"`
	}{}
	if err := json.Unmarshal([]byte(res.Stdout), &raw); err != nil {
		return nil, err
	}

	entry, ok := raw.Info[name]
	if !ok {
		return nil, fmt.Errorf("instance %q not found", name)
	}
	cpuCount, _ := strconv.Atoi(entry.CPUCount)
	snapshotCount, _ := strconv.Atoi(entry.SnapshotCount)
	memoryMiB := 0
	if entry.Memory.Total > 0 {
		memoryMiB = int(entry.Memory.Total / 1024 / 1024)
	}
	diskUsage := ""
	for _, disk := range entry.Disks {
		diskUsage = fmt.Sprintf("%s/%s", disk.Used, disk.Total)
		break
	}
	return &InstanceInfo{
		Name:      name,
		Status:    normalizeState(entry.State),
		IPv4:      entry.IPv4,
		Release:   entry.Release,
		ImageHash: entry.ImageHash,
		CPUCount:  cpuCount,
		MemoryMiB: memoryMiB,
		DiskUsage: diskUsage,
		Snapshots: snapshotCount,
		Backend:   m.Name(),
	}, nil
}

func (m *Multipass) UpdateResources(ctx context.Context, req UpdateResourcesRequest) error {
	if req.Name == "" {
		return fmt.Errorf("instance name is required")
	}
	if req.CPUs > 0 {
		if _, err := executil.Run(ctx, "multipass", "set", fmt.Sprintf("local.%s.cpus=%d", req.Name, req.CPUs)); err != nil {
			return err
		}
	}
	if req.MemoryGB > 0 {
		if _, err := executil.Run(ctx, "multipass", "set", fmt.Sprintf("local.%s.memory=%dG", req.Name, req.MemoryGB)); err != nil {
			return err
		}
	}
	if req.DiskGB > 0 {
		if _, err := executil.Run(ctx, "multipass", "set", fmt.Sprintf("local.%s.disk=%dG", req.Name, req.DiskGB)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Multipass) InstallSSHKey(ctx context.Context, req InstallSSHKeyRequest) error {
	if req.Name == "" {
		return fmt.Errorf("instance name is required")
	}
	user := req.Username
	if user == "" {
		user = "root"
	}
	var home string
	if user == "root" {
		home = "/root"
	} else {
		home = "/home/" + user
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(req.PublicKey))
	script := fmt.Sprintf(
		"pubkey=$(printf '%%s' %q | base64 -d) && install -d -m 700 %s/.ssh && touch %s/.ssh/authorized_keys && grep -qxF \"$pubkey\" %s/.ssh/authorized_keys || printf '%%s\\n' \"$pubkey\" >> %s/.ssh/authorized_keys && chmod 600 %s/.ssh/authorized_keys && chown -R %s:%s %s/.ssh",
		encoded,
		home,
		home,
		home,
		home,
		home,
		user,
		user,
		home,
	)

	_, err := executil.Run(ctx, "multipass", "exec", req.Name, "--", "bash", "-lc", script)
	return err
}

func (m *Multipass) Snapshot(ctx context.Context, name, snapshotName, comment string) error {
	args := []string{"snapshot"}
	if snapshotName != "" {
		args = append(args, "--name", snapshotName)
	}
	if comment != "" {
		args = append(args, "--comment", comment)
	}
	args = append(args, name)
	_, err := executil.Run(ctx, "multipass", args...)
	return err
}

func (m *Multipass) Restore(ctx context.Context, name, snapshotName string) error {
	target := fmt.Sprintf("%s.%s", name, snapshotName)
	_, err := executil.Run(ctx, "multipass", "restore", target)
	return err
}

func (m *Multipass) ListSnapshots(ctx context.Context, name string) ([]SnapshotInfo, error) {
	res, err := executil.Run(ctx, "multipass", "list", "--snapshots", "--format", "json")
	if err != nil {
		return nil, err
	}

	type rawSnapshot struct {
		Snapshot string `json:"snapshot"`
		Parent   string `json:"parent"`
		Comment  string `json:"comment"`
	}

	raw := struct {
		Info map[string]map[string]rawSnapshot `json:"info"`
	}{}
	if err := json.Unmarshal([]byte(res.Stdout), &raw); err != nil {
		return nil, err
	}

	entries := raw.Info[name]
	out := make([]SnapshotInfo, 0, len(entries))
	for snapshotName, entry := range entries {
		out = append(out, SnapshotInfo{
			Instance: name,
			Name:     fallbackSnapshotName(entry.Snapshot, snapshotName),
			Parent:   entry.Parent,
			Comment:  entry.Comment,
		})
	}

	return out, nil
}

func normalizeState(state string) InstanceStatus {
	switch strings.ToLower(state) {
	case "running":
		return StatusRunning
	case "stopped", "suspended":
		return StatusStopped
	default:
		return StatusUnknown
	}
}

func isLaunchContinuing(stdout, stderr string) bool {
	combined := strings.ToLower(strings.Join([]string{stdout, stderr}, "\n"))
	if strings.Contains(combined, "being prepared") {
		return true
	}
	if strings.Contains(combined, "timed out") && (strings.Contains(combined, "initialisation") || strings.Contains(combined, "initialization")) {
		return true
	}
	if strings.Contains(combined, "waiting for initialization to complete") || strings.Contains(combined, "waiting for initialisation to complete") {
		return true
	}
	return false
}

func fallbackSnapshotName(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
