package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stoicsoft/vpsbox/internal/config"
)

func newBucketStore(t *testing.T) (*Store, config.Paths) {
	t.Helper()
	base := t.TempDir()
	paths := config.FromBase(base, filepath.Join(base, "hosts"))
	if err := paths.Ensure(); err != nil {
		t.Fatalf("ensure paths: %v", err)
	}
	return NewStore(paths), paths
}

func TestLoadBucketsOnMissingFileIsEmpty(t *testing.T) {
	store, _ := newBucketStore(t)

	buckets, err := store.LoadBuckets()
	if err != nil {
		t.Fatalf("load buckets: %v", err)
	}
	if len(buckets) != 0 {
		t.Errorf("buckets = %v, want none", buckets)
	}

	objStore, err := store.LoadObjStore()
	if err != nil {
		t.Fatalf("load objstore: %v", err)
	}
	if objStore != nil {
		t.Errorf("objstore = %+v, want nil", objStore)
	}
}

func TestUpsertBucketRoundTripsAndSorts(t *testing.T) {
	store, _ := newBucketStore(t)

	for _, name := range []string{"uploads", "backups", "media"} {
		if err := store.UpsertBucket(Bucket{Name: name}); err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
	}

	buckets, err := store.LoadBuckets()
	if err != nil {
		t.Fatalf("load buckets: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("got %d buckets, want 3", len(buckets))
	}
	want := []string{"backups", "media", "uploads"}
	for i, name := range want {
		if buckets[i].Name != name {
			t.Errorf("bucket %d = %q, want %q", i, buckets[i].Name, name)
		}
	}
	for _, bucket := range buckets {
		if bucket.CreatedAt.IsZero() {
			t.Errorf("bucket %q has no creation time", bucket.Name)
		}
	}
}

func TestUpsertBucketPreservesCreatedAt(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertBucket(Bucket{Name: "backups"}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, err := store.GetBucket("backups")
	if err != nil {
		t.Fatalf("get bucket: %v", err)
	}

	if err := store.UpsertBucket(Bucket{Name: "backups"}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	second, err := store.GetBucket("backups")
	if err != nil {
		t.Fatalf("get bucket again: %v", err)
	}

	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("CreatedAt changed on re-upsert: %v then %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestDeleteBucketAndGetMissing(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.UpsertBucket(Bucket{Name: "backups"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := store.DeleteBucket("backups"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := store.GetBucket("backups"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("GetBucket after delete = %v, want os.ErrNotExist", err)
	}
}

// The object store and the bucket list share one file, so writing either must not
// wipe the other.
func TestObjStoreAndBucketsSurviveEachOthersWrites(t *testing.T) {
	store, _ := newBucketStore(t)

	if err := store.SaveObjStore(ObjStore{
		Provider:  "minio",
		Endpoint:  "http://127.0.0.1:9000",
		Port:      9000,
		Region:    "us-east-1",
		AccessKey: "vpsboxKEY",
		SecretKey: "supersecret",
		PID:       4242,
	}); err != nil {
		t.Fatalf("save objstore: %v", err)
	}

	if err := store.UpsertBucket(Bucket{Name: "backups"}); err != nil {
		t.Fatalf("upsert bucket: %v", err)
	}

	// Writing a bucket must leave the endpoint intact.
	objStore, err := store.LoadObjStore()
	if err != nil {
		t.Fatalf("load objstore: %v", err)
	}
	if objStore == nil || objStore.Port != 9000 || objStore.AccessKey != "vpsboxKEY" {
		t.Fatalf("objstore lost after a bucket write: %+v", objStore)
	}

	// And re-saving the endpoint must leave the buckets intact.
	objStore.PID = 9999
	if err := store.SaveObjStore(*objStore); err != nil {
		t.Fatalf("re-save objstore: %v", err)
	}
	buckets, err := store.LoadBuckets()
	if err != nil {
		t.Fatalf("load buckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Name != "backups" {
		t.Errorf("buckets lost after an objstore write: %v", buckets)
	}
}

// buckets.json holds the object store secret key, so it must not be readable by
// other users on the machine.
func TestBucketsFileIsNotWorldReadable(t *testing.T) {
	store, paths := newBucketStore(t)

	if err := store.SaveObjStore(ObjStore{Port: 9000, AccessKey: "k", SecretKey: "supersecret"}); err != nil {
		t.Fatalf("save objstore: %v", err)
	}

	info, err := os.Stat(paths.BucketsPath)
	if err != nil {
		t.Fatalf("stat buckets file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("buckets.json mode = %o, want 600 — it holds the secret key", mode)
	}
}

// A pre-existing file created with looser permissions must be tightened, since
// os.WriteFile only applies the mode when it creates the file.
func TestBucketsFilePermissionsAreRepaired(t *testing.T) {
	store, paths := newBucketStore(t)

	if err := os.WriteFile(paths.BucketsPath, []byte(`{"version":1,"buckets":[]}`), 0o644); err != nil {
		t.Fatalf("seed loose file: %v", err)
	}

	if err := store.SaveObjStore(ObjStore{Port: 9000, AccessKey: "k", SecretKey: "supersecret"}); err != nil {
		t.Fatalf("save objstore: %v", err)
	}

	info, err := os.Stat(paths.BucketsPath)
	if err != nil {
		t.Fatalf("stat buckets file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("buckets.json mode = %o, want 600 after repair", mode)
	}
}

func TestBucketsFileRecordsItsSchemaVersion(t *testing.T) {
	store, paths := newBucketStore(t)

	if err := store.UpsertBucket(Bucket{Name: "backups"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var f bucketsFile
	if err := readJSON(paths.BucketsPath, &f); err != nil {
		t.Fatalf("read buckets file: %v", err)
	}
	if f.Version != bucketsVersion {
		t.Errorf("version = %d, want %d", f.Version, bucketsVersion)
	}
}
