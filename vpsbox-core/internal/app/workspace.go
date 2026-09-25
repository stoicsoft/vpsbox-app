package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/stoicsoft/vpsbox/internal/registry"
	"github.com/stoicsoft/vpsbox/internal/workspace"
)

// WorkspaceInfo is a private network and who is on it, shaped for callers.
type WorkspaceInfo struct {
	Name      string
	CIDR      string
	Gateway   string
	CreatedAt time.Time
	Members   []WorkspaceMemberInfo
}

// WorkspaceMemberInfo is one sandbox on a private network.
type WorkspaceMemberInfo struct {
	Instance  string
	PrivateIP string
	Hostname  string // the fully qualified name other members can reach it by
	Status    string // the sandbox's own status, so a stopped member is visible
	JoinedAt  time.Time
}

// CreateWorkspace allocates a private subnet. Nothing is applied to any sandbox
// until something joins.
func (m *Manager) CreateWorkspace(ctx context.Context, name string) (*WorkspaceInfo, error) {
	if err := workspace.ValidateName(name); err != nil {
		return nil, err
	}

	existing, err := m.store.LoadWorkspaces()
	if err != nil {
		return nil, err
	}
	for _, candidate := range existing {
		if candidate.Name == name {
			return nil, fmt.Errorf("workspace %q already exists", name)
		}
	}

	used := make([]int, 0, len(existing))
	for _, candidate := range existing {
		used = append(used, candidate.Index)
	}
	index, err := workspace.AllocateIndex(used)
	if err != nil {
		return nil, err
	}
	cidr, err := workspace.CIDRForIndex(index)
	if err != nil {
		return nil, err
	}

	record := registry.Workspace{
		Name:      name,
		CIDR:      cidr,
		Index:     index,
		CreatedAt: time.Now().UTC(),
	}
	if err := m.store.UpsertWorkspace(record); err != nil {
		return nil, err
	}
	return m.describeWorkspace(record)
}

// ListWorkspaces returns every private network.
func (m *Manager) ListWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	records, err := m.store.LoadWorkspaces()
	if err != nil {
		return nil, err
	}

	out := make([]WorkspaceInfo, 0, len(records))
	for _, record := range records {
		info, err := m.describeWorkspace(record)
		if err != nil {
			return nil, err
		}
		out = append(out, *info)
	}
	return out, nil
}

// GetWorkspace returns one private network.
func (m *Manager) GetWorkspace(ctx context.Context, name string) (*WorkspaceInfo, error) {
	record, err := m.requireWorkspace(name)
	if err != nil {
		return nil, err
	}
	return m.describeWorkspace(*record)
}

