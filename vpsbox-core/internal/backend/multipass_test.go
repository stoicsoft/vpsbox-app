package backend

import (
	"reflect"
	"testing"
)

func TestMultipassRestoreArgsAreNonInteractive(t *testing.T) {
	want := []string{"restore", "--destructive", "demo.clean-base"}
	if got := multipassRestoreArgs("demo", "clean-base"); !reflect.DeepEqual(got, want) {
		t.Fatalf("multipassRestoreArgs() = %#v, want %#v", got, want)
	}
}

// The instance.snapshot form matters: `multipass delete demo` would destroy the
// whole sandbox rather than one of its snapshots.
func TestMultipassDeleteSnapshotArgsTargetTheSnapshot(t *testing.T) {
	want := []string{"delete", "demo.clean-base"}
	if got := multipassDeleteSnapshotArgs("demo", "clean-base"); !reflect.DeepEqual(got, want) {
		t.Fatalf("multipassDeleteSnapshotArgs() = %#v, want %#v", got, want)
	}
}

func TestMultipassDeleteSnapshotRejectsAnEmptyName(t *testing.T) {
	// Guards the same hazard from the other side: an empty snapshot name would
	// build "demo." and hand multipass something it may read as the instance.
	if err := (&Multipass{}).DeleteSnapshot(t.Context(), "demo", ""); err == nil {
		t.Fatal("expected an error for an empty snapshot name")
	}
}
