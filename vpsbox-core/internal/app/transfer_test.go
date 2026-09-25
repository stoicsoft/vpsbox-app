package app

import (
	"strings"
	"testing"

	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/migrate"
)

func TestEndpointForTarget(t *testing.T) {
	dir := t.TempDir()
	manager := &Manager{paths: config.FromBase(dir, dir+"/hosts")}

	ep := manager.endpointForTarget(migrate.Target{User: "root", Host: "203.0.113.10", Port: 22})
	joined := strings.Join(ep.args, " ")
	if !strings.HasSuffix(joined, "root@203.0.113.10") {
		t.Fatalf("destination missing: %s", joined)
	}
	if !strings.Contains(joined, "StrictHostKeyChecking=accept-new") {
		t.Fatalf("unknown hosts must be recorded, changed ones refused: %s", joined)
	}
	if strings.Contains(joined, "-p ") || strings.Contains(joined, "-i ") {
		t.Fatalf("default port/key should add no flags: %s", joined)
	}

	ep = manager.endpointForTarget(migrate.Target{User: "admin", Host: "vps.example.com", Port: 2222, KeyPath: "/keys/id"})
	joined = strings.Join(ep.args, " ")
	if !strings.Contains(joined, "-p 2222") || !strings.Contains(joined, "-i /keys/id") {
		t.Fatalf("port and key flags missing: %s", joined)
	}
}

func TestProgressReaderCounts(t *testing.T) {
	var reported int64
	pr := &progressReader{r: strings.NewReader(strings.Repeat("x", 1000)), report: func(n int64) { reported = n }}
	buf := make([]byte, 256)
	for {
		if _, err := pr.Read(buf); err != nil {
			break
		}
	}
	if pr.total != 1000 {
		t.Fatalf("total: %d", pr.total)
	}
	// lastTime was zero, so the very first read reports.
	if reported == 0 {
		t.Fatalf("expected at least one progress report")
	}
}

func TestBoundedBufferKeepsTail(t *testing.T) {
	var b boundedBuffer
	if _, err := b.Write([]byte(strings.Repeat("a", boundedBufferLimit))); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("THE-END")); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if len(got) > boundedBufferLimit || !strings.HasSuffix(got, "THE-END") {
		t.Fatalf("tail not kept: len=%d", len(got))
	}
}

func TestHumanSizes(t *testing.T) {
	if got := humanKB(2048); got != "2 MB" {
		t.Fatalf("2048 KB → %s", got)
	}
	if got := humanKB(3 * 1024 * 1024); got != "3.0 GB" {
		t.Fatalf("3 GB → %s", got)
	}
	if got := humanBytes(512); got != "512 B" {
		t.Fatalf("512 B → %s", got)
	}
}

func TestExpandKeyPath(t *testing.T) {
	if got := expandKeyPath("/abs/key"); got != "/abs/key" {
		t.Fatalf("absolute path changed: %s", got)
	}
	got := expandKeyPath("~/keys/id")
	if strings.HasPrefix(got, "~") || !strings.HasSuffix(got, "/keys/id") {
		t.Fatalf("tilde not expanded: %s", got)
	}
}
