package app

import (
	"strings"
	"testing"

	"github.com/stoicsoft/vpsbox/internal/workspace"
)

// probeOutput is real output from the stock cloud image. Note that netplan's key
// for the NIC is "default", not the interface name — the whole reason the probe
// exists.
const probeOutput = `@@iface
default via 192.168.2.1 dev enp0s1 proto dhcp src 192.168.2.15 metric 100
@@netdef
default:
`

func TestParseWorkspaceProbe(t *testing.T) {
	iface, netdef, err := parseWorkspaceProbe(probeOutput)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	if iface != "enp0s1" {
		t.Errorf("interface = %q, want enp0s1", iface)
	}
	// Using the interface name here instead would emit a second, competing
	// netplan definition and the address would silently never appear.
	if netdef != "default" {
		t.Errorf("netdef key = %q, want default", netdef)
	}
}

func TestParseWorkspaceProbeIgnoresNestedYAMLKeys(t *testing.T) {
	// `netplan get ethernets` prints the whole mapping when a filter is not
	// applied; only the unindented key is the definition name.
	out := `@@iface
default via 192.168.2.1 dev enp0s1 proto dhcp metric 100
@@netdef
default:
  match:
    macaddress: "52:54:00:68:96:84"
  dhcp4: true
`
	_, netdef, err := parseWorkspaceProbe(out)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	if netdef != "default" {
		t.Errorf("netdef = %q, want default — nested keys must not be mistaken for it", netdef)
	}
}

func TestParseWorkspaceProbeFallsBackToInterfaceName(t *testing.T) {
	// With no ethernets declared there is nothing to merge into, so creating a
	// definition keyed by the interface name is correct.
	out := "@@iface\ndefault via 10.0.0.1 dev ens3 proto dhcp metric 100\n@@netdef\n"

	iface, netdef, err := parseWorkspaceProbe(out)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	if iface != "ens3" || netdef != "ens3" {
		t.Errorf("got (%q, %q), want (ens3, ens3)", iface, netdef)
	}
}

func TestParseWorkspaceProbeRequiresAnInterface(t *testing.T) {
	if _, _, err := parseWorkspaceProbe("@@iface\n@@netdef\ndefault:\n"); err == nil {
		t.Error("a probe with no default route should fail rather than guess")
	}
}

func TestParseWorkspaceProbeTakesFirstOfSeveralRoutes(t *testing.T) {
	out := `@@iface
default via 192.168.2.1 dev enp0s1 proto dhcp metric 100
default via 10.0.0.1 dev enp0s2 proto dhcp metric 200
@@netdef
default:
eth1:
`
	iface, netdef, err := parseWorkspaceProbe(out)
	if err != nil {
		t.Fatalf("parse probe: %v", err)
	}
	if iface != "enp0s1" {
		t.Errorf("interface = %q, want the lowest-metric enp0s1", iface)
	}
	if netdef != "default" {
		t.Errorf("netdef = %q, want the first key", netdef)
	}
}

func testWorkspaceApplyConfig() workspaceApplyConfig {
	return workspaceApplyConfig{
		Interface: "enp0s1",
		NetdefKey: "default",
		PrivateIP: "10.42.1.10",
		CIDR:      "10.42.1.0/24",
		Workspace: "alpha",
		Members: []workspace.Member{
			{Instance: "db", PrivateIP: "10.42.1.10"},
			{Instance: "api", PrivateIP: "10.42.1.11"},
		},
	}
}

// The netplan drop-in must be keyed by netplan's definition name, and the
// verification step by the kernel's interface name. Swapping them produces a
// config netplan accepts and silently does not apply.
func TestWorkspaceApplyScriptKeysNetplanByDefinitionNotInterface(t *testing.T) {
	cfg := testWorkspaceApplyConfig()
	script := workspaceApplyScript(cfg)

	if !strings.Contains(script, "    "+cfg.NetdefKey+":\n") {
		t.Errorf("netplan drop-in is not keyed by the definition name %q", cfg.NetdefKey)
	}
	if strings.Contains(script, "    "+cfg.Interface+":\n") {
		t.Errorf("netplan drop-in is keyed by the interface name, which creates a competing definition")
	}
	// The verification still has to use the kernel name — netplan's key is not an
	// interface as far as `ip` is concerned.
	if !strings.Contains(script, "ip -4 addr show dev "+cfg.Interface) {
		t.Errorf("verification does not use the kernel interface name")
	}
}

func TestWorkspaceApplyScriptFailsFast(t *testing.T) {
	script := workspaceApplyScript(testWorkspaceApplyConfig())

	// Half-applying a network — address but no firewall — is worse than not
	// applying it, because the isolation silently is not there.
	if !strings.HasPrefix(script, "set -eu") {
		t.Error("apply script does not abort on the first failure")
	}
}

