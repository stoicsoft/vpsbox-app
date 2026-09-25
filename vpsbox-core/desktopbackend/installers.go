package desktopbackend

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	vpsapp "github.com/stoicsoft/vpsbox/internal/app"
)

// installAllPrerequisites is declared (with a build-tagged body) in
// installers_darwin.go, installers_linux.go, and installers_windows.go.
// Each platform installs Multipass, mkcert, and cloudflared the way that's
// most appropriate for it (package manager, GitHub release, etc.) and
// reports progress through the callback.

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type ghAsset struct {
	Name string
	URL  string
}

// syncLocalDomainsWithPrivileges is the desktop app's stub for writing
// /etc/hosts entries. It needs admin and is currently degraded.
func syncLocalDomainsWithPrivileges(manager *vpsapp.Manager) error {
	_ = manager
	return fmt.Errorf("local hostname syncing is not packaged yet; use the CLI for privileged hosts updates")
}

func latestGitHubAsset(owner, repo string, pattern *regexp.Regexp) (ghAsset, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo), nil)
	if err != nil {
		return ghAsset{}, err
	}
	req.Header.Set("User-Agent", "vpsbox-desktop")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return ghAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ghAsset{}, fmt.Errorf("GitHub API %s/%s returned %s", owner, repo, resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ghAsset{}, err
	}
	for _, candidate := range release.Assets {
		if pattern.MatchString(candidate.Name) {
			return ghAsset{Name: candidate.Name, URL: candidate.URL}, nil
		}
	}
	return ghAsset{}, fmt.Errorf("no release asset matched %s/%s %s", owner, repo, pattern.String())
}

func downloadAsset(url, name string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "vpsbox-desktop")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}

	dir, err := os.MkdirTemp("", "vpsbox-desktop-download-*")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}
	return path, nil
}

// vpsboxBinDir returns ~/.vpsbox/bin and ensures it exists. Used on Linux
// and Windows as a per-user install location for downloaded binaries
// (mkcert, cloudflared) so we don't need admin just to drop a file in PATH.
// desktop/main.go prepends this dir to PATH at startup so executil finds it.
func vpsboxBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".vpsbox", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func singleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
