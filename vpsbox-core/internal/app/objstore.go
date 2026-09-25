package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/objstore"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

const (
	// objstoreHostsStart and objstoreHostsEnd fence the block we manage inside the
	// sandbox's /etc/hosts, so re-attaching replaces our entries instead of
	// stacking a fresh copy on every run.
	objstoreHostsStart = "# >>> vpsbox objstore >>>"
	objstoreHostsEnd   = "# <<< vpsbox objstore <<<"

	// objstoreProfilePath is where the endpoint is exported for interactive
	// shells. Credentials deliberately do not go here — profile.d is world
	// readable, and every S3 client reads ~/.aws/credentials anyway.
	objstoreProfilePath = "/etc/profile.d/vpsbox-objstore.sh"

	// objstoreWrapperPath is a tiny `s3` command that pins --endpoint-url. The
	// AWS CLI only honours the endpoint_url config key from v2.13 on, and Ubuntu
	// still ships v1 in places, so the wrapper is what actually makes
	// "no flags needed" true across versions.
	objstoreWrapperPath = "/usr/local/bin/s3"

	// objstoreWrapperMarker identifies a wrapper we wrote, so we never clobber an
	// unrelated `s3` binary the user put there.
	objstoreWrapperMarker = "vpsbox-managed-s3-wrapper"
)

// BucketAccess is everything needed to talk to the local object store, from both
// sides: the host uses HostEndpoint, a sandbox uses Endpoint.
type BucketAccess struct {
	Endpoint     string // as reached from inside a sandbox
	HostEndpoint string // as reached from this machine
	Region       string
	AccessKey    string
	SecretKey    string
	Running      bool
	Buckets      []registry.Bucket
}

// BucketEndpoint returns the object store's access details, starting the server
// if it is not up.
func (m *Manager) BucketEndpoint(ctx context.Context) (*BucketAccess, error) {
	store, err := m.objstore.Credentials(ctx, nil)
	if err != nil {
		return nil, err
	}
	buckets, err := m.objstore.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	return accessFrom(store, buckets, true), nil
}

// BucketStatus reports the object store without starting it — for status displays
// and UI polls that must not have side effects.
func (m *Manager) BucketStatus(ctx context.Context) (*BucketAccess, error) {
	store, running, err := m.objstore.Status()
	if err != nil {
		return nil, err
	}
	buckets, err := m.objstore.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return &BucketAccess{Region: "us-east-1", Buckets: buckets}, nil
	}
	return accessFrom(store, buckets, running), nil
}

func accessFrom(store *registry.ObjStore, buckets []registry.Bucket, running bool) *BucketAccess {
	return &BucketAccess{
		Endpoint:     fmt.Sprintf("http://%s:%d", objstore.LocalDomain, store.Port),
		HostEndpoint: store.Endpoint,
		Region:       store.Region,
		AccessKey:    store.AccessKey,
		SecretKey:    store.SecretKey,
		Running:      running,
		Buckets:      buckets,
	}
}

// CreateBucket makes a bucket in the local object store. The server is started on
// demand, so this is the only command a user needs to get storage.
func (m *Manager) CreateBucket(ctx context.Context, name string, progress func(string)) (*registry.Bucket, error) {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}

	if err := objstore.ValidateBucketName(name); err != nil {
		return nil, err
	}

	bucket, err := m.objstore.CreateBucket(ctx, name, report)
	if err != nil {
		return nil, err
	}
	report(fmt.Sprintf("Bucket %q is ready", name))
	return bucket, nil
}

// ListBuckets returns the buckets in the local object store.
func (m *Manager) ListBuckets(ctx context.Context) ([]registry.Bucket, error) {
	return m.objstore.ListBuckets(ctx)
}

// BucketsInstalled reports whether the object storage server binary is present,
// so a UI can offer to install it before the user asks for a bucket.
func (m *Manager) BucketsInstalled() bool {
	return m.objstore.Installed()
}

// DestroyBucket removes a bucket. Without force it refuses a bucket that still
// holds objects, because unlike a sandbox there is no snapshot to restore from.
func (m *Manager) DestroyBucket(ctx context.Context, name string, force bool) error {
	return m.objstore.DeleteBucket(ctx, name, force)
}

// StopBuckets shuts the object store server down, leaving the data on disk.
func (m *Manager) StopBuckets() error {
	return m.objstore.Stop()
}

