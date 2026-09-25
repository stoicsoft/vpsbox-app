// Package workspace models private networks shared by a group of sandboxes,
// after the way a cloud provider's private networking works: each workspace gets
// a subnet, each member gets a stable address on it, and members of different
// workspaces cannot reach each other.
//
// Everything here is pure — address allocation and config rendering — so the
// arithmetic and the generated files can be tested without a VM. Applying any of
// it lives in internal/app.
package workspace

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

const (
	// BaseNetwork is carved into one /24 per workspace. 10.42 is used rather than
	// anything in 192.168 because the hypervisor already owns a 192.168 subnet for
	// the sandboxes themselves, and a home LAN very often owns another.
	BaseNetwork = "10.42.0.0/16"

	// PrefixLen is the size of each workspace's subnet: 254 usable addresses,
	// far more than a laptop will ever run.
	PrefixLen = 24

	// firstMemberOctet leaves .1 through .9 free. .1 is what everyone expects a
	// gateway to be, and the rest are room for future infrastructure addresses
	// that should not collide with members.
	firstMemberOctet = 10

	// lastMemberOctet stops before the broadcast address.
	lastMemberOctet = 254

	// minIndex and maxIndex bound the third octet, so workspace subnets run from
	// 10.42.1.0/24 to 10.42.254.0/24. Index 0 is skipped: 10.42.0.x reads like the
	// base network itself and invites confusion.
	minIndex = 1
	maxIndex = 254

	// InternalSuffix is the DNS suffix for names inside a workspace. It is not a
	// real TLD, which is the point — these names only resolve inside the network.
	InternalSuffix = "vpsbox.internal"
)

// safeName keeps workspace names usable as DNS labels, since they appear in
// member hostnames as <instance>.<workspace>.vpsbox.internal.
// Two characters is the floor, so short names like "db" or "qa" are usable.
var safeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$`)

// ValidateName checks a workspace name. The rules are DNS label rules, because
// the name becomes one.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace name is required")
	}
	if len(name) > 32 {
		return fmt.Errorf("workspace name %q is longer than 32 characters", name)
	}
	if !safeName.MatchString(name) {
		return fmt.Errorf("workspace name %q must be lowercase letters, digits and hyphens, start with a letter, and end with a letter or digit", name)
	}
	return nil
}

// AllocateIndex picks the lowest subnet index not already in use, so removing a
// workspace frees its subnet for the next one rather than drifting upward.
func AllocateIndex(used []int) (int, error) {
	taken := make(map[int]bool, len(used))
	for _, index := range used {
		taken[index] = true
	}

	for index := minIndex; index <= maxIndex; index++ {
		if !taken[index] {
			return index, nil
		}
	}
	return 0, fmt.Errorf("no free private network left in %s (all %d are in use)", BaseNetwork, maxIndex-minIndex+1)
}

// CIDRForIndex returns the subnet for a workspace index, e.g. 10.42.1.0/24.
func CIDRForIndex(index int) (string, error) {
	if index < minIndex || index > maxIndex {
		return "", fmt.Errorf("workspace index %d is outside %d-%d", index, minIndex, maxIndex)
	}
	return fmt.Sprintf("10.42.%d.0/%d", index, PrefixLen), nil
}

// GatewayForCIDR returns the .1 address of a workspace subnet. Nothing serves it
// today; it is reserved so a future router or ingress has the address everyone
// already assumes it will have.
func GatewayForCIDR(cidr string) (string, error) {
	base, err := networkBase(cidr)
	if err != nil {
		return "", err
	}
	base[3] = 1
	return base.String(), nil
}

// NextMemberIP returns the lowest free address in the subnet, skipping the
// reserved low range and anything already handed out.
func NextMemberIP(cidr string, taken []string) (string, error) {
	base, err := networkBase(cidr)
	if err != nil {
		return "", err
	}

	used := make(map[string]bool, len(taken))
	for _, address := range taken {
		used[address] = true
	}

	for octet := firstMemberOctet; octet <= lastMemberOctet; octet++ {
		candidate := make(net.IP, len(base))
		copy(candidate, base)
		candidate[3] = byte(octet)
		if !used[candidate.String()] {
			return candidate.String(), nil
		}
	}
	return "", fmt.Errorf("no free address left in %s", cidr)
}

// Contains reports whether an address falls inside a workspace's subnet.
func Contains(cidr, address string) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	parsed := net.ParseIP(address)
	if parsed == nil {
		return false
	}
	return network.Contains(parsed)
}

// networkBase returns the 4-byte network address of an IPv4 CIDR.
func networkBase(cidr string) (net.IP, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace subnet %q: %w", cidr, err)
	}
	base := network.IP.To4()
	if base == nil {
		return nil, fmt.Errorf("workspace subnet %q is not IPv4", cidr)
	}
	out := make(net.IP, len(base))
	copy(out, base)
	return out, nil
}

// Member is one sandbox on the network, as the renderers need it.
type Member struct {
	Instance  string
	PrivateIP string
}

// SortMembers orders members by address so generated files are stable — a
// re-render that only reorders lines would otherwise look like a real change.
func SortMembers(members []Member) []Member {
	out := make([]Member, len(members))
	copy(out, members)
	sort.Slice(out, func(i, j int) bool {
		left := net.ParseIP(out[i].PrivateIP).To4()
		right := net.ParseIP(out[j].PrivateIP).To4()
		if left == nil || right == nil {
			return out[i].Instance < out[j].Instance
		}
		for octet := 0; octet < 4; octet++ {
			if left[octet] != right[octet] {
				return left[octet] < right[octet]
			}
		}
		return out[i].Instance < out[j].Instance
	})
	return out
}

// HostNamesFor returns the names a member answers to inside the workspace: the
// bare sandbox name for convenience, and the fully qualified one that cannot
// collide with anything else on the box.
func HostNamesFor(instance, workspaceName string) []string {
	return []string{
		instance,
		strings.Join([]string{instance, workspaceName, InternalSuffix}, "."),
	}
}
