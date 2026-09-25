package app

import (
	"strings"
	"testing"
)

func TestParseDefaultGateway(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want string
		ok   bool
	}{
		{
			name: "multipass on macos",
			out:  "default via 192.168.64.1 dev enp0s1 proto dhcp src 192.168.64.12 metric 100",
			want: "192.168.64.1",
			ok:   true,
		},
		{
			name: "multipass on linux",
			out:  "default via 10.85.12.1 dev ens3 proto dhcp src 10.85.12.44 metric 100",
			want: "10.85.12.1",
			ok:   true,
		},
		{
			name: "trailing newline and whitespace",
			out:  "\n  default via 192.168.64.1 dev enp0s1 \n",
			want: "192.168.64.1",
			ok:   true,
		},
		{
			name: "two default routes takes the first listed",
			out: "default via 192.168.64.1 dev enp0s1 proto dhcp metric 100\n" +
				"default via 10.0.0.1 dev enp0s2 proto dhcp metric 200",
			want: "192.168.64.1",
			ok:   true,
		},
		{
			name: "link scoped default with no gateway",
			out:  "default dev tun0 scope link",
			want: "",
			ok:   false,
		},
		{
			name: "empty",
			out:  "",
			want: "",
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseDefaultGateway(tc.out)
			if ok != tc.ok {
				t.Fatalf("parseDefaultGateway ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("parseDefaultGateway = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHostsBlockIsFencedAndCoversEveryBucket(t *testing.T) {
	cfg := attachConfig{
		Gateway: "192.168.64.1",
		Domain:  "buckets.vpsbox.local",
		Buckets: []string{"backups", "uploads"},
	}

	block := cfg.hostsBlock()

	if !strings.HasPrefix(block, objstoreHostsStart) {
		t.Errorf("block does not open with the start marker:\n%s", block)
	}
	if !strings.HasSuffix(block, objstoreHostsEnd) {
		t.Errorf("block does not close with the end marker:\n%s", block)
	}

	// The bare domain carries path-style addressing; per-bucket names carry
	// subdomain style, which /etc/hosts cannot wildcard.
	for _, want := range []string{
		"buckets.vpsbox.local",
		"backups.buckets.vpsbox.local",
		"uploads.buckets.vpsbox.local",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q:\n%s", want, block)
		}
	}

	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if !strings.HasPrefix(line, cfg.Gateway+" ") {
			t.Errorf("host line does not start with the gateway: %q", line)
		}
	}
}

func TestHostsBlockWithNoBucketsStillMapsTheDomain(t *testing.T) {
	cfg := attachConfig{
		Gateway: "192.168.64.1",
		Domain:  "buckets.vpsbox.local",
	}

	block := cfg.hostsBlock()

	if !strings.Contains(block, "192.168.64.1 buckets.vpsbox.local") {
		t.Errorf("empty bucket list should still map the domain:\n%s", block)
	}
}

func TestHostsBlockWrapsLongBucketLists(t *testing.T) {
	cfg := attachConfig{
		Gateway: "192.168.64.1",
		Domain:  "buckets.vpsbox.local",
		Buckets: []string{"one", "two", "three", "four", "five", "six", "seven"},
	}

	block := cfg.hostsBlock()

	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		// Gateway plus at most four names per line.
		if fields := strings.Fields(line); len(fields) > 5 {
			t.Errorf("host line carries %d names, want at most 4: %q", len(fields)-1, line)
		}
	}

	// Every name must still be present after wrapping.
	for _, bucket := range cfg.Buckets {
		if !strings.Contains(block, bucket+".buckets.vpsbox.local") {
			t.Errorf("wrapping dropped %q:\n%s", bucket, block)
		}
	}
}

func testAttachConfig() attachConfig {
	return attachConfig{
		Gateway:   "192.168.64.1",
		Domain:    "buckets.vpsbox.local",
		Endpoint:  "http://buckets.vpsbox.local:9000",
		Region:    "us-east-1",
		AccessKey: "vpsboxAABBCCDDEEFF00112233",
		SecretKey: "0123456789abcdef0123456789abcdef01234567",
		Buckets:   []string{"backups"},
	}
}

func TestAttachScriptWritesCredentialsWhereClientsLookForThem(t *testing.T) {
	script := attachScript(testAttachConfig())

	// ~/.aws/credentials is what makes this work for cron jobs and scripts, which
	// never source /etc/profile.d.
	if !strings.Contains(script, `"$HOME/.aws/credentials"`) {
		t.Error("script does not write ~/.aws/credentials")
	}
	if !strings.Contains(script, "aws_access_key_id = vpsboxAABBCCDDEEFF00112233") {
		t.Error("script does not write the access key")
	}
	if !strings.Contains(script, "aws_secret_access_key = 0123456789abcdef0123456789abcdef01234567") {
		t.Error("script does not write the secret key")
	}
	if !strings.Contains(script, `chmod 600 "$HOME/.aws/credentials"`) {
		t.Error("credentials file is not restricted to the owner")
	}
}

func TestAttachScriptKeepsSecretsOutOfProfileD(t *testing.T) {
	cfg := testAttachConfig()
	script := attachScript(cfg)

	start := strings.Index(script, "VPSBOX_PROFILE_EOF")
	if start < 0 {
		t.Fatal("script has no profile.d block")
	}
	end := strings.Index(script[start+1:], "VPSBOX_PROFILE_EOF")
	if end < 0 {
		t.Fatal("profile.d heredoc is not closed")
	}
	profileBlock := script[start : start+1+end]

	// /etc/profile.d is world readable, so the secret must not appear there.
	if strings.Contains(profileBlock, cfg.SecretKey) {
		t.Error("secret key leaked into the world-readable profile.d script")
	}
	if !strings.Contains(profileBlock, "AWS_ENDPOINT_URL="+cfg.Endpoint) {
		t.Error("profile.d block does not export the endpoint")
	}
}

func TestAttachScriptInstallsZeroFlagWrapper(t *testing.T) {
	cfg := testAttachConfig()
	script := attachScript(cfg)

	if !strings.Contains(script, objstoreWrapperPath) {
		t.Error("script does not install the s3 wrapper")
	}
	// The endpoint has to be baked in: profile.d is not sourced for
	// `ssh box 'command'`, so an env-var lookup would be empty there.
	if !strings.Contains(script, "exec aws --endpoint-url "+cfg.Endpoint+` s3 "$@"`) {
		t.Error("wrapper does not pin the endpoint literally")
	}
	// Never clobber an unrelated binary the user put at that path.
	if !strings.Contains(script, objstoreWrapperMarker) {
		t.Error("wrapper carries no ownership marker")
	}
	if !strings.Contains(script, "if [ ! -e "+objstoreWrapperPath+" ]") {
		t.Error("script overwrites the wrapper path unconditionally")
	}
}

func TestAttachScriptReplacesItsOwnHostsBlock(t *testing.T) {
	script := attachScript(testAttachConfig())

	// Without the delete, re-attaching would stack a second copy of the block.
	if !strings.Contains(script, "sudo sed -i") {
		t.Error("script does not remove the previous hosts block")
	}
	if !strings.Contains(script, "sudo tee -a /etc/hosts") {
		t.Error("script does not append the new hosts block")
	}
	if strings.Count(script, objstoreHostsStart) == 0 {
		t.Error("script does not emit the hosts start marker")
	}
}

func TestAttachScriptVerifiesTheConnection(t *testing.T) {
	cfg := testAttachConfig()
	script := attachScript(cfg)

	// Claiming success without checking is how a broken firewall rule turns into
	// a bug report instead of an error message.
	if !strings.Contains(script, "aws --endpoint-url "+cfg.Endpoint+" s3 ls") {
		t.Error("script does not verify connectivity before reporting success")
	}
	if !strings.HasPrefix(script, "set -eu") {
		t.Error("script does not fail fast — a mid-script failure would go unnoticed")
	}
}

func TestAttachScriptInstallsAwsCliOnlyWhenMissing(t *testing.T) {
	script := attachScript(testAttachConfig())

	if !strings.Contains(script, "if ! command -v aws >/dev/null 2>&1; then") {
		t.Error("script does not guard the AWS CLI install")
	}
	if !strings.Contains(script, "apt-get install -y -qq awscli") {
		t.Error("script does not install the AWS CLI")
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"plain", "'plain'"},
		{"with space", "'with space'"},
		{"it's", `'it'\''s'`},
	}

	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
