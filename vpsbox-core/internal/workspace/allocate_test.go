package workspace

import (
	"testing"
)

func TestAllocateIndexStartsAtOne(t *testing.T) {
	// 10.42.0.x is skipped: it reads like the base network itself.
	index, err := AllocateIndex(nil)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if index != 1 {
		t.Errorf("first index = %d, want 1", index)
	}
}

func TestAllocateIndexReusesFreedSubnets(t *testing.T) {
	// Destroying a workspace must free its subnet, or indexes drift upward
	// forever on a machine that creates and removes workspaces.
	index, err := AllocateIndex([]int{1, 3, 4})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if index != 2 {
		t.Errorf("index = %d, want the freed 2", index)
	}
}

func TestAllocateIndexSkipsContiguousRuns(t *testing.T) {
	index, err := AllocateIndex([]int{1, 2, 3})
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if index != 4 {
		t.Errorf("index = %d, want 4", index)
	}
}

func TestAllocateIndexExhausted(t *testing.T) {
	used := make([]int, 0, 254)
	for i := 1; i <= 254; i++ {
		used = append(used, i)
	}
	if _, err := AllocateIndex(used); err == nil {
		t.Error("allocating from a full range should fail")
	}
}

func TestCIDRForIndex(t *testing.T) {
	cases := []struct {
		index int
		want  string
	}{
		{1, "10.42.1.0/24"},
		{2, "10.42.2.0/24"},
		{254, "10.42.254.0/24"},
	}

	for _, tc := range cases {
		got, err := CIDRForIndex(tc.index)
		if err != nil {
			t.Fatalf("CIDRForIndex(%d): %v", tc.index, err)
		}
		if got != tc.want {
			t.Errorf("CIDRForIndex(%d) = %q, want %q", tc.index, got, tc.want)
		}
	}

	for _, bad := range []int{0, -1, 255, 1000} {
		if _, err := CIDRForIndex(bad); err == nil {
			t.Errorf("CIDRForIndex(%d) should fail", bad)
		}
	}
}

func TestGatewayForCIDR(t *testing.T) {
	got, err := GatewayForCIDR("10.42.7.0/24")
	if err != nil {
		t.Fatalf("gateway: %v", err)
	}
	if got != "10.42.7.1" {
		t.Errorf("gateway = %q, want 10.42.7.1", got)
	}
}

func TestNextMemberIPSkipsReservedLowRange(t *testing.T) {
	// .1 through .9 are reserved so a future gateway can have the address
	// everyone assumes it has.
	got, err := NextMemberIP("10.42.1.0/24", nil)
	if err != nil {
		t.Fatalf("next ip: %v", err)
	}
	if got != "10.42.1.10" {
		t.Errorf("first member = %q, want 10.42.1.10", got)
	}
}

func TestNextMemberIPFillsGaps(t *testing.T) {
	// A member leaving frees its address for the next one to join.
	got, err := NextMemberIP("10.42.1.0/24", []string{"10.42.1.10", "10.42.1.12"})
	if err != nil {
		t.Fatalf("next ip: %v", err)
	}
	if got != "10.42.1.11" {
		t.Errorf("next member = %q, want the freed 10.42.1.11", got)
	}
}

func TestNextMemberIPSequential(t *testing.T) {
	taken := []string{}
	want := []string{"10.42.3.10", "10.42.3.11", "10.42.3.12"}

	for _, expected := range want {
		got, err := NextMemberIP("10.42.3.0/24", taken)
		if err != nil {
			t.Fatalf("next ip: %v", err)
		}
		if got != expected {
			t.Fatalf("got %q, want %q", got, expected)
		}
		taken = append(taken, got)
	}
}

func TestNextMemberIPExhausted(t *testing.T) {
	taken := make([]string, 0, 245)
	for octet := 10; octet <= 254; octet++ {
		ip, err := NextMemberIP("10.42.1.0/24", taken)
		if err != nil {
			t.Fatalf("unexpected exhaustion at octet %d: %v", octet, err)
		}
		taken = append(taken, ip)
	}
	if _, err := NextMemberIP("10.42.1.0/24", taken); err == nil {
		t.Error("a full subnet should refuse to allocate")
	}
}

func TestNextMemberIPRejectsBadCIDR(t *testing.T) {
	if _, err := NextMemberIP("not-a-cidr", nil); err == nil {
		t.Error("an invalid subnet should fail")
	}
}

func TestContains(t *testing.T) {
	if !Contains("10.42.1.0/24", "10.42.1.10") {
		t.Error("10.42.1.10 should be inside 10.42.1.0/24")
	}
	// This is the isolation property: another workspace's address is outside.
	if Contains("10.42.1.0/24", "10.42.2.10") {
		t.Error("10.42.2.10 must not be inside 10.42.1.0/24")
	}
	if Contains("10.42.1.0/24", "not-an-ip") {
		t.Error("garbage must not be reported as inside")
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"alpha", "web-tier", "prod2", "a1", "my-long-workspace-name"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct {
		name   string
		reason string
	}{
		{"", "empty"},
		{"A", "too short and uppercase"},
		{"a", "single character"},
		{"Alpha", "uppercase"},
		{"1alpha", "starts with a digit"},
		{"-alpha", "starts with a hyphen"},
		{"alpha-", "ends with a hyphen"},
		{"my_workspace", "underscore"},
		{"my workspace", "space"},
		{"my.workspace", "dot would break the DNS label"},
		{"this-workspace-name-is-far-too-long-to-be-valid", "too long"},
	}
	for _, tc := range invalid {
		if err := ValidateName(tc.name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want an error (%s)", tc.name, tc.reason)
		}
	}
}

func TestSortMembersOrdersNumericallyNotLexically(t *testing.T) {
	// Lexical sorting would put .100 before .11 and make every re-render look
	// like a change.
	members := []Member{
		{Instance: "c", PrivateIP: "10.42.1.100"},
		{Instance: "a", PrivateIP: "10.42.1.11"},
		{Instance: "b", PrivateIP: "10.42.1.9"},
	}

	sorted := SortMembers(members)
	want := []string{"10.42.1.9", "10.42.1.11", "10.42.1.100"}
	for i, expected := range want {
		if sorted[i].PrivateIP != expected {
			t.Errorf("position %d = %q, want %q", i, sorted[i].PrivateIP, expected)
		}
	}
}

func TestSortMembersDoesNotMutateInput(t *testing.T) {
	members := []Member{
		{Instance: "b", PrivateIP: "10.42.1.20"},
		{Instance: "a", PrivateIP: "10.42.1.10"},
	}
	_ = SortMembers(members)
	if members[0].Instance != "b" {
		t.Error("SortMembers mutated its input")
	}
}

func TestHostNamesFor(t *testing.T) {
	names := HostNamesFor("db", "alpha")
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2", len(names))
	}
	if names[0] != "db" {
		t.Errorf("short name = %q, want db", names[0])
	}
	if names[1] != "db.alpha.vpsbox.internal" {
		t.Errorf("fqdn = %q, want db.alpha.vpsbox.internal", names[1])
	}
}
