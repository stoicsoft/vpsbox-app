package registry

import (
	"errors"
	"os"
	"testing"
)

func TestLoadWorkspacesOnMissingFileIsEmpty(t *testing.T) {
	store, _ := newBucketStore(t)

	workspaces, err := store.LoadWorkspaces()
	if err != nil {
		t.Fatalf("load workspaces: %v", err)
	}
	if len(workspaces) != 0 {
		t.Errorf("workspaces = %v, want none", workspaces)
	}
}

func TestUpsertWorkspaceRoundTrips(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertWorkspace(Workspace{
		Name:  "alpha",
		CIDR:  "10.42.1.0/24",
		Index: 1,
		Members: []WorkspaceMember{
			{Instance: "db", PrivateIP: "10.42.1.10"},
		},
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.GetWorkspace("alpha")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CIDR != "10.42.1.0/24" || got.Index != 1 {
		t.Errorf("workspace = %+v, want the 10.42.1.0/24 record", got)
	}
	if len(got.Members) != 1 || got.Members[0].Instance != "db" {
		t.Errorf("members = %v, want db", got.Members)
	}
	if got.CreatedAt.IsZero() {
		t.Error("workspace has no creation time")
	}
}

func TestUpsertWorkspacePreservesCreatedAt(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertWorkspace(Workspace{Name: "alpha", CIDR: "10.42.1.0/24", Index: 1}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, err := store.GetWorkspace("alpha")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// Adding a member re-upserts the whole record; that must not look like a new
	// workspace.
	first.Members = append(first.Members, WorkspaceMember{Instance: "api", PrivateIP: "10.42.1.11"})
	if err := store.UpsertWorkspace(*first); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	second, err := store.GetWorkspace("alpha")
	if err != nil {
		t.Fatalf("get again: %v", err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("CreatedAt changed: %v then %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestDeleteWorkspaceFreesTheName(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertWorkspace(Workspace{Name: "alpha", CIDR: "10.42.1.0/24", Index: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.DeleteWorkspace("alpha"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetWorkspace("alpha"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("get after delete = %v, want os.ErrNotExist", err)
	}
}

func TestWorkspaceForInstanceFindsMembership(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertWorkspace(Workspace{
		Name: "alpha", CIDR: "10.42.1.0/24", Index: 1,
		Members: []WorkspaceMember{{Instance: "db", PrivateIP: "10.42.1.10"}},
	}); err != nil {
		t.Fatalf("upsert alpha: %v", err)
	}
	if err := store.UpsertWorkspace(Workspace{
		Name: "beta", CIDR: "10.42.2.0/24", Index: 2,
		Members: []WorkspaceMember{{Instance: "web", PrivateIP: "10.42.2.10"}},
	}); err != nil {
		t.Fatalf("upsert beta: %v", err)
	}

	found, err := store.WorkspaceForInstance("web")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found.Name != "beta" {
		t.Errorf("workspace = %q, want beta", found.Name)
	}

	// This lookup is what stops a sandbox joining two networks and bridging them.
	if _, err := store.WorkspaceForInstance("unattached"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("lookup of a non-member = %v, want os.ErrNotExist", err)
	}
}

func TestWorkspacesSortedByName(t *testing.T) {
	store, _ := newBucketStore(t)

	for _, name := range []string{"zeta", "alpha", "mid"} {
		if err := store.UpsertWorkspace(Workspace{Name: name, CIDR: "10.42.1.0/24", Index: 1}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
	}

	workspaces, err := store.LoadWorkspaces()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, expected := range want {
		if workspaces[i].Name != expected {
			t.Errorf("position %d = %q, want %q", i, workspaces[i].Name, expected)
		}
	}
}

func TestWorkspacesFileRecordsItsSchemaVersion(t *testing.T) {
	store, paths := newBucketStore(t)

	if err := store.UpsertWorkspace(Workspace{Name: "alpha", CIDR: "10.42.1.0/24", Index: 1}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var f workspacesFile
	if err := readJSON(paths.WorkspacesPath, &f); err != nil {
		t.Fatalf("read workspaces file: %v", err)
	}
	if f.Version != workspacesVersion {
		t.Errorf("version = %d, want %d", f.Version, workspacesVersion)
	}
}

// Workspaces and buckets live in different files; neither may disturb the other.
func TestWorkspacesAndBucketsAreIndependent(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertBucket(Bucket{Name: "backups"}); err != nil {
		t.Fatalf("upsert bucket: %v", err)
	}
	if err := store.UpsertWorkspace(Workspace{Name: "alpha", CIDR: "10.42.1.0/24", Index: 1}); err != nil {
		t.Fatalf("upsert workspace: %v", err)
	}

	buckets, err := store.LoadBuckets()
	if err != nil {
		t.Fatalf("load buckets: %v", err)
	}
	if len(buckets) != 1 {
		t.Errorf("buckets = %v, want the one recorded", buckets)
	}

	workspaces, err := store.LoadWorkspaces()
	if err != nil {
		t.Fatalf("load workspaces: %v", err)
	}
	if len(workspaces) != 1 {
		t.Errorf("workspaces = %v, want the one recorded", workspaces)
	}
}