// JoinWorkspace puts a sandbox on a private network: it allocates a stable
// address, configures the sandbox, and re-syncs every other member so they all
// learn the new name. Re-running it for a sandbox that is already a member just
// re-applies the configuration.
func (m *Manager) JoinWorkspace(ctx context.Context, name, instanceName string, status, output func(string)) (*WorkspaceInfo, error) {
	report := func(message string) {
		if status != nil {
			status(message)
		}
	}

	record, err := m.requireWorkspace(name)
	if err != nil {
		return nil, err
	}

	instance, err := m.requireRunningInstance(ctx, instanceName)
	if err != nil {
		return nil, err
	}

	// A sandbox on two private networks would be a route between networks that
	// are supposed to be isolated, which defeats the whole point.
	if other, err := m.store.WorkspaceForInstance(instance.Name); err == nil && other.Name != record.Name {
		return nil, fmt.Errorf("%s is already on the %q workspace — remove it from that one first", instance.Name, other.Name)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	privateIP := ""
	for _, member := range record.Members {
		if member.Instance == instance.Name {
			privateIP = member.PrivateIP
			break
		}
	}
	if privateIP == "" {
		taken := make([]string, 0, len(record.Members))
		for _, member := range record.Members {
			taken = append(taken, member.PrivateIP)
		}
		privateIP, err = workspace.NextMemberIP(record.CIDR, taken)
		if err != nil {
			return nil, err
		}
		record.Members = append(record.Members, registry.WorkspaceMember{
			Instance:  instance.Name,
			PrivateIP: privateIP,
			JoinedAt:  time.Now().UTC(),
		})
		if err := m.store.UpsertWorkspace(*record); err != nil {
			return nil, err
		}
	}

	report(fmt.Sprintf("Giving %s the private address %s…", instance.Name, privateIP))
	if err := m.applyWorkspaceTo(ctx, instance, *record, privateIP, output); err != nil {
		return nil, err
	}

	// Everyone else needs the new member in their hosts file before they can
	// reach it by name.
	report("Telling the other members about it…")
	if err := m.syncWorkspaceMembers(ctx, *record, instance.Name, output); err != nil {
		return nil, err
	}

	report(fmt.Sprintf("%s is on %s at %s", instance.Name, record.Name, privateIP))
	return m.describeWorkspace(*record)
}

// LeaveWorkspace takes a sandbox off a private network, removing its address and
// firewall rules, then re-syncs the members that remain.
func (m *Manager) LeaveWorkspace(ctx context.Context, name, instanceName string, status, output func(string)) (*WorkspaceInfo, error) {
	report := func(message string) {
		if status != nil {
			status(message)
		}
	}

	record, err := m.requireWorkspace(name)
	if err != nil {
		return nil, err
	}

	remaining := make([]registry.WorkspaceMember, 0, len(record.Members))
	found := false
	for _, member := range record.Members {
		if member.Instance == instanceName {
			found = true
			continue
		}
		remaining = append(remaining, member)
	}
	if !found {
		return nil, fmt.Errorf("%s is not on the %q workspace", instanceName, name)
	}
	record.Members = remaining
	if err := m.store.UpsertWorkspace(*record); err != nil {
		return nil, err
	}

	// Best effort: the membership record is already gone, and a sandbox that is
	// stopped or broken must not block the others from being re-synced.
	report(fmt.Sprintf("Removing the private network from %s…", instanceName))
	if instance, err := m.requireRunningInstance(ctx, instanceName); err == nil {
		if _, err := m.streamRemoteOn(ctx, instance, workspaceTeardownScript(), output); err != nil {
			report(fmt.Sprintf("Could not clean up %s (%v) — its membership was still removed", instanceName, err))
		}
	} else {
		report(fmt.Sprintf("%s is not reachable — its membership was removed anyway", instanceName))
	}

	report("Re-syncing the remaining members…")
	if err := m.syncWorkspaceMembers(ctx, *record, "", output); err != nil {
		return nil, err
	}
	return m.describeWorkspace(*record)
}

// SyncWorkspace re-applies the network to every member. This is the repair
// command: sandboxes that were stopped when something changed, or that came back
// with a different address, are put right by running it.
func (m *Manager) SyncWorkspace(ctx context.Context, name string, status, output func(string)) (*WorkspaceInfo, error) {
	report := func(message string) {
		if status != nil {
			status(message)
		}
	}

	record, err := m.requireWorkspace(name)
	if err != nil {
		return nil, err
	}
	if len(record.Members) == 0 {
		report(fmt.Sprintf("Workspace %q has no members yet", name))
		return m.describeWorkspace(*record)
	}

	report(fmt.Sprintf("Re-applying %s to %d member(s)…", name, len(record.Members)))
	if err := m.syncWorkspaceMembers(ctx, *record, "", output); err != nil {
		return nil, err
	}
	return m.describeWorkspace(*record)
}

// DestroyWorkspace removes a private network, taking the configuration off every
// member first. The sandboxes themselves are untouched.
func (m *Manager) DestroyWorkspace(ctx context.Context, name string, status, output func(string)) error {
	report := func(message string) {
		if status != nil {
			status(message)
		}
	}

	record, err := m.requireWorkspace(name)
	if err != nil {
		return err
	}

	for _, member := range record.Members {
		instance, err := m.requireRunningInstance(ctx, member.Instance)
		if err != nil {
			report(fmt.Sprintf("%s is not reachable — skipping its cleanup", member.Instance))
			continue
		}
		report(fmt.Sprintf("Removing the private network from %s…", member.Instance))
		if _, err := m.streamRemoteOn(ctx, instance, workspaceTeardownScript(), output); err != nil {
			report(fmt.Sprintf("Could not clean up %s (%v)", member.Instance, err))
		}
	}

	return m.store.DeleteWorkspace(name)
}

// WorkspaceReachability checks, from one member, whether it can actually reach
// the others. Claiming a private network works without testing it is how a
// silent netplan failure turns into a confusing bug later.
func (m *Manager) WorkspaceReachability(ctx context.Context, name, fromInstance string) (map[string]bool, error) {
	record, err := m.requireWorkspace(name)
	if err != nil {
		return nil, err
	}

	instance, err := m.requireRunningInstance(ctx, fromInstance)
	if err != nil {
		return nil, err
	}

	results := make(map[string]bool)
	for _, member := range record.Members {
		if member.Instance == fromInstance {
			continue
		}
		// One packet, one second: this runs across every member, and an
		// unreachable host should fail fast rather than stall the report.
		command := fmt.Sprintf("ping -c 1 -W 1 %s >/dev/null 2>&1 && echo ok || echo unreachable", member.PrivateIP)
		stdout, _, err := m.runRemoteOn(ctx, instance, command)
		results[member.Instance] = err == nil && strings.Contains(stdout, "ok")
	}
	return results, nil
}

func (m *Manager) requireWorkspace(name string) (*registry.Workspace, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("workspace name is required")
	}
	record, err := m.store.GetWorkspace(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no workspace named %q (create one with `vpsbox workspace create %s`)", name, name)
		}
		return nil, err
	}
	return record, nil
}

