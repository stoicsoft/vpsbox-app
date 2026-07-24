package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stoicsoft/vpsbox/internal/backend"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

const checkpointPrefix = "checkpoint-"

// Checkpoint takes a snapshot of the VM AND captures a baseline of in-VM
// state (packages, services, listening ports, /etc file timestamps) so the
// user can later see what changed (`vpsbox diff`) or roll back (`vpsbox undo`).
//
// The sandbox has to be running: the backend can only snapshot a stopped VM, so
// Snapshot stops and restarts it, and the baseline capture that follows needs a
// live SSH connection.
func (m *Manager) Checkpoint(ctx context.Context, name, label string) (*registry.Baseline, error) {
	instance, err := m.requireRunningInstance(ctx, name)
	if err != nil {
		return nil, err
	}

	label = checkpointLabel(label)

	if err := m.Snapshot(ctx, instance.Name, label, "vpsbox checkpoint"); err != nil {
		return nil, err
	}

	refreshed, err := m.waitAndRefreshInstance(ctx, *instance, false, nil)
	if err != nil {
		return nil, err
	}

	baseline, err := m.captureBaseline(ctx, refreshed, label)
	if err != nil {
		return nil, fmt.Errorf("snapshot saved but baseline capture failed: %w", err)
	}
	return baseline, nil
}

// checkpointLabel normalizes a user-supplied label, defaulting to a timestamp
// and making sure the checkpoint prefix Undo looks for is present.
func checkpointLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return checkpointPrefix + time.Now().UTC().Format("20060102-150405")
	}
	if strings.HasPrefix(label, checkpointPrefix) {
		return label
	}
	return checkpointPrefix + label
}

// RestoreCheckpoint restores a snapshot and re-captures the baseline against
// the state the sandbox was actually rolled back to. The baseline is a single
// file per instance, so without this it keeps describing the newest checkpoint
// and a diff taken after restoring an older snapshot reports changes that never
// happened. An empty snapshot name restores the most recent checkpoint.
func (m *Manager) RestoreCheckpoint(ctx context.Context, name, snapshot string) (*registry.Instance, string, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, "", err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return nil, "", err
	}

	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		snapshot, err = m.latestCheckpoint(ctx, instance.Name)
		if err != nil {
			return nil, "", err
		}
	}

	refreshed, err := m.Reset(ctx, instance.Name, snapshot)
	if err != nil {
		return nil, snapshot, err
	}

	if _, err := m.captureBaseline(ctx, refreshed, snapshot); err != nil {
		return refreshed, snapshot, fmt.Errorf("restored %s but baseline capture failed: %w", snapshot, err)
	}
	return refreshed, snapshot, nil
}

// Undo restores the most recent checkpoint snapshot. Returns the restored
// instance and the snapshot name that was used.
func (m *Manager) Undo(ctx context.Context, name string) (*registry.Instance, string, error) {
	return m.RestoreCheckpoint(ctx, name, "")
}

// latestCheckpoint returns the newest checkpoint-prefixed snapshot for an
// instance, ignoring snapshots taken by other features (labs, manual saves).
func (m *Manager) latestCheckpoint(ctx context.Context, instanceName string) (string, error) {
	snapshots, err := m.backend.ListSnapshots(ctx, instanceName)
	if err != nil {
		return "", err
	}
	latest := ""
	for _, s := range snapshots {
		if strings.HasPrefix(s.Name, checkpointPrefix) {
			latest = s.Name
		}
	}
	if latest == "" {
		return "", errors.New("no checkpoints found — create one with `vpsbox checkpoint`")
	}
	return latest, nil
}

// DeleteSnapshot removes a saved snapshot and reclaims its disk space. If the
// deleted snapshot is the one the diff baseline describes, the baseline goes
// with it — otherwise a later diff would be measured against a point the
// sandbox can no longer be restored to.
func (m *Manager) DeleteSnapshot(ctx context.Context, name, snapshot string) error {
	instance, err := m.requireInstance(name)
	if err != nil {
		return err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return err
	}

	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" {
		return errors.New("snapshot name is required")
	}

	if err := m.backend.DeleteSnapshot(ctx, instance.Name, snapshot); err != nil {
		return err
	}

	baseline, err := m.store.LoadBaseline(instance.Name)
	if err != nil {
		return err
	}
	if baseline != nil && baseline.Checkpoint == snapshot {
		return m.store.DeleteBaseline(instance.Name)
	}
	return nil
}

// CheckpointBaseline reports the saved baseline for an instance, or nil when no
// checkpoint has been taken yet. Callers use it to tell "no checkpoint" apart
// from "checkpoint exists and nothing changed" without opening an SSH session.
func (m *Manager) CheckpointBaseline(name string) (*registry.Baseline, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	return m.store.LoadBaseline(instance.Name)
}

