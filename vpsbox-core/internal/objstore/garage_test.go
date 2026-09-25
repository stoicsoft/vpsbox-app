package objstore

import (
	"strings"
	"testing"
)

// Real output from `garage node id` on 2.3.0 — the ID line is followed by prose
// that must not be mistaken for it.
const nodeIDOutput = `915ea2a3ce71ff0953d198852c5e602fe4b013f904ff3ff7709ce9b8c8170343@127.0.0.1:13901

To instruct a node to connect to this node, run the following command on that node:
    garage [-c <config file path>] node connect 915ea2a3ce71ff09@127.0.0.1:13901
`

func TestParseNodeID(t *testing.T) {
	got, ok := parseNodeID(nodeIDOutput)
	if !ok {
		t.Fatal("parseNodeID found no id")
	}
	want := "915ea2a3ce71ff0953d198852c5e602fe4b013f904ff3ff7709ce9b8c8170343"
	if got != want {
		t.Errorf("parseNodeID = %q, want %q", got, want)
	}
}

func TestParseNodeIDRejectsProse(t *testing.T) {
	cases := []string{
		"",
		"no at sign here",
		// An "@" inside prose must not be read as a node id.
		"contact us at support@example.com for help",
	}

	for _, out := range cases {
		if got, ok := parseNodeID(out); ok {
			t.Errorf("parseNodeID(%q) = %q, want no match", out, got)
		}
	}
}

// Real output from `garage layout show` after a layout has been applied.
const layoutShowOutput = `==== CURRENT CLUSTER LAYOUT ====
ID                Tags  Zone   Capacity  Usable capacity
915ea2a3ce71ff09  []    local  9.3 GiB   9.3 GiB (100.0%)

Zone redundancy: maximum

Current cluster layout version: 1
`

func TestParseLayoutVersion(t *testing.T) {
	got, ok := parseLayoutVersion(layoutShowOutput)
	if !ok {
		t.Fatal("parseLayoutVersion found no version")
	}
	if got != 1 {
		t.Errorf("parseLayoutVersion = %d, want 1", got)
	}
}

func TestParseLayoutVersionMissing(t *testing.T) {
	if _, ok := parseLayoutVersion("==== CURRENT CLUSTER LAYOUT ====\nno version here\n"); ok {
		t.Error("parseLayoutVersion reported a version where there is none")
	}
}

// Real output from `garage status` on a node that has not been given a role.
const statusUnassignedOutput = `==== HEALTHY NODES ====
ID                Hostname   Address          Tags  Zone  Capacity          DataAvail  Version
915ea2a3ce71ff09  localhost  127.0.0.1:13901              NO ROLE ASSIGNED             cargo:2.3.0
`

const statusAssignedOutput = `==== HEALTHY NODES ====
ID                Hostname   Address          Tags  Zone   Capacity  DataAvail            Version
915ea2a3ce71ff09  localhost  127.0.0.1:13901  []    local  9.3 GiB   1.2 TiB (75.0%)      cargo:2.3.0
`

func TestLayoutUnassigned(t *testing.T) {
	if !layoutUnassigned(statusUnassignedOutput) {
		t.Error("a node with no role was not detected as unassigned")
	}
	if layoutUnassigned(statusAssignedOutput) {
		t.Error("a node with a role was reported as unassigned")
	}
}

func TestRenderGarageConfigKeepsAdminOffTheNetwork(t *testing.T) {
	cfg := garageConfig{
		MetaDir:    "/home/u/.vpsbox/objstore/meta",
		DataDir:    "/home/u/.vpsbox/objstore/data",
		APIPort:    9000,
		RPCPort:    9001,
		AdminPort:  9002,
		Region:     "us-east-1",
		RootDomain: "buckets.vpsbox.local",
		RPCSecret:  "aaaa",
		AdminToken: "bbbb",
	}

	out := renderGarageConfig(cfg)

	// The S3 API has to be reachable from the sandbox over the private bridge.
	if !strings.Contains(out, `api_bind_addr = "0.0.0.0:9000"`) {
		t.Error("S3 API is not reachable from a sandbox")
	}
	// RPC and admin must not be. Anyone who can reach admin owns the store.
	if !strings.Contains(out, `rpc_bind_addr = "127.0.0.1:9001"`) {
		t.Error("RPC listener is not confined to loopback")
	}
	if !strings.Contains(out, `api_bind_addr = "127.0.0.1:9002"`) {
		t.Error("admin listener is not confined to loopback")
	}
}

func TestRenderGarageConfigIsSingleNodeSafe(t *testing.T) {
	out := renderGarageConfig(garageConfig{Region: "us-east-1", RootDomain: "buckets.vpsbox.local"})

	// Asking for more than one replica on a one-node cluster leaves it
	// permanently degraded and refusing writes.
	if !strings.Contains(out, "replication_factor = 1") {
		t.Error("config does not request single-copy replication")
	}
}

func TestRenderGarageConfigEnablesSubdomainAddressing(t *testing.T) {
	out := renderGarageConfig(garageConfig{Region: "us-east-1", RootDomain: "buckets.vpsbox.local"})

	// The leading dot is what makes <bucket>.buckets.vpsbox.local resolve to a
	// bucket rather than being taken literally as a host.
	if !strings.Contains(out, `root_domain = ".buckets.vpsbox.local"`) {
		t.Errorf("root_domain is not set for subdomain addressing:\n%s", out)
	}
}

func TestRenderGarageConfigQuotesPathsWithSpaces(t *testing.T) {
	out := renderGarageConfig(garageConfig{
		MetaDir:    `/Users/a b/.vpsbox/objstore/meta`,
		DataDir:    `/Users/a b/.vpsbox/objstore/data`,
		Region:     "us-east-1",
		RootDomain: "buckets.vpsbox.local",
	})

	// macOS home directories can contain spaces; an unquoted path is invalid TOML.
	if !strings.Contains(out, `metadata_dir = "/Users/a b/.vpsbox/objstore/meta"`) {
		t.Errorf("paths with spaces are not quoted:\n%s", out)
	}
}

func TestGenerateCredentialsMatchServerKeyFormat(t *testing.T) {
	accessKey, secretKey, err := generateCredentials()
	if err != nil {
		t.Fatalf("generate credentials: %v", err)
	}

	// The server mints keys as "GK" + 24 hex characters; matching that shape keeps
	// vpsbox-created keys indistinguishable from native ones.
	if !strings.HasPrefix(accessKey, "GK") {
		t.Errorf("access key %q does not start with GK", accessKey)
	}
	if len(accessKey) != 26 {
		t.Errorf("access key length = %d, want 26 (GK + 24 hex)", len(accessKey))
	}
	if !isHex(strings.TrimPrefix(accessKey, "GK")) {
		t.Errorf("access key %q is not GK plus hex", accessKey)
	}
	if len(secretKey) != 64 || !isHex(secretKey) {
		t.Errorf("secret key is %d chars, want 64 hex", len(secretKey))
	}
}

func TestIsHex(t *testing.T) {
	for _, valid := range []string{"deadbeef", "0123456789abcdef", "ABCDEF"} {
		if !isHex(valid) {
			t.Errorf("isHex(%q) = false, want true", valid)
		}
	}
	for _, invalid := range []string{"", "xyz", "dead beef", "dead-beef", "127.0.0.1"} {
		if isHex(invalid) {
			t.Errorf("isHex(%q) = true, want false", invalid)
		}
	}
}
