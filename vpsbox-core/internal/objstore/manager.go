package objstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/executil"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

const (
	// binary is the S3 server we manage. Garage is a single static binary built
	// for small self-hosted deployments, which is exactly this: one node, one
	// disk, on a laptop.
	binary = "garage"

	// provider is recorded in the registry so a future second implementation can
	// be told apart from this one.
	provider = "garage"

	// keyName labels our credentials on the server, for anyone who inspects it
	// with the garage CLI directly.
	keyName = "vpsbox"

	// layoutZone and layoutCapacity are the storage role given to the single node.
	// Capacity is advisory — it steers partition placement across a cluster and
	// has no cluster to steer here — so it is set high enough never to be the
	// thing that stops a backup working. The real limit is the disk.
	layoutZone     = "local"
	layoutCapacity = "100G"

	// defaultRegion is what the client signs with. Nothing in a local store cares
	// which region it is, but SigV4 requires one and clients default to this.
	defaultRegion = "us-east-1"

	// LocalDomain is the hostname a sandbox uses to reach the object store. It is
	// also handed to the server as its virtual-host domain, so that
	// <bucket>.buckets.vpsbox.local works and not just path-style addressing —
	// several SDKs (the JS v3 client among them) prefer subdomain style and fail
	// in confusing ways against a path-only endpoint.
	LocalDomain = "buckets.vpsbox.local"

	// firstAPIPort is where port selection starts. 9000 is MinIO's conventional
	// port, so a user who already knows MinIO sees the port they expect.
	firstAPIPort = 9000

	// portScanRange bounds how far port selection walks before giving up.
	portScanRange = 20

	// startupTimeout is how long to wait for a freshly spawned server to answer a
	// signed request before calling the start a failure.
	startupTimeout = 30 * time.Second
)

// ErrNotInstalled is returned when the S3 server binary is missing and we cannot
// install it on this platform.
var ErrNotInstalled = errors.New("object storage server is not installed")

// ErrNotRunning is returned by operations that need a live server when none is up
// and the caller asked not to start one.
var ErrNotRunning = errors.New("object storage server is not running")

// Manager owns the machine-local S3 server and the bucket registry. It follows
// the same shape as share.Manager: ensure a binary exists, run it detached, track
// the PID in JSON, and reap the record when the process goes away.
type Manager struct {
	paths config.Paths
	store *registry.Store
}

func NewManager(paths config.Paths, store *registry.Store) *Manager {
	return &Manager{paths: paths, store: store}
}

// Status reports the recorded server and whether its process is still alive,
// without starting anything. Safe to call on a hot path like a UI poll.
func (m *Manager) Status() (*registry.ObjStore, bool, error) {
	recorded, err := m.store.LoadObjStore()
	if err != nil {
		return nil, false, err
	}
	if recorded == nil {
		return nil, false, nil
	}
	return recorded, processAlive(recorded.PID), nil
}

// Installed reports whether the S3 server binary is available.
func (m *Manager) Installed() bool {
	return executil.LookPath(binary)
}

// EnsureInstalled installs the S3 server through the platform package manager,
// mirroring how share.Manager gets cloudflared.
func (m *Manager) EnsureInstalled(ctx context.Context) error {
	if m.Installed() {
		return nil
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		if executil.LookPath("brew") {
			if _, err := executil.Run(ctx, "brew", "install", binary); err != nil {
				return err
			}
			if m.Installed() {
				return nil
			}
		}
	}

	// Garage publishes no Windows build, so there is nothing to fall back to
	// there. Say so plainly rather than failing later with a missing binary.
	if runtime.GOOS == "windows" {
		return fmt.Errorf("%w: local object storage is not available on Windows yet", ErrNotInstalled)
	}

	return fmt.Errorf("%w: install it with `brew install %s` and try again", ErrNotInstalled, binary)
}

