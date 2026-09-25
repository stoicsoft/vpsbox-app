package workspace

import (
	"fmt"
	"strings"
)

const (
	// NetplanPath is a drop-in rather than a rewrite of the image's own netplan
	// file. Netplan merges files by interface, so declaring only `addresses` here
	// adds the private address while leaving the DHCP config that brings up the
	// primary address untouched. The 99- prefix makes it merge last.
	NetplanPath = "/etc/netplan/99-vpsbox-workspace.yaml"

	// HostsStart and HostsEnd fence the block we manage in a member's /etc/hosts,
	// so re-syncing replaces our entries instead of stacking another copy.
	HostsStart = "# >>> vpsbox workspace >>>"
	HostsEnd   = "# <<< vpsbox workspace <<<"

	// NFTPath holds the firewall rules. It is our own file — the rules live in a
	// dedicated table so loading them never disturbs anything else the user has.
	NFTPath = "/etc/vpsbox/workspace.nft"

	// NFTTable is the table we own. Everything in it can be replaced wholesale
	// without touching other tables, which is what makes this safe to re-apply.
	NFTTable = "vpsbox_workspace"

	// ServiceName reloads the rules at boot. nftables rules are not persistent on
	// their own, and a firewall that quietly disappears on reboot is worse than no
	// firewall, because you stop checking.
	ServiceName = "vpsbox-workspace-fw.service"
	ServicePath = "/etc/systemd/system/" + ServiceName

	// HostsSavePath is where the names block is kept so it can be restored.
	//
	// The cloud image sets cloud-init's manage_etc_hosts, and its update_etc_hosts
	// module runs on every boot — it regenerates /etc/hosts from a template and
	// takes our block with it. The address and the firewall survive a reboot on
	// their own; without this, the names would silently stop resolving and only
	// the names.
	HostsSavePath = "/etc/vpsbox/workspace-hosts"

	// HostsRestoreScript re-applies the saved block. Kept as a file so the unit
	// stays a one-liner and the logic is inspectable on the sandbox.
	HostsRestoreScript = "/etc/vpsbox/workspace-hosts-apply.sh"

	// HostsServiceName restores the names after cloud-init has finished rewriting
	// /etc/hosts. It is a separate unit from the firewall because the two need
	// opposite orderings: rules belong before the network comes up, names belong
	// after cloud-init has had its turn.
	HostsServiceName = "vpsbox-workspace-hosts.service"
	HostsServicePath = "/etc/systemd/system/" + HostsServiceName
)

// RenderNetplan produces the drop-in that gives a member its private address.
//
// netdefKey must be the key of the *existing* ethernet definition, not the
// interface name — on the stock cloud image they are different, and getting this
// wrong fails silently rather than loudly. That image declares its NIC as:
//
//	ethernets:
//	  default:
//	    match: {macaddress: "..."}
//	    dhcp4: true
//
// Netplan merges by key, so a drop-in keyed "enp0s1" is a *second* definition
// claiming the same NIC. Netplan accepts it without complaint and emits two
// systemd-networkd files — one matching the MAC with DHCP, one matching the name
// with the static address. Only the lexically first is applied, so the address
// simply never appears; had the names sorted the other way the sandbox would
// have lost DHCP and gone unreachable instead.
//
// Reusing the existing key merges properly: one definition, DHCP and the static
// address together. Discover it with `netplan get ethernets`.
//
// The address is written to disk rather than added with `ip addr add` so it
// survives a reboot — a private network that vanishes when the server restarts
// would teach exactly the wrong lesson.
func RenderNetplan(netdefKey, privateIP string) string {
	var b strings.Builder
	b.WriteString("# Managed by vpsbox. Regenerated on every workspace sync.\n")
	b.WriteString("#\n")
	b.WriteString("# Adds this sandbox's private workspace address. The key below is the\n")
	b.WriteString("# image's own ethernet definition, so netplan merges this address into it\n")
	b.WriteString("# instead of declaring a second, competing definition for the same NIC.\n")
	b.WriteString("network:\n")
	b.WriteString("  version: 2\n")
	b.WriteString("  ethernets:\n")
	fmt.Fprintf(&b, "    %s:\n", netdefKey)
	b.WriteString("      addresses:\n")
	fmt.Fprintf(&b, "        - %s/%d\n", privateIP, PrefixLen)
	return b.String()
}

