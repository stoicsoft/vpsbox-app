package objstore

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
)

// TestClientAgainstLiveServer exercises the hand-rolled SigV4 signing against a
// real S3 server. The stub in s3_test.go proves the client is self-consistent;
// only this proves the signatures are actually correct.
//
// It is skipped unless VPSBOX_OBJSTORE_E2E_ENDPOINT is set, so `go test ./...`
// stays hermetic. To run it:
//
//	VPSBOX_OBJSTORE_E2E_ENDPOINT=http://127.0.0.1:3900 \
//	VPSBOX_OBJSTORE_E2E_ACCESS_KEY=GK... \
//	VPSBOX_OBJSTORE_E2E_SECRET_KEY=... \
//	VPSBOX_OBJSTORE_E2E_REGION=garage \
//	go test ./internal/objstore -run Live -v
func TestClientAgainstLiveServer(t *testing.T) {
	endpoint := os.Getenv("VPSBOX_OBJSTORE_E2E_ENDPOINT")
	if endpoint == "" {
		t.Skip("set VPSBOX_OBJSTORE_E2E_ENDPOINT to run against a real server")
	}
	region := os.Getenv("VPSBOX_OBJSTORE_E2E_REGION")
	if region == "" {
		region = defaultRegion
	}

	c := newClient(
		endpoint,
		region,
		os.Getenv("VPSBOX_OBJSTORE_E2E_ACCESS_KEY"),
		os.Getenv("VPSBOX_OBJSTORE_E2E_SECRET_KEY"),
	)
	ctx := context.Background()
	bucket := "vpsbox-e2e-bucket"

	// Clean up whatever a previous failed run left behind.
	t.Cleanup(func() {
		_ = c.emptyBucket(ctx, bucket)
		_ = c.DeleteBucket(ctx, bucket)
	})

	if err := c.CreateBucket(ctx, bucket); err != nil {
		t.Fatalf("create bucket against live server: %v", err)
	}

	// Repeating a create must not error — the manager relies on that.
	if err := c.CreateBucket(ctx, bucket); err != nil {
		t.Errorf("re-creating a bucket should be a no-op, got: %v", err)
	}

	names, err := c.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	found := false
	for _, name := range names {
		if name == bucket {
			found = true
		}
	}
	if !found {
		t.Fatalf("bucket %q missing from live listing %v", bucket, names)
	}

	empty, err := c.BucketEmpty(ctx, bucket)
	if err != nil {
		t.Fatalf("bucket empty on live server: %v", err)
	}
	if !empty {
		t.Error("fresh live bucket reported as non-empty")
	}

	// Keys with a space and a slash are where escaping bugs surface, so upload
	// one of each rather than a simple name.
	keys := []string{"db.sql", "nested/with space.txt"}
	for _, key := range keys {
		if err := putObject(ctx, c, bucket, key, "hello from vpsbox"); err != nil {
			t.Fatalf("put %q: %v", key, err)
		}
	}

	empty, err = c.BucketEmpty(ctx, bucket)
	if err != nil {
		t.Fatalf("bucket empty after upload: %v", err)
	}
	if empty {
		t.Error("bucket with objects reported as empty")
	}

	page, next, err := c.listObjectPage(ctx, bucket, "")
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if next != "" {
		t.Errorf("two objects should fit one page, got continuation token %q", next)
	}
	if len(page) != len(keys) {
		t.Fatalf("listed %v, want %v", page, keys)
	}
	for _, want := range keys {
		found := false
		for _, got := range page {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("key %q missing from live listing %v", want, page)
		}
	}

	// A non-empty bucket must be refused, which is what protects user data.
	if err := c.DeleteBucket(ctx, bucket); err == nil {
		t.Error("live server accepted deletion of a non-empty bucket")
	}

	if err := c.emptyBucket(ctx, bucket); err != nil {
		t.Fatalf("empty bucket: %v", err)
	}
	if err := c.DeleteBucket(ctx, bucket); err != nil {
		t.Fatalf("delete emptied bucket: %v", err)
	}
}

// putObject uploads a small object. The client does not need PUT-with-body for
// normal operation, so this signs its own request rather than widening the
// client's API for a test.
func putObject(ctx context.Context, c *client, bucket, key, body string) error {
	payload := []byte(body)
	path := "/" + bucket + "/" + key
	escaped := s3EscapePath(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint+escaped, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.URL.Path = path
	req.URL.RawPath = escaped
	req.ContentLength = int64(len(payload))
	c.signWithPayload(req, escaped, "", payload)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("put %s: status %d", key, resp.StatusCode)
	}
	return nil
}
