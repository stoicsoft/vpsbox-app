package desktopbackend

import (
	"testing"
	"time"

	vpsapp "github.com/stoicsoft/vpsbox/internal/app"
)

func TestDisplayLabelStripsCheckpointPrefix(t *testing.T) {
	if got := displayLabel("checkpoint-before-coolify"); got != "before-coolify" {
		t.Fatalf("label = %q, want the storage prefix stripped", got)
	}
	if got := displayLabel("lab-baseline"); got != "lab-baseline" {
		t.Fatalf("label = %q, want non-checkpoint names left alone", got)
	}
}

func TestMapDiffFlattensEveryGroup(t *testing.T) {
	captured := time.Date(2026, 7, 23, 10, 30, 0, 0, time.UTC)
	diff := &vpsapp.BaselineDiff{
		Checkpoint:      "checkpoint-before-coolify",
		CapturedAt:      captured,
		AddedPackages:   []string{"docker-ce"},
		RemovedPackages: []string{"apache2"},
		AddedServices:   []string{"docker.service"},
		RemovedServices: []string{"apache2.service"},
		AddedPorts:      []string{"0.0.0.0:8000"},
		RemovedPorts:    []string{"0.0.0.0:80"},
		AddedFiles:      []string{"docker/daemon.json"},
		RemovedFiles:    []string{"apache2/apache2.conf"},
		ModifiedFiles:   []string{"hosts"},
	}

	out := mapDiff(diff)

	if out.Total != 9 {
		t.Fatalf("total = %d, want 9", out.Total)
	}
	if len(out.Changes) != out.Total {
		t.Fatalf("flattened %d changes, want %d", len(out.Changes), out.Total)
	}
	if out.Checkpoint != "before-coolify" {
		t.Fatalf("checkpoint = %q, want the display label", out.Checkpoint)
	}
	if out.CapturedAt == "" || out.FetchedAt == "" {
		t.Fatalf("expected both timestamps to be set, got %+v", out)
	}

	counts := map[string]int{}
	for _, change := range out.Changes {
		counts[change.Kind+"/"+change.Group]++
	}
	want := map[string]int{
		"added/package": 1, "removed/package": 1,
		"added/service": 1, "removed/service": 1,
		"added/port": 1, "removed/port": 1,
		"added/file": 1, "removed/file": 1,
		"modified/file": 1,
	}
	for key, expected := range want {
		if counts[key] != expected {
			t.Fatalf("%s count = %d, want %d (got %+v)", key, counts[key], expected, counts)
		}
	}
}

func TestMapDiffHandlesNoChanges(t *testing.T) {
	out := mapDiff(&vpsapp.BaselineDiff{Checkpoint: "checkpoint-clean"})

	if out.Total != 0 {
		t.Fatalf("total = %d, want 0", out.Total)
	}
	if len(out.Changes) != 0 {
		t.Fatalf("changes = %+v, want none", out.Changes)
	}
	if out.CapturedAt != "" {
		t.Fatalf("capturedAt = %q, want empty for a zero timestamp", out.CapturedAt)
	}
}

func TestMapDiffHandlesNil(t *testing.T) {
	out := mapDiff(nil)

	if out.Total != 0 || len(out.Changes) != 0 {
		t.Fatalf("nil diff = %+v, want an empty result", out)
	}
	if out.FetchedAt == "" {
		t.Fatal("expected fetchedAt to be stamped even for a nil diff")
	}
}
