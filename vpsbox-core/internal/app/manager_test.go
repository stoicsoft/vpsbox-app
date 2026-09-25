package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/domain"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

func TestIgnoreHostsPrivilegeError(t *testing.T) {
	if err := ignoreHostsPrivilegeError(domain.ErrPrivilegesRequired); err != nil {
		t.Fatalf("direct privilege error should be non-fatal: %v", err)
	}
	if err := ignoreHostsPrivilegeError(fmt.Errorf("sync hosts: %w", domain.ErrPrivilegesRequired)); err != nil {
		t.Fatalf("wrapped privilege error should be non-fatal: %v", err)
	}

	want := errors.New("registry read failed")
	if got := ignoreHostsPrivilegeError(want); !errors.Is(got, want) {
		t.Fatalf("non-privilege error = %v, want %v", got, want)
	}
}

func TestCleanupInstanceArtifacts(t *testing.T) {
	baseDir := t.TempDir()
	paths := config.FromBase(baseDir, filepath.Join(baseDir, "hosts"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store := registry.NewStore(paths)
	manager := &Manager{paths: paths, store: store}

	artifactPaths := []string{
		paths.KeyPath("demo"),
		paths.PublicKeyPath("demo"),
		paths.CertPath("demo"),
		paths.CertKeyPath("demo"),
		paths.CloudInitPath("demo"),
	}
	for _, artifactPath := range artifactPaths {
		if err := os.WriteFile(artifactPath, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveBaseline(registry.Baseline{Instance: "demo"}); err != nil {
		t.Fatal(err)
	}
	otherKey := paths.KeyPath("keep-me")
	if err := os.WriteFile(otherKey, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := manager.cleanupInstanceArtifacts("demo"); err != nil {
		t.Fatal(err)
	}
	for _, artifactPath := range artifactPaths {
		if _, err := os.Stat(artifactPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("artifact %s still exists or returned unexpected error: %v", artifactPath, err)
		}
	}
	if baseline, err := store.LoadBaseline("demo"); err != nil || baseline != nil {
		t.Fatalf("baseline = %#v, err = %v; want no baseline", baseline, err)
	}
	if _, err := os.Stat(otherKey); err != nil {
		t.Fatalf("unrelated key was removed: %v", err)
	}
}

func TestDestroyForgetsAVMTheHypervisorAlreadyDropped(t *testing.T) {
	stub := &stubBackend{
		deleteErr: fmt.Errorf(`multipass delete demo: delete failed: instance "demo" does not exist`),
	}
	manager := newTestManager(t, stub)

	if err := manager.Destroy(t.Context(), "demo", true); err != nil {
		t.Fatalf("Destroy leftover registry row: %v", err)
	}
	if _, err := manager.store.GetInstance("demo"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GetInstance after Destroy = %v, want os.ErrNotExist", err)
	}
}

func TestDestroyKeepsTheRowWhenTheHypervisorDeleteFails(t *testing.T) {
	stub := &stubBackend{deleteErr: fmt.Errorf("multipass delete demo: qemu failed")}
	manager := newTestManager(t, stub)

	if err := manager.Destroy(t.Context(), "demo", true); err == nil {
		t.Fatal("Destroy = nil, want the hypervisor error")
	}
	if _, err := manager.store.GetInstance("demo"); err != nil {
		t.Fatalf("registry row was dropped after a real delete failure: %v", err)
	}
}

func TestListMarksAMissingHypervisorVMUnknown(t *testing.T) {
	manager := newTestManager(t, &stubBackend{})
	instances, err := manager.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(instances) != 1 || instances[0].Name != "demo" {
		t.Fatalf("List = %+v, want the leftover demo row", instances)
	}
	if instances[0].Status != "unknown" {
		t.Fatalf("status = %q, want unknown once Multipass no longer has the VM", instances[0].Status)
	}
}
