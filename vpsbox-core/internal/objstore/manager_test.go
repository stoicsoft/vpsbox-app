package objstore

import (
	"path/filepath"
	"testing"

	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

func newTestManager(t *testing.T) (*Manager, *registry.Store) {
	t.Helper()
	base := t.TempDir()
	paths := config.FromBase(base, filepath.Join(base, "hosts"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure paths: %v", err)
	}
	store := registry.NewStore(paths)
	return NewManager(paths, store), store
}

// Stopping the server must not rotate the credentials. Sandboxes that were
// attached already hold this access key in ~/.aws/credentials, and the server
// scopes bucket listings per key — so a fresh key on the next start would both
// break those sandboxes and hide every bucket that already exists.
func TestStopKeepsCredentials(t *testing.T) {
	manager, store := newTestManager(t)

	// PID 0 is never alive, so Stop takes the bookkeeping path without needing a
	// real server to kill.
	original := registry.ObjStore{
		Provider:  provider,
		Endpoint:  "http://127.0.0.1:9000",
		Port:      9000,
		Region:    defaultRegion,
		AccessKey: "GKaaaaaaaaaaaaaaaaaaaaaa",
		SecretKey: "ssssssssssssssssssssssssssssssssssssssss",
		PID:       0,
	}
	if err := store.SaveObjStore(original); err != nil {
		t.Fatalf("seed objstore: %v", err)
	}

	if err := manager.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	after, err := store.LoadObjStore()
	if err != nil {
		t.Fatalf("load objstore: %v", err)
	}
	if after == nil {
		t.Fatal("stop forgot the object store record entirely")
	}
	if after.AccessKey != original.AccessKey {
		t.Errorf("access key changed on stop: %q then %q", original.AccessKey, after.AccessKey)
	}
	if after.SecretKey != original.SecretKey {
		t.Error("secret key changed on stop")
	}
	if after.PID != 0 {
		t.Errorf("PID = %d, want 0 after stop", after.PID)
	}
}

func TestStopOnNeverStartedStoreIsNoOp(t *testing.T) {
	manager, _ := newTestManager(t)

	if err := manager.Stop(); err != nil {
		t.Errorf("stopping a store that was never started should succeed, got: %v", err)
	}
}

func TestStatusReportsStoppedForZeroPID(t *testing.T) {
	manager, store := newTestManager(t)

	if err := store.SaveObjStore(registry.ObjStore{Port: 9000, PID: 0}); err != nil {
		t.Fatalf("seed objstore: %v", err)
	}

	recorded, alive, err := manager.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if recorded == nil {
		t.Fatal("status lost the record")
	}
	if alive {
		t.Error("a zero PID must not be reported as alive")
	}
}

func TestStatusOnEmptyStore(t *testing.T) {
	manager, _ := newTestManager(t)

	recorded, alive, err := manager.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if recorded != nil || alive {
		t.Errorf("status = (%+v, %v), want (nil, false)", recorded, alive)
	}
}

// With the server down, listing falls back to the recorded buckets rather than
// erroring — a UI poll must still be able to render.
func TestListBucketsFallsBackToRecordWhenStopped(t *testing.T) {
	manager, store := newTestManager(t)

	if err := store.SaveObjStore(registry.ObjStore{Port: 9000, PID: 0}); err != nil {
		t.Fatalf("seed objstore: %v", err)
	}
	for _, name := range []string{"backups", "uploads"} {
		if err := store.UpsertBucket(registry.Bucket{Name: name}); err != nil {
			t.Fatalf("seed bucket: %v", err)
		}
	}

	buckets, err := manager.ListBuckets(t.Context())
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	if len(buckets) != 2 {
		t.Errorf("got %d buckets, want the 2 recorded ones", len(buckets))
	}
}

func TestProcessIsBinaryRejectsUnknownPID(t *testing.T) {
	if processIsBinary(0, binary) {
		t.Error("PID 0 must never be claimed as ours")
	}
	if processIsBinary(-1, binary) {
		t.Error("a negative PID must never be claimed as ours")
	}
	// PID 1 exists on every unix host and is definitely not our server. This is
	// the guard that stops a recycled PID from getting an unrelated process killed.
	if processIsBinary(1, binary) {
		t.Error("PID 1 must not be mistaken for the object storage server")
	}
}

func TestFreePortReturnsSomethingBindable(t *testing.T) {
	port, err := freePort(firstAPIPort)
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	if port < firstAPIPort || port >= firstAPIPort+portScanRange {
		t.Errorf("port %d is outside the scan range", port)
	}
}
