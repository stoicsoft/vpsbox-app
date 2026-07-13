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
