package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.yaml")
	content := `apiVersion: vpsbox.stoicsoft.com/v1alpha1
kind: Lab
metadata:
  name: smoke
  unexpected: true
spec:
  instances: []
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field unexpected") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestValidateBoundsAndReferences(t *testing.T) {
	manifest := Manifest{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "easy-smoke"},
		Spec: Spec{Instances: []InstanceSpec{{
			Name:      "source",
			Role:      "easypanel-source",
			Image:     "24.04",
			Resources: Resources{CPUs: 4, MemoryGB: 6, DiskGB: 30},
			Fixture:   "easypanel/mimic",
		}}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if manifest.Spec.MaxParallel != 1 {
		t.Fatalf("expected default max parallel 1, got %d", manifest.Spec.MaxParallel)
	}

	manifest.Spec.Instances[0].JoinSwarmOf = "missing"
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected invalid swarm reference")
	}
}

func TestInstanceNameIsDeterministicAndBounded(t *testing.T) {
	first, err := InstanceName("migration-run", "source")
	if err != nil {
		t.Fatal(err)
	}
	second, err := InstanceName("migration-run", "source")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) > 63 {
		t.Fatalf("unexpected names %q and %q", first, second)
	}
	if _, err := InstanceName("", "source"); err == nil {
		t.Fatal("expected empty run ID to fail")
	}
}

func TestCommittedEasyPanelScenariosAreValid(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "scenarios", "easypanel-migration", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("expected three EasyPanel scenarios, got %d", len(paths))
	}
	for _, path := range paths {
		if _, err := Load(path); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
}