// AttachBuckets wires the local object store into a sandbox: it teaches the
// sandbox how to reach the host, writes S3 credentials the standard clients read
// without being told to, and installs a zero-flag `s3` command. After this,
// `pg_dump ... | s3 cp - s3://backups/db.sql` works inside the box.
//
// It is idempotent, and re-running it is how a sandbox picks up buckets created
// since the last attach.
func (m *Manager) AttachBuckets(ctx context.Context, name string, status, output func(string)) (*BucketAccess, error) {
	report := func(message string) {
		if status != nil {
			status(message)
		}
	}

	instance, err := m.requireRunningInstance(ctx, name)
	if err != nil {
		return nil, err
	}

	store, err := m.objstore.Credentials(ctx, report)
	if err != nil {
		return nil, err
	}
	buckets, err := m.objstore.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}

	// The sandbox reaches this machine over the hypervisor's private bridge. The
	// host-side address of that bridge differs per platform and per network, so
	// ask the sandbox itself rather than guessing.
	report("Finding the host address from inside the sandbox…")
	routeOut, routeErr, err := m.runRemoteOn(ctx, instance, "ip route show default")
	if err != nil {
		return nil, fmt.Errorf("find host address: %w\n%s", err, strings.TrimSpace(routeErr))
	}
	gateway, ok := parseDefaultGateway(routeOut)
	if !ok {
		return nil, fmt.Errorf("could not work out this machine's address from the sandbox's routing table:\n%s", strings.TrimSpace(routeOut))
	}

	names := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		names = append(names, bucket.Name)
	}

	report(fmt.Sprintf("Wiring %s to the object store at %s…", instance.Name, gateway))
	script := attachScript(attachConfig{
		Gateway:   gateway,
		Domain:    objstore.LocalDomain,
		Endpoint:  fmt.Sprintf("http://%s:%d", objstore.LocalDomain, store.Port),
		Region:    store.Region,
		AccessKey: store.AccessKey,
		SecretKey: store.SecretKey,
		Buckets:   names,
	})

	tail, err := m.streamRemoteOn(ctx, instance, script, output)
	if err != nil {
		return nil, fmt.Errorf("wire object store into sandbox: %w\n%s", err, tail)
	}

	report(fmt.Sprintf("%s can now reach the object store", instance.Name))
	return accessFrom(store, buckets, true), nil
}

// parseDefaultGateway pulls the gateway address out of `ip route show default`
// output, e.g. "default via 192.168.64.1 dev enp0s1 proto dhcp src ... metric 100".
// There can be more than one default route; the first with a via wins, matching
// what the kernel would pick by metric order in the listing.
func parseDefaultGateway(out string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "via" {
				return fields[i+1], true
			}
		}
	}
	return "", false
}

type attachConfig struct {
	Gateway   string
	Domain    string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	Buckets   []string
}

// hostsBlock renders the managed /etc/hosts section for inside the sandbox. The
// bare domain covers path-style addressing; one entry per bucket covers
// subdomain style, which /etc/hosts cannot wildcard.
func (c attachConfig) hostsBlock() string {
	hosts := []string{c.Domain}
	for _, bucket := range c.Buckets {
		hosts = append(hosts, bucket+"."+c.Domain)
	}

	var out strings.Builder
	out.WriteString(objstoreHostsStart + "\n")
	// Long lines are legal in /etc/hosts but hard to read, so cap how many names
	// share a line.
	const perLine = 4
	for start := 0; start < len(hosts); start += perLine {
		end := start + perLine
		if end > len(hosts) {
			end = len(hosts)
		}
		out.WriteString(c.Gateway + " " + strings.Join(hosts[start:end], " ") + "\n")
	}
	out.WriteString(objstoreHostsEnd)
	return out.String()
}

