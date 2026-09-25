package app

import (
	"fmt"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/workspace"
)

type workspaceApplyConfig struct {
	// Interface is the kernel's name for the NIC, used to verify the address
	// actually came up. NetdefKey is netplan's key for that same NIC, which is
	// often a different string — see workspace.RenderNetplan.
	Interface string
	NetdefKey string
	PrivateIP string
	CIDR      string
	Workspace string
	Members   []workspace.Member
}

// workspaceApplyScript builds the script that puts one sandbox on a private
// network. Every heredoc is quoted so the sandbox's shell performs no expansion
// on rendered config — addresses and names go in literally.
//
// The order matters: the address has to exist before the firewall references its
// subnet, and both have to be in place before we claim the network works.
func workspaceApplyScript(c workspaceApplyConfig) string {
	var b strings.Builder

	b.WriteString("set -eu\n\n")

	// The private address, written where it survives a reboot.
	b.WriteString("echo " + shellQuote("Adding private address "+c.PrivateIP+" on "+c.Interface) + "\n")
	b.WriteString("sudo tee " + workspace.NetplanPath + " >/dev/null <<'VPSBOX_NETPLAN_EOF'\n")
	b.WriteString(workspace.RenderNetplan(c.NetdefKey, c.PrivateIP))
	b.WriteString("VPSBOX_NETPLAN_EOF\n")
	// Netplan refuses to use a file others can read, and says so loudly.
	b.WriteString("sudo chmod 600 " + workspace.NetplanPath + "\n")
	b.WriteString("sudo netplan apply\n\n")

	// Names for the other members. The block is saved to disk as well as applied,
	// because cloud-init regenerates /etc/hosts on every boot and would otherwise
	// take it with it — see workspace.HostsSavePath.
	b.WriteString("sudo mkdir -p /etc/vpsbox\n")
	b.WriteString("sudo tee " + workspace.HostsSavePath + " >/dev/null <<'VPSBOX_WS_HOSTS_EOF'\n")
	b.WriteString(workspace.RenderHostsBlock(c.Workspace, c.Members))
	b.WriteString("\nVPSBOX_WS_HOSTS_EOF\n")

	b.WriteString("sudo tee " + workspace.HostsRestoreScript + " >/dev/null <<'VPSBOX_WS_RESTORE_EOF'\n")
	b.WriteString(workspace.RenderHostsRestoreScript())
	b.WriteString("VPSBOX_WS_RESTORE_EOF\n")
	b.WriteString("sudo chmod 755 " + workspace.HostsRestoreScript + "\n")

	b.WriteString("sudo tee " + workspace.HostsServicePath + " >/dev/null <<'VPSBOX_WS_HOSTS_UNIT_EOF'\n")
	b.WriteString(workspace.RenderHostsService())
	b.WriteString("VPSBOX_WS_HOSTS_UNIT_EOF\n")

	// Applying it now is the same operation the unit performs at boot, so there is
	// one code path rather than two that can drift.
	b.WriteString("sudo /bin/sh " + workspace.HostsRestoreScript + "\n\n")

	// Isolation rules, plus the unit that reloads them at boot.
	b.WriteString("echo " + shellQuote("Applying workspace isolation rules") + "\n")
	b.WriteString("sudo mkdir -p /etc/vpsbox\n")
	b.WriteString("sudo tee " + workspace.NFTPath + " >/dev/null <<'VPSBOX_NFT_EOF'\n")
	b.WriteString(workspace.RenderNFT(c.CIDR))
	b.WriteString("VPSBOX_NFT_EOF\n")
	b.WriteString("sudo chmod 644 " + workspace.NFTPath + "\n")

	b.WriteString("sudo tee " + workspace.ServicePath + " >/dev/null <<'VPSBOX_UNIT_EOF'\n")
	b.WriteString(workspace.RenderFirewallService())
	b.WriteString("VPSBOX_UNIT_EOF\n")
	b.WriteString("sudo systemctl daemon-reload\n")
	b.WriteString("sudo systemctl enable " + workspace.HostsServiceName + " >/dev/null 2>&1 || true\n")
	// Replace our table rather than adding to it, so re-running does not stack
	// duplicate rules. Deleting a table that is not there is not an error worth
	// failing on, hence the guard.
	b.WriteString("sudo nft list table inet " + workspace.NFTTable + " >/dev/null 2>&1 && sudo nft delete table inet " + workspace.NFTTable + " || true\n")
	b.WriteString("sudo nft -f " + workspace.NFTPath + "\n")
	b.WriteString("sudo systemctl enable " + workspace.ServiceName + " >/dev/null 2>&1 || true\n\n")

	// Prove the address is actually up rather than trusting that netplan worked.
	b.WriteString("echo " + shellQuote("Verifying the private address") + "\n")
	b.WriteString("ip -4 addr show dev " + c.Interface + " | grep -q " + shellQuote(c.PrivateIP) + " || {\n")
	b.WriteString("  echo 'The private address was not applied. netplan output:' >&2\n")
	b.WriteString("  sudo netplan get 2>&1 >&2 || true\n")
	b.WriteString("  exit 1\n")
	b.WriteString("}\n")
	b.WriteString("echo " + shellQuote("Private address "+c.PrivateIP+" is up") + "\n")

	return b.String()
}

// workspaceTeardownScript removes everything workspaceApplyScript put in place.
//
// Every step tolerates already being undone: teardown runs when a sandbox leaves
// a workspace and when one is destroyed, and it must not fail on a sandbox that
// was only ever half configured.
func workspaceTeardownScript() string {
	var b strings.Builder

	b.WriteString("set -u\n\n")
	b.WriteString("echo " + shellQuote("Removing the private network configuration") + "\n")

	b.WriteString("sudo systemctl disable --now " + workspace.ServiceName + " >/dev/null 2>&1 || true\n")
	b.WriteString("sudo systemctl disable --now " + workspace.HostsServiceName + " >/dev/null 2>&1 || true\n")
	b.WriteString("sudo rm -f " + workspace.ServicePath + " " + workspace.HostsServicePath + "\n")
	b.WriteString("sudo systemctl daemon-reload || true\n")
	b.WriteString("sudo nft delete table inet " + workspace.NFTTable + " >/dev/null 2>&1 || true\n")
	b.WriteString("sudo rm -f " + workspace.NFTPath + " " + workspace.HostsSavePath + " " + workspace.HostsRestoreScript + "\n")
	// Leave no empty directory behind, but never remove one something else put
	// files in.
	b.WriteString("sudo rmdir --ignore-fail-on-non-empty /etc/vpsbox 2>/dev/null || true\n")

	b.WriteString("sudo rm -f " + workspace.NetplanPath + "\n")
	// Bring the interface back to just its DHCP address.
	b.WriteString("sudo netplan apply || true\n")

	b.WriteString(fmt.Sprintf(
		"sudo sed -i '/^%s$/,/^%s$/d' /etc/hosts || true\n",
		regexpEscapeForSed(workspace.HostsStart), regexpEscapeForSed(workspace.HostsEnd)))

	b.WriteString("echo " + shellQuote("Private network configuration removed") + "\n")
	return b.String()
}
