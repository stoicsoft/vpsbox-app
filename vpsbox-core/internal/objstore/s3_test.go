package objstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixedClient returns a client whose signatures are deterministic, so a signing
// change shows up as a test failure rather than a runtime auth error.
func fixedClient(endpoint string) *client {
	c := newClient(endpoint, "us-east-1", "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	c.now = func() time.Time { return time.Date(2024, 5, 17, 12, 0, 0, 0, time.UTC) }
	return c
}

func TestSignProducesStableAuthorizationHeader(t *testing.T) {
	c := fixedClient("http://127.0.0.1:9000")

	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:9000/backups", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	c.sign(req, "/backups", "")

	auth := req.Header.Get("Authorization")

	// The credential scope and signed headers are part of the contract with the
	// server — a change here means every request starts failing auth.
	wantCredential := "Credential=AKIAIOSFODNN7EXAMPLE/20240517/us-east-1/s3/aws4_request"
	if !strings.Contains(auth, wantCredential) {
		t.Errorf("authorization missing credential scope\n got: %s\nwant substring: %s", auth, wantCredential)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("authorization missing signed headers: %s", auth)
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Errorf("authorization missing algorithm: %s", auth)
	}

	if got := req.Header.Get("X-Amz-Date"); got != "20240517T120000Z" {
		t.Errorf("x-amz-date = %q, want 20240517T120000Z", got)
	}
	if got := req.Header.Get("X-Amz-Content-Sha256"); got != emptyPayloadHash {
		t.Errorf("content hash = %q, want the empty-body hash", got)
	}

	// Signing the identical request twice must produce the identical signature;
	// anything else means we are mixing in something nondeterministic.
	second, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1:9000/backups", nil)
	c.sign(second, "/backups", "")
	if second.Header.Get("Authorization") != auth {
		t.Error("signing the same request twice produced different signatures")
	}
}

func TestSignDiffersByMethodAndPath(t *testing.T) {
	c := fixedClient("http://127.0.0.1:9000")

	sig := func(method, path, query string) string {
		req, _ := http.NewRequest(method, "http://127.0.0.1:9000"+path, nil)
		c.sign(req, path, query)
		return req.Header.Get("Authorization")
	}

	put := sig(http.MethodPut, "/backups", "")
	del := sig(http.MethodDelete, "/backups", "")
	other := sig(http.MethodPut, "/archive", "")
	queried := sig(http.MethodGet, "/backups", "list-type=2&max-keys=1")

	if put == del {
		t.Error("PUT and DELETE signed identically — method is not in the signature")
	}
	if put == other {
		t.Error("two different buckets signed identically — path is not in the signature")
	}
	if sig(http.MethodGet, "/backups", "") == queried {
		t.Error("query string is not in the signature")
	}
}

func TestCanonicalQuerySortsAndKeepsEmptyValues(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"sorted", "list-type=2&max-keys=1", "list-type=2&max-keys=1"},
		{"unsorted", "max-keys=1&list-type=2", "list-type=2&max-keys=1"},
		{"empty value", "versions=", "versions="},
		{"encoded value", "prefix=a b", "prefix=a+b"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonicalQuery(tc.in); got != tc.want {
				t.Errorf("canonicalQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestS3EscapePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/backups", "/backups"},
		{"/backups/db.sql", "/backups/db.sql"},
		{"/backups/with space", "/backups/with%20space"},
		{"/backups/a+b", "/backups/a%2Bb"},
		{"/backups/100%", "/backups/100%25"},
		{"/backups/tilde~dash-under_dot.", "/backups/tilde~dash-under_dot."},
		{"/backups/nested/deep/key", "/backups/nested/deep/key"},
	}

	for _, tc := range cases {
		if got := s3EscapePath(tc.in); got != tc.want {
			t.Errorf("s3EscapePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// stubServer stands in for the object store so the client can be exercised
// without a real server. It records what it was asked for.
type stubServer struct {
	buckets []string
	objects map[string][]string
	deleted []string
	// requests records "METHOD path?query" for each call.
	requests []string
}

func newStubServer() *stubServer {
	return &stubServer{objects: map[string][]string{}}
}

func (s *stubServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := r.Method + " " + r.URL.EscapedPath()
		if r.URL.RawQuery != "" {
			record += "?" + r.URL.RawQuery
		}
		s.requests = append(s.requests, record)

		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		trimmed := strings.Trim(r.URL.Path, "/")
		parts := strings.SplitN(trimmed, "/", 2)
		bucket := parts[0]

		switch {
		case r.Method == http.MethodGet && trimmed == "":
			w.Header().Set("Content-Type", "application/xml")
			var sb strings.Builder
			sb.WriteString(`<ListAllMyBucketsResult><Buckets>`)
			for _, name := range s.buckets {
				fmt.Fprintf(&sb, `<Bucket><Name>%s</Name><CreationDate>2024-05-17T12:00:00.000Z</CreationDate></Bucket>`, name)
			}
			sb.WriteString(`</Buckets></ListAllMyBucketsResult>`)
			_, _ = w.Write([]byte(sb.String()))

		case r.Method == http.MethodPut && len(parts) == 1:
			for _, existing := range s.buckets {
				if existing == bucket {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`<Error><Code>BucketAlreadyOwnedByYou</Code><Message>already yours</Message></Error>`))
					return
				}
			}
			s.buckets = append(s.buckets, bucket)
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodGet && len(parts) == 1:
			keys := s.objects[bucket]
			max := len(keys)
			if raw := r.URL.Query().Get("max-keys"); raw == "1" && max > 1 {
				max = 1
			}
			w.Header().Set("Content-Type", "application/xml")
			var sb strings.Builder
			fmt.Fprintf(&sb, `<ListBucketResult><KeyCount>%d</KeyCount><IsTruncated>false</IsTruncated>`, max)
			for _, key := range keys[:max] {
				fmt.Fprintf(&sb, `<Contents><Key>%s</Key></Contents>`, key)
			}
			sb.WriteString(`</ListBucketResult>`)
			_, _ = w.Write([]byte(sb.String()))

		case r.Method == http.MethodDelete && len(parts) == 2:
			key := parts[1]
			s.deleted = append(s.deleted, bucket+"/"+key)
			remaining := make([]string, 0, len(s.objects[bucket]))
			for _, existing := range s.objects[bucket] {
				if existing != key {
					remaining = append(remaining, existing)
				}
			}
			s.objects[bucket] = remaining
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && len(parts) == 1:
			if len(s.objects[bucket]) > 0 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`<Error><Code>BucketNotEmpty</Code><Message>not empty</Message></Error>`))
				return
			}
			remaining := make([]string, 0, len(s.buckets))
			for _, existing := range s.buckets {
				if existing != bucket {
					remaining = append(remaining, existing)
				}
			}
			s.buckets = remaining
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<Error><Code>NoSuchBucket</Code><Message>missing</Message></Error>`))
		}
	})
}

func TestClientBucketLifecycle(t *testing.T) {
	stub := newStubServer()
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	c := fixedClient(server.URL)
	ctx := context.Background()

	if err := c.CreateBucket(ctx, "backups"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// Creating the same bucket again must succeed — callers repeat this.
	if err := c.CreateBucket(ctx, "backups"); err != nil {
		t.Fatalf("re-create bucket should be a no-op, got: %v", err)
	}

	names, err := c.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("list buckets: %v", err)
	}
	if len(names) != 1 || names[0] != "backups" {
		t.Fatalf("list buckets = %v, want [backups]", names)
	}

	empty, err := c.BucketEmpty(ctx, "backups")
	if err != nil {
		t.Fatalf("bucket empty: %v", err)
	}
	if !empty {
		t.Error("fresh bucket should be empty")
	}

	if err := c.DeleteBucket(ctx, "backups"); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}
	names, err = c.ListBuckets(ctx)
	if err != nil {
		t.Fatalf("list buckets after delete: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("buckets after delete = %v, want none", names)
	}
}

func TestClientBucketEmptyReportsFalseWithObjects(t *testing.T) {
	stub := newStubServer()
	stub.buckets = []string{"backups"}
	stub.objects["backups"] = []string{"db.sql", "logs/app.log"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	c := fixedClient(server.URL)

	empty, err := c.BucketEmpty(context.Background(), "backups")
	if err != nil {
		t.Fatalf("bucket empty: %v", err)
	}
	if empty {
		t.Error("bucket with objects reported as empty")
	}
}

func TestClientEmptyBucketDeletesEveryKey(t *testing.T) {
	stub := newStubServer()
	stub.buckets = []string{"backups"}
	stub.objects["backups"] = []string{"db.sql", "nested/with space.txt"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	c := fixedClient(server.URL)
	ctx := context.Background()

	if err := c.emptyBucket(ctx, "backups"); err != nil {
		t.Fatalf("empty bucket: %v", err)
	}
	if len(stub.deleted) != 2 {
		t.Fatalf("deleted %v, want 2 keys", stub.deleted)
	}
	// A key with a space must survive the round trip intact, which only works if
	// signing and the wire path agree on escaping.
	found := false
	for _, key := range stub.deleted {
		if key == "backups/nested/with space.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("key with a space was not deleted correctly: %v", stub.deleted)
	}

	if err := c.DeleteBucket(ctx, "backups"); err != nil {
		t.Fatalf("delete emptied bucket: %v", err)
	}
}

func TestClientSurfacesS3ErrorCode(t *testing.T) {
	stub := newStubServer()
	stub.buckets = []string{"backups"}
	stub.objects["backups"] = []string{"db.sql"}
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	c := fixedClient(server.URL)

	err := c.DeleteBucket(context.Background(), "backups")
	if err == nil {
		t.Fatal("deleting a non-empty bucket should fail")
	}

	var failure *apiError
	if !errors.As(err, &failure) {
		t.Fatalf("error %v is not an *apiError", err)
	}
	if failure.Code != "BucketNotEmpty" {
		t.Errorf("error code = %q, want BucketNotEmpty", failure.Code)
	}
}

func TestClientUsesPathStyleAddressing(t *testing.T) {
	stub := newStubServer()
	server := httptest.NewServer(stub.handler())
	defer server.Close()

	c := fixedClient(server.URL)
	if err := c.CreateBucket(context.Background(), "backups"); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	// Path-style is required on the host side: the endpoint is an IP, which
	// cannot carry a bucket as a subdomain.
	if len(stub.requests) == 0 || stub.requests[0] != "PUT /backups" {
		t.Errorf("requests = %v, want first to be PUT /backups", stub.requests)
	}
}