func TestWorkspaceApplyScriptPersistsTheAddress(t *testing.T) {
	script := workspaceApplyScript(testWorkspaceApplyConfig())

	// `ip addr add` would vanish on reboot, which teaches the wrong lesson about
	// what a private network is.
	if strings.Contains(script, "ip addr add") {
		t.Error("apply script adds a volatile address instead of persisting it")
	}
	if !strings.Contains(script, workspace.NetplanPath) {
		t.Error("apply script does not write a netplan drop-in")
	}
	if !strings.Contains(script, "sudo netplan apply") {
		t.Error("apply script does not activate the new config")
	}
	// Netplan refuses a config others can read.
	if !strings.Contains(script, "sudo chmod 600 "+workspace.NetplanPath) {
		t.Error("netplan drop-in is not restricted to root")
	}
}

func TestWorkspaceApplyScriptVerifiesTheAddressCameUp(t *testing.T) {
	cfg := testWorkspaceApplyConfig()
	script := workspaceApplyScript(cfg)

	// netplan apply can succeed while the address does not appear; without this
	// check the command would report success on a broken network.
	if !strings.Contains(script, "ip -4 addr show dev "+cfg.Interface) {
		t.Error("apply script does not verify the address is actually up")
	}
	if !strings.Contains(script, "exit 1") {
		t.Error("apply script does not fail when the address is missing")
	}
}

func TestWorkspaceApplyScriptReplacesRatherThanStacks(t *testing.T) {
	script := workspaceApplyScript(testWorkspaceApplyConfig())

	// Joining is re-runnable, so both the hosts block and the firewall table have
	// to be replaced rather than appended to. Hosts replacement happens inside the
	// restore script, which apply invokes so there is one code path shared with
	// the boot-time unit.
	if !strings.Contains(script, "sudo /bin/sh "+workspace.HostsRestoreScript) {
		t.Error("apply script does not run the hosts restore script")
	}
	if !strings.Contains(workspace.RenderHostsRestoreScript(), "sed -i") {
		t.Error("hosts restore script does not strip the previous block before appending")
	}
	if !strings.Contains(script, "nft delete table inet "+workspace.NFTTable) {
		t.Error("apply script does not replace its previous firewall table")
	}
}

// cloud-init regenerates /etc/hosts on every boot, so the names have to be saved
// and re-applied by a unit or they silently stop resolving after a restart.
func TestWorkspaceApplyScriptMakesNamesSurviveReboot(t *testing.T) {
	script := workspaceApplyScript(testWorkspaceApplyConfig())

	if !strings.Contains(script, workspace.HostsSavePath) {
		t.Error("apply script does not save the names block for restoration")
	}
	if !strings.Contains(script, "systemctl enable "+workspace.HostsServiceName) {
		t.Error("apply script does not enable the hosts restore unit")
	}
}

func TestWorkspaceApplyScriptNamesEveryMember(t *testing.T) {
	script := workspaceApplyScript(testWorkspaceApplyConfig())

	for _, want := range []string{
		"10.42.1.10 db db.alpha.vpsbox.internal",
		"10.42.1.11 api api.alpha.vpsbox.internal",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("apply script missing host entry %q", want)
		}
	}
}

func TestWorkspaceApplyScriptCarriesTheIsolationRules(t *testing.T) {
	cfg := testWorkspaceApplyConfig()
	script := workspaceApplyScript(cfg)

	if !strings.Contains(script, "ip saddr "+cfg.CIDR+" accept") {
		t.Error("apply script does not allow the workspace's own subnet")
	}
	if !strings.Contains(script, "ip saddr "+workspace.BaseNetwork+" drop") {
		t.Error("apply script does not block other workspaces")
	}
	if !strings.Contains(script, "systemctl enable "+workspace.ServiceName) {
		t.Error("apply script does not make the rules survive a reboot")
	}
}

func TestWorkspaceTeardownScriptToleratesPartialState(t *testing.T) {
	script := workspaceTeardownScript()

	// Teardown runs on sandboxes that may only ever have been half configured, so
	// it must not abort partway and leave the rest behind.
	if strings.HasPrefix(script, "set -e") {
		t.Error("teardown aborts on the first missing piece")
	}
	for _, want := range []string{
		workspace.NetplanPath,
		workspace.NFTPath,
		workspace.ServicePath,
		workspace.HostsServicePath,
		workspace.HostsSavePath,
		workspace.HostsRestoreScript,
		"nft delete table inet " + workspace.NFTTable,
		"netplan apply",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("teardown does not remove %q", want)
		}
	}
}

func TestWorkspaceTeardownRemovesTheHostsBlock(t *testing.T) {
	script := workspaceTeardownScript()

	// Leaving stale names behind would let a removed member still be resolved.
	if !strings.Contains(script, "sudo sed -i") || !strings.Contains(script, "/etc/hosts") {
		t.Error("teardown does not remove the managed hosts block")
	}
}

func TestWorkspaceScriptsAreIdempotentOnRepeat(t *testing.T) {
	cfg := testWorkspaceApplyConfig()

	// Rendering twice must be byte-identical, or a re-sync would look like a
	// change and churn the files on every run.
	if workspaceApplyScript(cfg) != workspaceApplyScript(cfg) {
		t.Error("apply script is not deterministic")
	}
	if workspaceTeardownScript() != workspaceTeardownScript() {
		t.Error("teardown script is not deterministic")
	}
}