// configPath is the generated server configuration. It holds the RPC secret, so
// it is written 0600 — the server refuses to start with a world-readable secret.
func (m *Manager) configPath() string {
	return filepath.Join(m.paths.ObjStoreDir, "garage.toml")
}

func (m *Manager) metaDir() string {
	return filepath.Join(m.paths.ObjStoreDir, "meta")
}

// Ensure returns a running server, starting one if the recorded process is gone.
// Credentials and the data directory are reused across restarts — VMs that were
// already wired hold the old access key, so rotating it would silently break them.
func (m *Manager) Ensure(ctx context.Context, progress func(string)) (*registry.ObjStore, error) {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}

	recorded, alive, err := m.Status()
	if err != nil {
		return nil, err
	}

	if alive && processIsBinary(recorded.PID, binary) {
		// Something of ours is alive, but a process can be running and not yet
		// serving. A signed round-trip is the only proof that matters.
		client := clientFor(recorded)
		if client.Reachable(ctx) == nil {
			return recorded, nil
		}
		// Alive but not answering: it is wedged or still coming up. Give it the
		// startup grace period before deciding to replace it.
		if waitReachable(ctx, client, startupTimeout) == nil {
			return recorded, nil
		}
		_ = killProcessGroup(recorded.PID)
	}

	if !m.Installed() {
		report("Installing the object storage server (one time)…")
	}
	if err := m.EnsureInstalled(ctx); err != nil {
		return nil, err
	}

	report("Starting the local object store…")
	return m.start(ctx, recorded, report)
}

// start spawns the server, bootstraps it if this is its first run, and records
// it. previous carries forward credentials from an earlier run when there was
// one.
func (m *Manager) start(ctx context.Context, previous *registry.ObjStore, report func(string)) (*registry.ObjStore, error) {
	dataDir := m.paths.ObjStoreDataDir()
	for _, dir := range []string{dataDir, m.metaDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create object store directory: %w", err)
		}
	}

	accessKey, secretKey := "", ""
	if previous != nil {
		accessKey, secretKey = previous.AccessKey, previous.SecretKey
	}
	if accessKey == "" || secretKey == "" {
		generatedAccess, generatedSecret, err := generateCredentials()
		if err != nil {
			return nil, err
		}
		accessKey, secretKey = generatedAccess, generatedSecret
	}

	// Three listeners: the S3 API, inter-node RPC, and the admin endpoint. Only
	// the first is reachable off this machine.
	apiPort, err := freePort(firstAPIPort)
	if err != nil {
		return nil, err
	}
	rpcPort, err := freePort(apiPort + 1)
	if err != nil {
		return nil, err
	}
	adminPort, err := freePort(rpcPort + 1)
	if err != nil {
		return nil, err
	}

	rpcSecret, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	adminToken, err := randomHex(32)
	if err != nil {
		return nil, err
	}

	// The config is regenerated per start because the ports are. Nothing durable
	// lives in it — the buckets and the layout are in the metadata directory.
	config := renderGarageConfig(garageConfig{
		MetaDir:    m.metaDir(),
		DataDir:    dataDir,
		APIPort:    apiPort,
		RPCPort:    rpcPort,
		AdminPort:  adminPort,
		Region:     defaultRegion,
		RootDomain: LocalDomain,
		RPCSecret:  rpcSecret,
		AdminToken: adminToken,
	})
	if err := os.WriteFile(m.configPath(), []byte(config), 0o600); err != nil {
		return nil, fmt.Errorf("write object store config: %w", err)
	}
	// WriteFile leaves an existing file's mode alone, and the server refuses to
	// start if its secrets are world readable.
	if err := os.Chmod(m.configPath(), 0o600); err != nil {
		return nil, fmt.Errorf("secure object store config: %w", err)
	}

	resolved := executil.Resolve(binary)
	if resolved == "" {
		return nil, ErrNotInstalled
	}

	logFile, err := os.OpenFile(m.paths.ObjStoreLogPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	cmd := exec.Command(resolved, "-c", m.configPath(), "server")
	cmd.Env = executil.Env()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = processGroupAttrs()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start object storage server: %w", err)
	}

	pid := cmd.Process.Pid
	failed := func(err error) (*registry.ObjStore, error) {
		_ = killProcessGroup(pid)
		return nil, fmt.Errorf("%w (see %s)", err, m.paths.ObjStoreLogPath())
	}

	// The daemon answers admin calls before it serves S3, so wait on the CLI
	// first — a node with no storage role never answers S3 at all.
	if err := m.waitForDaemon(ctx, startupTimeout); err != nil {
		return failed(fmt.Errorf("object storage server did not come up: %w", err))
	}
	if err := m.ensureLayout(ctx, report); err != nil {
		return failed(err)
	}
	if err := m.ensureKey(ctx, accessKey, secretKey); err != nil {
		return failed(err)
	}

	store := registry.ObjStore{
		Provider:  provider,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d", apiPort),
		Port:      apiPort,
		Region:    defaultRegion,
		AccessKey: accessKey,
		SecretKey: secretKey,
		DataDir:   dataDir,
		PID:       pid,
		LogPath:   m.paths.ObjStoreLogPath(),
		StartedAt: time.Now().UTC(),
	}

	if err := waitReachable(ctx, clientFor(&store), startupTimeout); err != nil {
		return failed(fmt.Errorf("object storage server is not serving S3: %w", err))
	}

	// Detach: the server must survive the process that started it exiting.
	_ = cmd.Process.Release()

	if err := m.store.SaveObjStore(store); err != nil {
		return nil, err
	}
	return &store, nil
}

