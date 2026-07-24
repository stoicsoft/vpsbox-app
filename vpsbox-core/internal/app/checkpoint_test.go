package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stoicsoft/vpsbox/internal/backend"
	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

// stubBackend is a Backend that reports whatever the test sets, so the
// checkpoint guards can be exercised without a real VM.
type stubBackend struct {
	status    backend.InstanceStatus
	ipv4      []string
	snapshots []backend.SnapshotInfo

	deleted []string
}

func (s *stubBackend) Name() string  { return "stub" }
func (s *stubBackend) Priority() int { return 0 }
func (s *stubBackend) Available(context.Context) (bool, error) {
	return true, nil
}
func (s *stubBackend) EnsureInstalled(context.Context) error               { return nil }
func (s *stubBackend) Create(context.Context, backend.CreateRequest) error { return nil }
func (s *stubBackend) Start(context.Context, string) error                 { return nil }
func (s *stubBackend) Stop(context.Context, string) error                  { return nil }
func (s *stubBackend) Delete(context.Context, string) error                { return nil }

func (s *stubBackend) List(context.Context) ([]backend.InstanceInfo, error) {
	return nil, nil
}

func (s *stubBackend) Info(_ context.Context, name string) (*backend.InstanceInfo, error) {
	return &backend.InstanceInfo{
		Name:    name,
		Status:  s.status,
		Backend: "stub",
		IPv4:    s.ipv4,
	}, nil
}

func (s *stubBackend) UpdateResources(context.Context, backend.UpdateResourcesRequest) error {
	return nil
}
func (s *stubBackend) InstallSSHKey(context.Context, backend.InstallSSHKeyRequest) error {
	return nil
}
func (s *stubBackend) Snapshot(context.Context, string, string, string) error { return nil }
func (s *stubBackend) Restore(context.Context, string, string) error          { return nil }

func (s *stubBackend) ListSnapshots(context.Context, string) ([]backend.SnapshotInfo, error) {
	return s.snapshots, nil
}

func (s *stubBackend) DeleteSnapshot(_ context.Context, _, snapshotName string) error {
	s.deleted = append(s.deleted, snapshotName)
	return nil
}

