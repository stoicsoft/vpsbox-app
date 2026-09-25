package desktopbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseAssets mirrors what .github/workflows/build.yml publishes for a tag.
func releaseAssets() []releaseAsset {
	return []releaseAsset{
		{Name: "VPSBox-1-4-0-linux-amd64.tar.gz", URL: "https://example.test/linux.tar.gz", Size: 10},
		{Name: "VPSBox-1-4-0-macos.dmg", URL: "https://example.test/macos.dmg", Size: 20},
		{Name: "VPSBox-1-4-0-macos.zip", URL: "https://example.test/macos.zip", Size: 30},
		{Name: "VPSBox-1-4-0-windows-setup.exe", URL: "https://example.test/setup.exe", Size: 40},
		{Name: "VPSBox-1-4-0-windows.exe", URL: "https://example.test/standalone.exe", Size: 50},
	}
}

func TestSelectUpdateAsset(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		assets []releaseAsset
		want   string
	}{
		{name: "macos prefers the app zip", goos: "darwin", goarch: "arm64", assets: releaseAssets(), want: "VPSBox-1-4-0-macos.zip"},
		{
			name:   "macos falls back to the dmg",
			goos:   "darwin",
			goarch: "arm64",
			assets: []releaseAsset{{Name: "VPSBox-1-4-0-macos.dmg", URL: "https://example.test/macos.dmg"}},
			want:   "VPSBox-1-4-0-macos.dmg",
		},
		{name: "windows prefers the installer", goos: "windows", goarch: "amd64", assets: releaseAssets(), want: "VPSBox-1-4-0-windows-setup.exe"},
		{
			name:   "windows falls back to the standalone exe",
			goos:   "windows",
			goarch: "amd64",
			assets: []releaseAsset{{Name: "VPSBox-1-4-0-windows.exe", URL: "https://example.test/standalone.exe"}},
			want:   "VPSBox-1-4-0-windows.exe",
		},
		{name: "linux matches the arch", goos: "linux", goarch: "amd64", assets: releaseAssets(), want: "VPSBox-1-4-0-linux-amd64.tar.gz"},
		{
			name:   "linux accepts the x86_64 spelling",
			goos:   "linux",
			goarch: "amd64",
			assets: []releaseAsset{{Name: "vpsbox-linux-x86_64.tar.gz", URL: "https://example.test/x.tar.gz"}},
			want:   "vpsbox-linux-x86_64.tar.gz",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			asset, err := selectUpdateAsset(test.assets, test.goos, test.goarch)
			if err != nil {
				t.Fatalf("selectUpdateAsset() error = %v", err)
			}
			if asset.Name != test.want {
				t.Fatalf("selectUpdateAsset() = %q, want %q", asset.Name, test.want)
			}
		})
	}
}

func TestSelectUpdateAssetRejectsMismatches(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		assets []releaseAsset
	}{
		{name: "no asset for the arch", goos: "linux", goarch: "arm64", assets: releaseAssets()},
		{name: "no asset at all", goos: "darwin", goarch: "arm64", assets: nil},
		{
			name:   "asset without a download URL",
			goos:   "darwin",
			goarch: "arm64",
			assets: []releaseAsset{{Name: "VPSBox-1-4-0-macos.zip"}},
		},
		{name: "unsupported platform", goos: "freebsd", goarch: "amd64", assets: releaseAssets()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if asset, err := selectUpdateAsset(test.assets, test.goos, test.goarch); err == nil {
				t.Fatalf("selectUpdateAsset() = %q, want an error", asset.Name)
			}
		})
	}
}