// waitForDaemon blocks until the server answers admin commands.
func (m *Manager) waitForDaemon(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, lastErr = m.garageCLI(ctx, "status"); lastErr == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("timed out")
	}
	return lastErr
}

// Stop shuts the server down. The data survives, so a later Ensure brings the
// same buckets back.
//
// The credentials are deliberately kept: sandboxes that were attached already
// have this access key in their ~/.aws/credentials, and the server scopes bucket
// listings per key, so minting a new one on the next start would both break those
// sandboxes and hide every existing bucket. Only the liveness is cleared.
func (m *Manager) Stop() error {
	recorded, alive, err := m.Status()
	if err != nil {
		return err
	}
	if recorded == nil {
		return nil
	}
	if alive && processIsBinary(recorded.PID, binary) {
		if err := killProcessGroup(recorded.PID); err != nil {
			return err
		}
	}

	stopped := *recorded
	stopped.PID = 0
	return m.store.SaveObjStore(stopped)
}

// CreateBucket makes a bucket on the server and records it. Creating a bucket
// that already exists is a no-op, so this is safe to repeat.
func (m *Manager) CreateBucket(ctx context.Context, name string, progress func(string)) (*registry.Bucket, error) {
	if err := ValidateBucketName(name); err != nil {
		return nil, err
	}

	store, err := m.Ensure(ctx, progress)
	if err != nil {
		return nil, err
	}

	if err := clientFor(store).CreateBucket(ctx, name); err != nil {
		return nil, err
	}

	bucket := registry.Bucket{Name: name, CreatedAt: time.Now().UTC()}
	if err := m.store.UpsertBucket(bucket); err != nil {
		return nil, err
	}
	return &bucket, nil
}