// requireRunningInstance loads an instance and confirms the VM is up, with its
// address refreshed from the backend. Callers that need SSH must go through
// here: the refresh helpers poll for up to 20 minutes before giving up, so a
// stopped sandbox has to fail fast instead of hanging the caller.
func (m *Manager) requireRunningInstance(ctx context.Context, name string) (*registry.Instance, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}
	info, err := m.backend.Info(ctx, instance.Name)
	if err != nil {
		return nil, err
	}
	applyBackendInfo(instance, *info)
	if !strings.EqualFold(instance.Status, string(backend.StatusRunning)) {
		return nil, fmt.Errorf("%s is not running — start it first", instance.Name)
	}
	if instance.Host == "" {
		return nil, fmt.Errorf("%s has no address yet — wait for it to finish starting", instance.Name)
	}
	return instance, nil
}

// Panic is the friendly alias for Undo.
func (m *Manager) Panic(ctx context.Context, name string) (*registry.Instance, string, error) {
	return m.Undo(ctx, name)
}

// Diff captures the current VM state and compares it to the last saved
// checkpoint baseline.
func (m *Manager) Diff(ctx context.Context, name string) (*BaselineDiff, error) {
	instance, err := m.requireRunningInstance(ctx, name)
	if err != nil {
		return nil, err
	}
	base, err := m.store.LoadBaseline(instance.Name)
	if err != nil {
		return nil, err
	}
	if base == nil {
		return nil, errors.New("no checkpoint baseline found — create one with `vpsbox checkpoint`")
	}
	current, err := m.captureState(ctx, instance)
	if err != nil {
		return nil, err
	}
	return diffBaselines(base, current), nil
}

func (m *Manager) captureBaseline(ctx context.Context, instance *registry.Instance, checkpoint string) (*registry.Baseline, error) {
	state, err := m.captureState(ctx, instance)
	if err != nil {
		return nil, err
	}
	state.Instance = instance.Name
	state.Checkpoint = checkpoint
	state.CapturedAt = time.Now().UTC()
	if err := m.store.SaveBaseline(*state); err != nil {
		return nil, err
	}
	return state, nil
}

// captureState SSHes into the VM and gathers the state we care about for diff.
func (m *Manager) captureState(ctx context.Context, instance *registry.Instance) (*registry.Baseline, error) {
	const script = `set +e
echo '##PACKAGES##'
dpkg-query -W -f='${binary:Package}\n' 2>/dev/null | sort
echo '##SERVICES##'
systemctl list-units --type=service --state=running --no-pager --no-legend --plain 2>/dev/null | awk '{print $1}' | sort
echo '##PORTS##'
ss -H -tln 2>/dev/null | awk '{print $4}' | sort -u
echo '##ETC##'
sudo find /etc -type f -printf '%T@ %P\n' 2>/dev/null | sort -k2
echo '##END##'
`
	stdout, _, err := m.runRemoteOn(ctx, instance, script)
	if err != nil {
		return nil, err
	}
	return parseStateOutput(stdout), nil
}

func parseStateOutput(s string) *registry.Baseline {
	b := &registry.Baseline{
		EtcFiles: map[string]string{},
	}
	section := ""
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "##") && strings.HasSuffix(line, "##") {
			section = strings.Trim(line, "#")
			continue
		}
		if line == "" {
			continue
		}
		switch section {
		case "PACKAGES":
			b.Packages = append(b.Packages, line)
		case "SERVICES":
			b.Services = append(b.Services, line)
		case "PORTS":
			b.Ports = append(b.Ports, line)
		case "ETC":
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				b.EtcFiles[parts[1]] = parts[0]
			}
		}
	}
	return b
}

// BaselineDiff is the result of comparing current VM state to a checkpoint.
type BaselineDiff struct {
	Checkpoint      string
	CapturedAt      time.Time
	AddedPackages   []string
	RemovedPackages []string
	AddedServices   []string
	RemovedServices []string
	AddedPorts      []string
	RemovedPorts    []string
	AddedFiles      []string
	RemovedFiles    []string
	ModifiedFiles   []string
}

func (d *BaselineDiff) Total() int {
	return len(d.AddedPackages) + len(d.RemovedPackages) +
		len(d.AddedServices) + len(d.RemovedServices) +
		len(d.AddedPorts) + len(d.RemovedPorts) +
		len(d.AddedFiles) + len(d.RemovedFiles) + len(d.ModifiedFiles)
}