func TestSanitizeAssetName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "VPSBox-1-4-0-macos.zip", want: "VPSBox-1-4-0-macos.zip"},
		{in: "../../../etc/passwd", want: "passwd"},
		{in: `..\..\windows\system32\evil.exe`, want: "evil.exe"},
		{in: "/absolute/path.zip", want: "path.zip"},
		{in: "..", want: "vpsbox-update.download"},
		{in: "   ", want: "vpsbox-update.download"},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			if got := sanitizeAssetName(test.in); got != test.want {
				t.Fatalf("sanitizeAssetName(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestDownloadWithProgress(t *testing.T) {
	payload := strings.Repeat("vpsbox", 4096)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "update.bin")
	var last int64
	err := downloadWithProgress(context.Background(), server.URL, dest, int64(len(payload)), func(downloaded, total int64) {
		last = downloaded
	})
	if err != nil {
		t.Fatalf("downloadWithProgress() error = %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading the download: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(payload))
	}
	if last != int64(len(payload)) {
		t.Fatalf("final progress = %d, want %d", last, len(payload))
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("the partial file was left behind")
	}
}

func TestDownloadWithProgressRejectsShortBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("truncated"))
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "update.bin")
	err := downloadWithProgress(context.Background(), server.URL, dest, 4096, nil)
	if err == nil {
		t.Fatal("downloadWithProgress() = nil, want an error for a short download")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("an incomplete download was left in place")
	}
}

func TestDownloadWithProgressReportsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "update.bin")
	if err := downloadWithProgress(context.Background(), server.URL, dest, 0, nil); err == nil {
		t.Fatal("downloadWithProgress() = nil, want an error for a 404")
	}
}

func TestPercentOf(t *testing.T) {
	tests := []struct {
		done, total int64
		want        int
	}{
		{done: 0, total: 100, want: 0},
		{done: 50, total: 100, want: 50},
		{done: 100, total: 100, want: 100},
		{done: 5, total: 0, want: 0},       // unknown size
		{done: 200, total: 100, want: 100}, // server sent more than announced
	}

	for _, test := range tests {
		if got := percentOf(test.done, test.total); got != test.want {
			t.Fatalf("percentOf(%d, %d) = %d, want %d", test.done, test.total, got, test.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{in: 512, want: "512 B"},
		{in: 2048, want: "2.0 KB"},
		{in: 5 * 1024 * 1024, want: "5.0 MB"},
		{in: 3 * 1024 * 1024 * 1024, want: "3.0 GB"},
	}

	for _, test := range tests {
		if got := formatBytes(test.in); got != test.want {
			t.Fatalf("formatBytes(%d) = %q, want %q", test.in, got, test.want)
		}
	}
}

func TestStartDownloadUpdateRefusesWithoutAnUpdate(t *testing.T) {
	app := New()
	if _, err := app.StartDownloadUpdate(); err == nil {
		t.Fatal("StartDownloadUpdate() = nil error, want a refusal when no update is known")
	}

	app.update = &UpdateInfo{Available: true, Latest: "9.9.9", CanInstall: false, Blocker: "installed read-only"}
	_, err := app.StartDownloadUpdate()
	if err == nil || !strings.Contains(err.Error(), "installed read-only") {
		t.Fatalf("StartDownloadUpdate() error = %v, want the blocker explained", err)
	}
}

func TestApplyUpdateRefusesWithoutAStagedPayload(t *testing.T) {
	app := New()
	if err := app.ApplyUpdate(); err == nil {
		t.Fatal("ApplyUpdate() = nil, want an error when nothing has been downloaded")
	}
}

func TestSetUpdateInfoDropsAStalePayload(t *testing.T) {
	app := New()
	app.download = UpdateDownload{State: "ready", Version: "1.4.0"}
	app.staged = &stagedUpdate{Version: "1.4.0"}

	app.setUpdateInfo(UpdateInfo{Available: true, Latest: "1.5.0"})
	if app.staged != nil {
		t.Fatal("a payload staged for an older release survived a new check")
	}
	if got := app.updateDownloadState().State; got != "idle" {
		t.Fatalf("download state = %q, want idle", got)
	}

	// A download for the version still on offer must be left alone.
	app.download = UpdateDownload{State: "ready", Version: "1.5.0"}
	app.staged = &stagedUpdate{Version: "1.5.0"}
	app.setUpdateInfo(UpdateInfo{Available: true, Latest: "1.5.0"})
	if app.staged == nil {
		t.Fatal("a payload staged for the current release was discarded")
	}
}