func newTestManager(t *testing.T, stub *stubBackend) *Manager {
	t.Helper()
	baseDir := t.TempDir()
	paths := config.FromBase(baseDir, filepath.Join(baseDir, "hosts"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := registry.NewStore(paths)
	if err := store.UpsertInstance(registry.Instance{Name: "demo", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	return &Manager{paths: paths, store: store, backend: stub}
}

func TestCheckpointLabel(t *testing.T) {
	if got := checkpointLabel("before-coolify"); got != "checkpoint-before-coolify" {
		t.Fatalf("plain label = %q, want the checkpoint prefix added", got)
	}
	if got := checkpointLabel("  spaced  "); got != "checkpoint-spaced" {
		t.Fatalf("label = %q, want surrounding space trimmed", got)
	}
	if got := checkpointLabel("checkpoint-already"); got != "checkpoint-already" {
		t.Fatalf("label = %q, want an existing prefix left alone", got)
	}
	if got := checkpointLabel(""); !strings.HasPrefix(got, checkpointPrefix) || got == checkpointPrefix {
		t.Fatalf("empty label = %q, want a prefixed timestamp", got)
	}
}

// A stopped sandbox has no address, and the refresh helpers poll for 20 minutes
// before giving up — so this has to fail immediately rather than hang.
func TestRequireRunningInstanceRejectsStoppedSandbox(t *testing.T) {
	manager := newTestManager(t, &stubBackend{status: backend.StatusStopped})

	_, err := manager.requireRunningInstance(context.Background(), "demo")
	if err == nil {
		t.Fatal("expected an error for a stopped sandbox")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("error = %q, want it to say the sandbox is not running", err)
	}
}

func TestRequireRunningInstanceRejectsMissingAddress(t *testing.T) {
	manager := newTestManager(t, &stubBackend{status: backend.StatusRunning})

	_, err := manager.requireRunningInstance(context.Background(), "demo")
	if err == nil {
		t.Fatal("expected an error when the sandbox has no address yet")
	}
	if !strings.Contains(err.Error(), "no address") {
		t.Fatalf("error = %q, want it to mention the missing address", err)
	}
}

func TestRequireRunningInstanceRefreshesAddress(t *testing.T) {
	manager := newTestManager(t, &stubBackend{
		status: backend.StatusRunning,
		ipv4:   []string{"192.168.64.5"},
	})

	instance, err := manager.requireRunningInstance(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if instance.Host != "192.168.64.5" {
		t.Fatalf("host = %q, want the address refreshed from the backend", instance.Host)
	}
}

// Checkpoint must refuse a stopped sandbox for the same reason: the baseline
// capture that follows the snapshot needs a live SSH session.
func TestCheckpointRejectsStoppedSandbox(t *testing.T) {
	manager := newTestManager(t, &stubBackend{status: backend.StatusStopped})

	if _, err := manager.Checkpoint(context.Background(), "demo", ""); err == nil {
		t.Fatal("expected Checkpoint to reject a stopped sandbox")
	}
}

func TestDiffRejectsStoppedSandbox(t *testing.T) {
	manager := newTestManager(t, &stubBackend{status: backend.StatusStopped})

	if _, err := manager.Diff(context.Background(), "demo"); err == nil {
		t.Fatal("expected Diff to reject a stopped sandbox")
	}
}

func TestLatestCheckpointPicksNewestAndIgnoresOtherSnapshots(t *testing.T) {
	manager := newTestManager(t, &stubBackend{
		snapshots: []backend.SnapshotInfo{
			{Name: "checkpoint-first"},
			{Name: "lab-baseline"},
			{Name: "checkpoint-second"},
			{Name: "manual-save"},
		},
	})

	latest, err := manager.latestCheckpoint(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if latest != "checkpoint-second" {
		t.Fatalf("latest = %q, want the newest checkpoint-prefixed snapshot", latest)
	}
}

func TestLatestCheckpointReportsWhenNoneExist(t *testing.T) {
	manager := newTestManager(t, &stubBackend{
		snapshots: []backend.SnapshotInfo{{Name: "lab-baseline"}},
	})

	if _, err := manager.latestCheckpoint(context.Background(), "demo"); err == nil {
		t.Fatal("expected an error when no checkpoint snapshots exist")
	}
}

func TestCheckpointBaselineReportsNilBeforeAnyCheckpoint(t *testing.T) {
	manager := newTestManager(t, &stubBackend{status: backend.StatusRunning})

	baseline, err := manager.CheckpointBaseline("demo")
	if err != nil {
		t.Fatal(err)
	}
	if baseline != nil {
		t.Fatalf("baseline = %+v, want nil before any checkpoint is saved", baseline)
	}
}

func TestDeleteSnapshotRequiresASnapshotName(t *testing.T) {
	manager := newTestManager(t, &stubBackend{})

	if err := manager.DeleteSnapshot(context.Background(), "demo", "  "); err == nil {
		t.Fatal("expected an error when no snapshot name is given")
	}
}

// Deleting the snapshot the baseline describes has to take the baseline with
// it — otherwise a later diff is measured against a point that can no longer
// be restored.
func TestDeleteSnapshotClearsTheMatchingBaseline(t *testing.T) {
	stub := &stubBackend{}
	manager := newTestManager(t, stub)
	if err := manager.store.SaveBaseline(registry.Baseline{
		Instance:   "demo",
		Checkpoint: "checkpoint-before-coolify",
	}); err != nil {
		t.Fatal(err)
	}

	if err := manager.DeleteSnapshot(context.Background(), "demo", "checkpoint-before-coolify"); err != nil {
		t.Fatal(err)
	}

	if len(stub.deleted) != 1 || stub.deleted[0] != "checkpoint-before-coolify" {
		t.Fatalf("backend deletions = %v, want the one snapshot", stub.deleted)
	}
	baseline, err := manager.CheckpointBaseline("demo")
	if err != nil {
		t.Fatal(err)
	}
	if baseline != nil {
		t.Fatalf("baseline = %+v, want it removed with its snapshot", baseline)
	}
}

func TestDeleteSnapshotKeepsAnUnrelatedBaseline(t *testing.T) {
	manager := newTestManager(t, &stubBackend{})
	if err := manager.store.SaveBaseline(registry.Baseline{
		Instance:   "demo",
		Checkpoint: "checkpoint-keep-me",
	}); err != nil {
		t.Fatal(err)
	}

	if err := manager.DeleteSnapshot(context.Background(), "demo", "checkpoint-other"); err != nil {
		t.Fatal(err)
	}

	baseline, err := manager.CheckpointBaseline("demo")
	if err != nil {
		t.Fatal(err)
	}
	if baseline == nil || baseline.Checkpoint != "checkpoint-keep-me" {
		t.Fatalf("baseline = %+v, want the unrelated baseline left alone", baseline)
	}
}

func TestCheckpointBaselineReturnsSavedBaseline(t *testing.T) {
	manager := newTestManager(t, &stubBackend{status: backend.StatusRunning})
	if err := manager.store.SaveBaseline(registry.Baseline{
		Instance:   "demo",
		Checkpoint: "checkpoint-before-coolify",
		Packages:   []string{"nginx"},
	}); err != nil {
		t.Fatal(err)
	}

	baseline, err := manager.CheckpointBaseline("demo")
	if err != nil {
		t.Fatal(err)
	}
	if baseline == nil || baseline.Checkpoint != "checkpoint-before-coolify" {
		t.Fatalf("baseline = %+v, want the saved checkpoint", baseline)
	}
}