// ListBuckets returns the buckets the server actually holds, reconciling the
// local record against it so a bucket created or removed out of band shows up.
// With the server down it falls back to the recorded list.
func (m *Manager) ListBuckets(ctx context.Context) ([]registry.Bucket, error) {
	recorded, err := m.store.LoadBuckets()
	if err != nil {
		return nil, err
	}

	store, alive, err := m.Status()
	if err != nil || store == nil || !alive {
		return recorded, err
	}

	names, err := clientFor(store).ListBuckets(ctx)
	if err != nil {
		// The server is up but unhappy; the recorded list is still the best
		// answer available and listing should not hard-fail a UI poll.
		return recorded, nil
	}

	known := make(map[string]registry.Bucket, len(recorded))
	for _, bucket := range recorded {
		known[bucket.Name] = bucket
	}

	// The server decides what exists; the record only supplies the creation time.
	//
	// Records for buckets missing from the listing are kept rather than deleted.
	// A listing is scoped to our access key, so "not listed" can mean "not visible
	// to this key" rather than "gone" — deleting on that basis would silently
	// forget a bucket whose data is still on disk. An unlisted record simply is
	// not shown, and reappears intact if the bucket becomes visible again.
	out := make([]registry.Bucket, 0, len(names))
	for _, name := range names {
		if bucket, ok := known[name]; ok {
			out = append(out, bucket)
			continue
		}
		// Created outside vpsbox — adopt it so it stops looking like a ghost.
		adopted := registry.Bucket{Name: name, CreatedAt: time.Now().UTC()}
		if err := m.store.UpsertBucket(adopted); err == nil {
			out = append(out, adopted)
		}
	}

	return out, nil
}

// DeleteBucket removes a bucket. It refuses a bucket that still holds objects
// unless force is set — this is the one operation here that destroys data a
// sandbox snapshot cannot bring back.
func (m *Manager) DeleteBucket(ctx context.Context, name string, force bool) error {
	store, alive, err := m.Status()
	if err != nil {
		return err
	}
	if store == nil || !alive {
		return ErrNotRunning
	}

	client := clientFor(store)
	empty, err := client.BucketEmpty(ctx, name)
	if err != nil {
		return err
	}
	if !empty && !force {
		return fmt.Errorf("bucket %q still holds objects — pass --force to delete it and everything in it", name)
	}
	if !empty {
		if err := client.emptyBucket(ctx, name); err != nil {
			return err
		}
	}

	if err := client.DeleteBucket(ctx, name); err != nil {
		var failure *apiError
		if !errors.As(err, &failure) || failure.Code != "NoSuchBucket" {
			return err
		}
	}
	return m.store.DeleteBucket(name)
}

// Credentials returns the live endpoint and keys, starting the server if needed.
// This is what wiring a sandbox needs.
func (m *Manager) Credentials(ctx context.Context, progress func(string)) (*registry.ObjStore, error) {
	return m.Ensure(ctx, progress)
}

func clientFor(store *registry.ObjStore) *client {
	region := store.Region
	if region == "" {
		region = defaultRegion
	}
	return newClient(store.Endpoint, region, store.AccessKey, store.SecretKey)
}

func waitReachable(ctx context.Context, c *client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if lastErr = c.Reachable(ctx); lastErr == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("timed out")
	}
	return lastErr
}

// freePort walks upward from start looking for a port nothing is listening on.
// There is an inherent race between checking and the server binding, but both
// happen on this machine within milliseconds of each other.
func freePort(start int) (int, error) {
	for port := start; port < start+portScanRange; port++ {
		listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, fmt.Errorf("no free port in range %d-%d", start, start+portScanRange)
}

// generateCredentials produces an access key and secret in the server's native
// shape: a "GK"-prefixed 24-hex-character key ID and a 64-hex-character secret.
// The secret is long because the S3 endpoint listens on all interfaces — it is
// the only thing standing between a bucket and anyone else on the network.
func generateCredentials() (string, string, error) {
	accessKey, err := randomHex(12)
	if err != nil {
		return "", "", fmt.Errorf("generate access key: %w", err)
	}
	secretKey, err := randomHex(32)
	if err != nil {
		return "", "", fmt.Errorf("generate secret key: %w", err)
	}
	return "GK" + accessKey, secretKey, nil
}

// randomHex returns n cryptographically random bytes as hex.
func randomHex(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
