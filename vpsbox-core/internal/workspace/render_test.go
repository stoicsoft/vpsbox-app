package workspace

import (
	"strings"
	"testing"
)

func TestRenderNetplanDeclaresOnlyTheExtraAddress(t *testing.T) {
	out := RenderNetplan("enp0s1", "10.42.1.10")

	if !strings.Contains(out, "    enp0s1:") {
		t.Errorf("netplan does not target the interface:\n%s", out)
	}
	if !strings.Contains(out, "- 10.42.1.10/24") {
		t.Errorf("netplan does not carry the address with its prefix:\n%s", out)
	}
	// Declaring dhcp4 here would fight the image's own config for the primary
	// address instead of merging with it.
	if strings.Contains(out, "dhcp4") {
		t.Errorf("netplan drop-in must not touch DHCP:\n%s", out)
	}
	if !strings.HasPrefix(out, "# Managed by vpsbox") {
		t.Error("generated file is not marked as generated")
	}
}

func TestRenderNetplanIsValidYAMLShape(t *testing.T) {
	out := RenderNetplan("ens3", "10.42.9.14")

	for _, want := range []string{"network:", "  version: 2", "  ethernets:", "    ens3:", "      addresses:"} {
		if !strings.Contains(out, want) {
			t.Errorf("netplan missing %q:\n%s", want, out)
		}
	}
}

func TestRenderHostsBlockIsFencedAndNamesEveryMember(t *testing.T) {
	block := RenderHostsBlock("alpha", []Member{
		{Instance: "api", PrivateIP: "10.42.1.11"},
		{Instance: "db", PrivateIP: "10.42.1.10"},
	})

	if !strings.HasPrefix(block, HostsStart) {
		t.Errorf("block is not fenced at the start:\n%s", block)
	}
	if !strings.HasSuffix(block, HostsEnd) {
		t.Errorf("block is not fenced at the end:\n%s", block)
	}

	// Both the bare name and the qualified one, so `ping db` works and there is
	// still a name that cannot collide.
	for _, want := range []string{
		"10.42.1.10 db db.alpha.vpsbox.internal",
		"10.42.1.11 api api.alpha.vpsbox.internal",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}
}

func TestRenderHostsBlockIsStableAcrossInputOrder(t *testing.T) {
	// A re-sync that only reorders lines would look like a real change and make
	// the generated file churn.
	first := RenderHostsBlock("alpha", []Member{
		{Instance: "db", PrivateIP: "10.42.1.10"},
		{Instance: "api", PrivateIP: "10.42.1.11"},
	})
	second := RenderHostsBlock("alpha", []Member{
		{Instance: "api", PrivateIP: "10.42.1.11"},
		{Instance: "db", PrivateIP: "10.42.1.10"},
	})

	if first != second {
		t.Errorf("hosts block depends on input order:\n%s\n---\n%s", first, second)
	}
}

func TestRenderHostsBlockWithNoMembers(t *testing.T) {
	block := RenderHostsBlock("alpha", nil)

	if !strings.HasPrefix(block, HostsStart) || !strings.HasSuffix(block, HostsEnd) {
		t.Errorf("empty block must still be fenced so the next sync can replace it:\n%s", block)
	}
}

func TestRenderNFTAllowsOwnSubnetAndDropsOthers(t *testing.T) {
	out := RenderNFT("10.42.1.0/24")

	if !strings.Contains(out, "ip saddr 10.42.1.0/24 accept") {
		t.Errorf("own workspace is not allowed:\n%s", out)
	}
	// The drop is the isolation. Without it, every workspace can reach every
	// other one, because they all share the hypervisor's L2 segment.
	if !strings.Contains(out, "ip saddr "+BaseNetwork+" drop") {
		t.Errorf("other workspaces are not blocked:\n%s", out)
	}

	// The accept must come first, or our own subnet gets caught by the drop.
	acceptAt := strings.Index(out, "ip saddr 10.42.1.0/24 accept")
	dropAt := strings.Index(out, "ip saddr "+BaseNetwork+" drop")
	if acceptAt > dropAt {
		t.Error("the drop rule precedes the accept, which would block our own workspace")
	}
}

func TestRenderNFTKeepsOutboundConnectionsWorking(t *testing.T) {
	out := RenderNFT("10.42.1.0/24")

	// Without this, blocking inbound also breaks replies to connections the
	// sandbox opened itself.
	if !strings.Contains(out, "ct state established,related accept") {
		t.Errorf("established connections are not accepted:\n%s", out)
	}
}

func TestRenderNFTUsesAnAcceptPolicyInItsOwnTable(t *testing.T) {
	out := RenderNFT("10.42.1.0/24")

	// An accept policy in a dedicated table means we govern workspace traffic and
	// nothing else — we never become responsible for the user's other rules.
	if !strings.Contains(out, "policy accept;") {
		t.Errorf("chain policy is not accept:\n%s", out)
	}
	if !strings.Contains(out, "table inet "+NFTTable) {
		t.Errorf("rules are not in our own table:\n%s", out)
	}
}

func TestRenderFirewallServiceSurvivesReboot(t *testing.T) {
	out := RenderFirewallService()

	// nftables rules are not persistent, and a firewall that quietly disappears
	// on reboot is worse than none because you stop checking.
	if !strings.Contains(out, "WantedBy=multi-user.target") {
		t.Errorf("unit is not enabled at boot:\n%s", out)
	}
	if !strings.Contains(out, "ExecStart=/usr/sbin/nft -f "+NFTPath) {
		t.Errorf("unit does not load the rules:\n%s", out)
	}
	// Stopping must remove only our table, leaving any other firewall intact.
	if !strings.Contains(out, "ExecStop=/usr/sbin/nft delete table inet "+NFTTable) {
		t.Errorf("unit does not scope its teardown to our table:\n%s", out)
	}
	if !strings.Contains(out, "Type=oneshot") || !strings.Contains(out, "RemainAfterExit=yes") {
		t.Errorf("a rule-loading unit must be a oneshot that stays active:\n%s", out)
	}
}

func TestNetplanPathSortsLast(t *testing.T) {
	// Netplan merges files in lexical order; ours has to come after the image's
	// own config so the merge includes our address.
	if !strings.Contains(NetplanPath, "/99-") {
		t.Errorf("netplan drop-in %q is not ordered last", NetplanPath)
	}
}