// attachScript builds the shell script that runs inside the sandbox. Every
// heredoc is quoted so the sandbox's shell does no expansion on values we
// injected — credentials and hostnames go in literally.
func attachScript(c attachConfig) string {
	var b strings.Builder

	b.WriteString("set -eu\n\n")

	// /etc/hosts: drop our old block, append the current one.
	b.WriteString("HOSTS_BLOCK=$(cat <<'VPSBOX_HOSTS_EOF'\n")
	b.WriteString(c.hostsBlock())
	b.WriteString("\nVPSBOX_HOSTS_EOF\n)\n\n")
	b.WriteString("echo " + shellQuote("Pointing "+c.Domain+" at the host…") + "\n")
	b.WriteString(fmt.Sprintf(
		"sudo sed -i '/^%s$/,/^%s$/d' /etc/hosts\n",
		regexpEscapeForSed(objstoreHostsStart), regexpEscapeForSed(objstoreHostsEnd)))
	b.WriteString("printf '%s\\n' \"$HOSTS_BLOCK\" | sudo tee -a /etc/hosts >/dev/null\n\n")

	// ~/.aws/credentials — read by every S3 client without being told to. 0600
	// because it is the actual secret.
	b.WriteString("mkdir -p \"$HOME/.aws\"\n")
	b.WriteString("cat > \"$HOME/.aws/credentials\" <<'VPSBOX_CREDS_EOF'\n")
	b.WriteString("[default]\n")
	b.WriteString("aws_access_key_id = " + c.AccessKey + "\n")
	b.WriteString("aws_secret_access_key = " + c.SecretKey + "\n")
	b.WriteString("VPSBOX_CREDS_EOF\n")
	b.WriteString("chmod 600 \"$HOME/.aws/credentials\"\n\n")

	// ~/.aws/config — endpoint_url is honoured by newer AWS CLI and SDKs.
	b.WriteString("cat > \"$HOME/.aws/config\" <<'VPSBOX_CONFIG_EOF'\n")
	b.WriteString("[default]\n")
	b.WriteString("region = " + c.Region + "\n")
	b.WriteString("endpoint_url = " + c.Endpoint + "\n")
	b.WriteString("VPSBOX_CONFIG_EOF\n")
	b.WriteString("chmod 600 \"$HOME/.aws/config\"\n\n")

	// Endpoint in the environment for interactive shells. No credentials here.
	b.WriteString("sudo tee " + objstoreProfilePath + " >/dev/null <<'VPSBOX_PROFILE_EOF'\n")
	b.WriteString("# vpsbox: local object store endpoint. Credentials live in ~/.aws/credentials.\n")
	b.WriteString("export AWS_ENDPOINT_URL=" + c.Endpoint + "\n")
	b.WriteString("export AWS_ENDPOINT_URL_S3=" + c.Endpoint + "\n")
	b.WriteString("export AWS_DEFAULT_REGION=" + c.Region + "\n")
	b.WriteString("export AWS_REGION=" + c.Region + "\n")
	b.WriteString("export VPSBOX_S3_ENDPOINT=" + c.Endpoint + "\n")
	b.WriteString("VPSBOX_PROFILE_EOF\n")
	b.WriteString("sudo chmod 644 " + objstoreProfilePath + "\n\n")

	// The AWS CLI is the client people reach for; install it if the image lacks it.
	b.WriteString("if ! command -v aws >/dev/null 2>&1; then\n")
	b.WriteString("  echo 'Installing the AWS CLI…'\n")
	b.WriteString("  sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq\n")
	b.WriteString("  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq awscli\n")
	b.WriteString("fi\n\n")

	// The `s3` shorthand. Only ever overwrite our own wrapper.
	b.WriteString("if [ ! -e " + objstoreWrapperPath + " ] || grep -q " + shellQuote(objstoreWrapperMarker) + " " + objstoreWrapperPath + " 2>/dev/null; then\n")
	b.WriteString("  sudo tee " + objstoreWrapperPath + " >/dev/null <<'VPSBOX_WRAPPER_EOF'\n")
	b.WriteString("#!/bin/sh\n")
	b.WriteString("# " + objstoreWrapperMarker + "\n")
	b.WriteString("# Talk to the vpsbox object store without repeating --endpoint-url.\n")
	b.WriteString("exec aws --endpoint-url " + c.Endpoint + " s3 \"$@\"\n")
	b.WriteString("VPSBOX_WRAPPER_EOF\n")
	b.WriteString("  sudo chmod 755 " + objstoreWrapperPath + "\n")
	b.WriteString("else\n")
	b.WriteString("  echo 'Leaving the existing " + objstoreWrapperPath + " alone (not ours).'\n")
	b.WriteString("fi\n\n")

	// Prove the whole chain works rather than claiming it does.
	b.WriteString("echo 'Checking the connection…'\n")
	b.WriteString("aws --endpoint-url " + c.Endpoint + " s3 ls\n")
	b.WriteString("echo 'Object store reachable.'\n")

	return b.String()
}

// shellQuote wraps a value in single quotes for safe use in the generated script.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// regexpEscapeForSed escapes the characters that matter inside the sed address
// patterns we build from the hosts markers.
func regexpEscapeForSed(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`/`, `\/`,
		`.`, `\.`,
		`*`, `\*`,
		`[`, `\[`,
		`]`, `\]`,
		`^`, `\^`,
		`$`, `\$`,
	)
	return replacer.Replace(value)
}