func diffBaselines(base, cur *registry.Baseline) *BaselineDiff {
	d := &BaselineDiff{
		Checkpoint: base.Checkpoint,
		CapturedAt: base.CapturedAt,
	}
	d.AddedPackages, d.RemovedPackages = diffSorted(cur.Packages, base.Packages)
	d.AddedServices, d.RemovedServices = diffSorted(cur.Services, base.Services)
	d.AddedPorts, d.RemovedPorts = diffSorted(cur.Ports, base.Ports)

	for path, mtime := range cur.EtcFiles {
		if baseMtime, ok := base.EtcFiles[path]; !ok {
			d.AddedFiles = append(d.AddedFiles, path)
		} else if baseMtime != mtime {
			d.ModifiedFiles = append(d.ModifiedFiles, path)
		}
	}
	for path := range base.EtcFiles {
		if _, ok := cur.EtcFiles[path]; !ok {
			d.RemovedFiles = append(d.RemovedFiles, path)
		}
	}
	sort.Strings(d.AddedFiles)
	sort.Strings(d.RemovedFiles)
	sort.Strings(d.ModifiedFiles)
	return d
}

func diffSorted(current, baseline []string) (added, removed []string) {
	cur := map[string]bool{}
	for _, v := range current {
		cur[v] = true
	}
	base := map[string]bool{}
	for _, v := range baseline {
		base[v] = true
	}
	for v := range cur {
		if !base[v] {
			added = append(added, v)
		}
	}
	for v := range base {
		if !cur[v] {
			removed = append(removed, v)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// CLI commands ----------------------------------------------------------------

func newCheckpointCommand(ctx context.Context, manager *Manager) *cobra.Command {
	var label string
	cmd := &cobra.Command{
		Use:   "checkpoint [name]",
		Short: "Save a checkpoint you can roll back to with `vpsbox undo`",
		Long: "Take a named snapshot of the sandbox AND record what's installed and running.\n" +
			"Later you can:\n" +
			"  vpsbox diff   — see what changed since this checkpoint\n" +
			"  vpsbox undo   — roll back to this checkpoint\n" +
			"  vpsbox panic  — friendly alias for undo when you've broken something",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Capturing checkpoint…")
			baseline, err := manager.Checkpoint(ctx, firstArg(args), label)
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ Checkpoint saved: %s\n\n", baseline.Checkpoint)
			fmt.Printf("  packages tracked:  %d\n", len(baseline.Packages))
			fmt.Printf("  services tracked:  %d\n", len(baseline.Services))
			fmt.Printf("  /etc files:        %d\n\n", len(baseline.EtcFiles))
			fmt.Println("Roll back any time with: vpsbox undo")
			fmt.Println("See what changed with:   vpsbox diff")
			return nil
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "custom checkpoint label (default: timestamp)")
	return cmd
}

func newUndoCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "undo [name]",
		Short: "Roll back to the most recent checkpoint",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Rolling back to your last checkpoint…")
			instance, snap, err := manager.Undo(ctx, firstArg(args))
			if err != nil {
				return err
			}
			fmt.Printf("\n✓ %s is back to %s\n", instance.Name, snap)
			return nil
		},
	}
}

func newPanicCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "panic [name]",
		Short: "When you've broken everything: restore the last checkpoint",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Don't panic. Rolling back to your last checkpoint…")
			instance, snap, err := manager.Panic(ctx, firstArg(args))
			if err != nil {
				fmt.Println()
				fmt.Println("Could not roll back automatically:", err)
				fmt.Println()
				fmt.Println("If this is your first session and you have no checkpoints yet,")
				fmt.Println("create one for next time with: vpsbox checkpoint")
				return err
			}
			fmt.Printf("\n✓ %s is back to %s. You're safe.\n", instance.Name, snap)
			return nil
		},
	}
}

func newDiffCommand(ctx context.Context, manager *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "diff [name]",
		Short: "Show what changed on the VM since the last checkpoint",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := manager.Diff(ctx, firstArg(args))
			if err != nil {
				return err
			}
			renderDiff(d)
			return nil
		},
	}
}

func renderDiff(d *BaselineDiff) {
	fmt.Printf("Compared to checkpoint %s (%s)\n\n", d.Checkpoint, d.CapturedAt.Local().Format(time.RFC822))

	printList := func(label, sym string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Printf("%s (%d):\n", label, len(items))
		for _, item := range items {
			fmt.Printf("  %s %s\n", sym, item)
		}
		fmt.Println()
	}

	printList("Packages added", "+", d.AddedPackages)
	printList("Packages removed", "-", d.RemovedPackages)
	printList("Services started", "+", d.AddedServices)
	printList("Services stopped", "-", d.RemovedServices)
	printList("Ports opened", "+", d.AddedPorts)
	printList("Ports closed", "-", d.RemovedPorts)
	printList("/etc files added", "+", d.AddedFiles)
	printList("/etc files removed", "-", d.RemovedFiles)
	printList("/etc files modified", "~", d.ModifiedFiles)

	if d.Total() == 0 {
		fmt.Println("No changes since checkpoint.")
	}
}