// describeWorkspace joins a workspace record against the instance registry so
// callers can see which members are actually running.
func (m *Manager) describeWorkspace(record registry.Workspace) (*WorkspaceInfo, error) {
	gateway, err := workspace.GatewayForCIDR(record.CIDR)
	if err != nil {
		return nil, err
	}

	info := &WorkspaceInfo{
		Name:      record.Name,
		CIDR:      record.CIDR,
		Gateway:   gateway,
		CreatedAt: record.CreatedAt,
		Members:   make([]WorkspaceMemberInfo, 0, len(record.Members)),
	}

	for _, member := range record.Members {
		status := "unknown"
		if instance, err := m.store.GetInstance(member.Instance); err == nil {
			status = instance.Status
		}
		names := workspace.HostNamesFor(member.Instance, record.Name)
		info.Members = append(info.Members, WorkspaceMemberInfo{
			Instance:  member.Instance,
			PrivateIP: member.PrivateIP,
			Hostname:  names[len(names)-1],
			Status:    status,
			JoinedAt:  member.JoinedAt,
		})
	}

	sort.Slice(info.Members, func(i, j int) bool {
		return info.Members[i].PrivateIP < info.Members[j].PrivateIP
	})
	return info, nil
}

// syncWorkspaceMembers re-applies the network to every member, optionally
// skipping one that was just configured. A member that cannot be reached is
// reported and skipped rather than failing the whole operation — one stopped
// sandbox must not stop the others from being wired up.
func (m *Manager) syncWorkspaceMembers(ctx context.Context, record registry.Workspace, skip string, output func(string)) error {
	var failures []string

	for _, member := range record.Members {
		if member.Instance == skip {
			continue
		}
		instance, err := m.requireRunningInstance(ctx, member.Instance)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s (not running)", member.Instance))
			continue
		}
		if err := m.applyWorkspaceTo(ctx, instance, record, member.PrivateIP, output); err != nil {
			failures = append(failures, fmt.Sprintf("%s (%v)", member.Instance, err))
		}
	}

	if len(failures) > 0 && output != nil {
		output("Could not sync: " + strings.Join(failures, ", "))
		output("Start them and run `vpsbox workspace sync " + record.Name + "` to finish.")
	}
	return nil
}

// applyWorkspaceTo configures one sandbox: private address, member names, and
// the isolation rules.
func (m *Manager) applyWorkspaceTo(ctx context.Context, instance *registry.Instance, record registry.Workspace, privateIP string, output func(string)) error {
	// Both the interface name and netplan's key for it vary by image, and they are
	// not the same string — see workspace.RenderNetplan for why using the wrong one
	// fails silently. Ask the sandbox for both in a single round trip.
	probeOut, probeErr, err := m.runRemoteOn(ctx, instance, workspaceProbeScript)
	if err != nil {
		return fmt.Errorf("inspect the sandbox's network: %w\n%s", err, strings.TrimSpace(probeErr))
	}
	iface, netdef, err := parseWorkspaceProbe(probeOut)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(probeOut))
	}

	members := make([]workspace.Member, 0, len(record.Members))
	for _, member := range record.Members {
		members = append(members, workspace.Member{Instance: member.Instance, PrivateIP: member.PrivateIP})
	}

	script := workspaceApplyScript(workspaceApplyConfig{
		Interface: iface,
		NetdefKey: netdef,
		PrivateIP: privateIP,
		CIDR:      record.CIDR,
		Workspace: record.Name,
		Members:   members,
	})

	tail, err := m.streamRemoteOn(ctx, instance, script, output)
	if err != nil {
		return fmt.Errorf("configure %s: %w\n%s", instance.Name, err, tail)
	}
	return nil
}

// workspaceProbeScript collects the two facts needed to write a netplan drop-in
// that merges instead of colliding: the kernel's interface name, and netplan's
// own key for that interface. Sections are fenced so the output parses
// deterministically, the same way liveAppsScript does it.
const workspaceProbeScript = `echo "@@iface"
ip route show default
echo "@@netdef"
sudo netplan get ethernets 2>/dev/null | grep -E '^[a-zA-Z0-9_.-]+:' || true
`

// parseWorkspaceProbe reads the probe output into the interface name and the
// netplan definition key.
//
// If netplan declares no ethernets at all there is nothing to merge with, so the
// interface name is the right key to create one under.
func parseWorkspaceProbe(out string) (iface string, netdef string, err error) {
	section := ""
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
		case "iface":
			if iface != "" {
				continue
			}
			fields := strings.Fields(line)
			for i := 0; i < len(fields)-1; i++ {
				if fields[i] == "dev" {
					iface = fields[i+1]
					break
				}
			}
		case "netdef":
			if netdef != "" {
				continue
			}
			// Top-level keys only: nested mapping lines are indented.
			if strings.HasPrefix(raw, " ") || strings.HasPrefix(raw, "\t") {
				continue
			}
			netdef = strings.TrimSuffix(line, ":")
		}
	}

	if iface == "" {
		return "", "", errors.New("could not work out the sandbox's network interface from its routing table")
	}
	if netdef == "" {
		netdef = iface
	}
	return iface, netdef, nil
}