// RenderHostsBlock produces the fenced /etc/hosts section naming every member of
// the workspace. /etc/hosts is used rather than a resolver because it needs no
// daemon, and the member list is small and changes only when someone joins.
func RenderHostsBlock(workspaceName string, members []Member) string {
	var b strings.Builder
	b.WriteString(HostsStart + "\n")
	b.WriteString("# Members of the " + workspaceName + " workspace. Managed by vpsbox.\n")
	for _, member := range SortMembers(members) {
		names := HostNamesFor(member.Instance, workspaceName)
		b.WriteString(member.PrivateIP + " " + strings.Join(names, " ") + "\n")
	}
	b.WriteString(HostsEnd)
	return b.String()
}

// RenderNFT produces the firewall rules for a member.
//
// The shape matters. The table is flushed and rebuilt so re-applying is
// idempotent, the chain's policy is accept so we never become responsible for
// traffic we did not intend to govern, and the only drop is for the rest of the
// private range. That is the whole isolation rule: talk to your own workspace,
// not to anyone else's.
//
// This is a cooperative boundary, not a security one — the sandbox is root on
// itself and can flush these rules. It mirrors how a cloud private network
// actually behaves: the provider gives you addressing, and the firewall is yours.
func RenderNFT(ownCIDR string) string {
	var b strings.Builder
	b.WriteString("#!/usr/sbin/nft -f\n")
	b.WriteString("# Managed by vpsbox. Regenerated on every workspace sync.\n")
	b.WriteString("#\n")
	b.WriteString("# Members of this workspace can reach this host. Hosts on another\n")
	b.WriteString("# workspace's subnet cannot. Everything outside the private range is\n")
	b.WriteString("# left alone — this table governs workspace traffic only.\n\n")

	fmt.Fprintf(&b, "table inet %s {\n", NFTTable)
	b.WriteString("  chain input {\n")
	// Priority 0 with an accept policy: we sit alongside whatever else is
	// filtering rather than in front of it.
	b.WriteString("    type filter hook input priority 0; policy accept;\n\n")
	b.WriteString("    # Replies to connections this host opened are always allowed, or\n")
	b.WriteString("    # blocking inbound would also break outbound.\n")
	b.WriteString("    ct state established,related accept\n\n")
	fmt.Fprintf(&b, "    # Our own workspace.\n")
	fmt.Fprintf(&b, "    ip saddr %s accept\n\n", ownCIDR)
	b.WriteString("    # Any other workspace. This is the isolation.\n")
	fmt.Fprintf(&b, "    ip saddr %s drop\n", BaseNetwork)
	b.WriteString("  }\n")
	b.WriteString("}\n")
	return b.String()
}

// RenderHostsRestoreScript produces the script that puts the names block back
// into /etc/hosts. It strips any existing block first, so running it twice is the
// same as running it once.
//
// The markers are literal constants with no regular-expression metacharacters,
// so they need no escaping in the sed address.
func RenderHostsRestoreScript() string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# Managed by vpsbox. Re-applies the workspace names block to /etc/hosts,\n")
	b.WriteString("# which cloud-init regenerates from a template on every boot.\n")
	b.WriteString("set -u\n\n")
	fmt.Fprintf(&b, "[ -f %s ] || exit 0\n", HostsSavePath)
	fmt.Fprintf(&b, "sed -i '/^%s$/,/^%s$/d' /etc/hosts\n", HostsStart, HostsEnd)
	fmt.Fprintf(&b, "cat %s >> /etc/hosts\n", HostsSavePath)
	return b.String()
}

// RenderHostsService produces the unit that restores the names at boot, ordered
// after cloud-init so it does not get overwritten a moment later.
func RenderHostsService() string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=vpsbox workspace hostnames\n")
	// cloud-init's update_etc_hosts runs in cloud-init.service; running before it
	// would put the block back only for cloud-init to strip it again.
	b.WriteString("After=cloud-init.service\n")
	b.WriteString("Wants=cloud-init.service\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	b.WriteString("RemainAfterExit=yes\n")
	fmt.Fprintf(&b, "ExecStart=/bin/sh %s\n", HostsRestoreScript)
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	return b.String()
}

// RenderFirewallService produces the unit that reloads the rules at boot.
func RenderFirewallService() string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=vpsbox workspace network isolation\n")
	b.WriteString("After=network-pre.target\n")
	b.WriteString("Wants=network-pre.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	b.WriteString("RemainAfterExit=yes\n")
	fmt.Fprintf(&b, "ExecStart=/usr/sbin/nft -f %s\n", NFTPath)
	// Tearing down only our table on stop leaves any other firewall intact.
	fmt.Fprintf(&b, "ExecStop=/usr/sbin/nft delete table inet %s\n", NFTTable)
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	return b.String()
}
