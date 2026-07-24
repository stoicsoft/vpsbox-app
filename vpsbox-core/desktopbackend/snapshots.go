package desktopbackend

import (
	"context"
	"fmt"
	"strings"
	"time"

	vpsapp "github.com/stoicsoft/vpsbox/internal/app"
)

// checkpointPrefix mirrors the prefix internal/app puts on checkpoint
// snapshots. The desktop only uses it to label entries in the list; creating
// and restoring go through the manager, which owns the real naming.
const checkpointPrefix = "checkpoint-"

// SnapshotEntry is one saved point in time for a sandbox. The backend's own
// SnapshotInfo carries no JSON tags, so it is remapped here rather than bound
// to the frontend directly.
type SnapshotEntry struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Comment    string `json:"comment"`
	Parent     string `json:"parent"`
	Checkpoint bool   `json:"checkpoint"`
	Latest     bool   `json:"latest"`
	Current    bool   `json:"current"`
}

// SnapshotList is the snapshot tab's whole payload: the saved points plus what
// the current diff baseline is measured against.
type SnapshotList struct {
	Entries       []SnapshotEntry `json:"entries"`
	HasBaseline   bool            `json:"hasBaseline"`
	BaselineLabel string          `json:"baselineLabel,omitempty"`
	BaselineAt    string          `json:"baselineAt,omitempty"`
}

// DiffEntry is a single change between the baseline and the sandbox right now.
type DiffEntry struct {
	Kind  string `json:"kind"`  // added | removed | modified
	Group string `json:"group"` // package | service | port | file
	Value string `json:"value"`
}

// ServerDiff is the flattened result of comparing live state to the baseline.
type ServerDiff struct {
	Checkpoint string      `json:"checkpoint"`
	CapturedAt string      `json:"capturedAt"`
	FetchedAt  string      `json:"fetchedAt"`
	Total      int         `json:"total"`
	Changes    []DiffEntry `json:"changes"`
}

// StartCheckpoint saves a snapshot and records a baseline for later diffs.
// The sandbox restarts during the snapshot — the backend can only capture a
// stopped VM — so this is a job rather than a synchronous call.
func (a *App) StartCheckpoint(name, label string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	job := a.newJob("checkpoint", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Saving checkpoint")
		_, err := a.manager.Checkpoint(context.Background(), name, label)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Checkpoint saved")
		return nil
	})
	return job.ID, nil
}

// StartRestoreSnapshot rolls the sandbox back to a saved point. An empty
// snapshot name restores the most recent checkpoint, which is the "undo" case.
func (a *App) StartRestoreSnapshot(name, snapshot string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	job := a.newJob("restore", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Restoring snapshot")
		_, restored, err := a.manager.RestoreCheckpoint(context.Background(), name, snapshot)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Restored "+displayLabel(restored))
		return nil
	})
	return job.ID, nil
}

// StartDeleteSnapshot removes a saved point and reclaims its disk space.
// Multipass purges as part of the delete, which can take a moment, so this is a
// job rather than a synchronous call.
func (a *App) StartDeleteSnapshot(name, snapshot string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	if strings.TrimSpace(snapshot) == "" {
		return "", fmt.Errorf("snapshot name is required")
	}
	job := a.newJob("unsnapshot", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Deleting "+displayLabel(snapshot))
		if err := a.manager.DeleteSnapshot(context.Background(), name, snapshot); err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Deleted "+displayLabel(snapshot))
		return nil
	})
	return job.ID, nil
}

// ListSnapshots reports the saved points for a sandbox plus the baseline the
// diff view compares against. Read-only and safe to call while stopped.
func (a *App) ListSnapshots(name string) (SnapshotList, error) {
	if a.manager == nil {
		return SnapshotList{}, fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return SnapshotList{}, fmt.Errorf("sandbox name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	snapshots, err := a.manager.ListSnapshots(ctx, name)
	if err != nil {
		return SnapshotList{}, err
	}

	baseline, err := a.manager.CheckpointBaseline(name)
	if err != nil {
		return SnapshotList{}, err
	}

	list := SnapshotList{Entries: make([]SnapshotEntry, 0, len(snapshots))}
	if baseline != nil {
		list.HasBaseline = true
		list.BaselineLabel = displayLabel(baseline.Checkpoint)
		if !baseline.CapturedAt.IsZero() {
			list.BaselineAt = baseline.CapturedAt.Local().Format(time.RFC822)
		}
	}

	// The backend returns snapshots oldest first; the newest checkpoint is the
	// one "Undo" would roll back to, so mark it for the UI.
	latestCheckpoint := ""
	for _, snapshot := range snapshots {
		if strings.HasPrefix(snapshot.Name, checkpointPrefix) {
			latestCheckpoint = snapshot.Name
		}
	}

	for _, snapshot := range snapshots {
		list.Entries = append(list.Entries, SnapshotEntry{
			Name:       snapshot.Name,
			Label:      displayLabel(snapshot.Name),
			Comment:    snapshot.Comment,
			Parent:     snapshot.Parent,
			Checkpoint: strings.HasPrefix(snapshot.Name, checkpointPrefix),
			Latest:     snapshot.Name != "" && snapshot.Name == latestCheckpoint,
			Current:    baseline != nil && snapshot.Name == baseline.Checkpoint,
		})
	}

	// Newest first: the point a user wants to restore is almost always recent.
	for i, j := 0, len(list.Entries)-1; i < j; i, j = i+1, j-1 {
		list.Entries[i], list.Entries[j] = list.Entries[j], list.Entries[i]
	}

	return list, nil
}

// GetServerDiff compares the sandbox to its baseline. It SSHes in, so it needs
// a running sandbox and gets the same bounded timeout as the logs view.
func (a *App) GetServerDiff(name string) (ServerDiff, error) {
	if a.manager == nil {
		return ServerDiff{}, fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return ServerDiff{}, fmt.Errorf("sandbox name is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	diff, err := a.manager.Diff(ctx, name)
	if err != nil {
		return ServerDiff{}, err
	}
	return mapDiff(diff), nil
}

// mapDiff flattens the grouped diff into rows the table can render directly.
func mapDiff(diff *vpsapp.BaselineDiff) ServerDiff {
	out := ServerDiff{FetchedAt: nowString()}
	if diff == nil {
		return out
	}
	out.Checkpoint = displayLabel(diff.Checkpoint)
	if !diff.CapturedAt.IsZero() {
		out.CapturedAt = diff.CapturedAt.Local().Format(time.RFC822)
	}
	out.Total = diff.Total()

	groups := []struct {
		group   string
		added   []string
		removed []string
	}{
		{"package", diff.AddedPackages, diff.RemovedPackages},
		{"service", diff.AddedServices, diff.RemovedServices},
		{"port", diff.AddedPorts, diff.RemovedPorts},
		{"file", diff.AddedFiles, diff.RemovedFiles},
	}

	out.Changes = make([]DiffEntry, 0, out.Total)
	for _, g := range groups {
		for _, value := range g.added {
			out.Changes = append(out.Changes, DiffEntry{Kind: "added", Group: g.group, Value: value})
		}
		for _, value := range g.removed {
			out.Changes = append(out.Changes, DiffEntry{Kind: "removed", Group: g.group, Value: value})
		}
	}
	for _, value := range diff.ModifiedFiles {
		out.Changes = append(out.Changes, DiffEntry{Kind: "modified", Group: "file", Value: value})
	}
	return out
}

// displayLabel strips the internal checkpoint- prefix so the UI shows the name
// the user typed rather than the storage key.
func displayLabel(name string) string {
	return strings.TrimPrefix(name, checkpointPrefix)
}
