package desktopbackend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	vpsapp "github.com/stoicsoft/vpsbox/internal/app"
)

const (
	updateOwner       = "stoicsoft"
	updateRepo        = "vpsbox-app"
	updateCheckPeriod = 6 * time.Hour
)

type UpdateInfo struct {
	Available  bool   `json:"available"`
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	URL        string `json:"url"`
	CheckedAt  string `json:"checkedAt,omitempty"`
	ReleasedAt string `json:"releasedAt,omitempty"`
	Error      string `json:"error,omitempty"`

	// The fields below describe whether this build can install the update
	// itself. CanInstall is only true when a release asset matches this
	// OS/arch *and* the app is installed somewhere it can be replaced;
	// otherwise Blocker says why, and the UI falls back to the download link.
	AssetName  string `json:"assetName,omitempty"`
	AssetURL   string `json:"assetURL,omitempty"`
	AssetSize  int64  `json:"assetSize,omitempty"`
	CanInstall bool   `json:"canInstall"`
	Blocker    string `json:"blocker,omitempty"`

	// checksums points at the release's signed SHA256SUMS; internal only.
	checksums releaseChecksums
}

// releaseAsset is one downloadable file attached to a GitHub release.
type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type updateCache struct {
	mu        sync.Mutex
	info      *UpdateInfo
	checkedAt time.Time
}

var cachedUpdate updateCache

func checkForUpdate(force bool) UpdateInfo {
	cachedUpdate.mu.Lock()
	defer cachedUpdate.mu.Unlock()

	if !force && cachedUpdate.info != nil && time.Since(cachedUpdate.checkedAt) < updateCheckPeriod {
		return *cachedUpdate.info
	}

	info := fetchUpdateInfo()
	cachedUpdate.info = &info
	cachedUpdate.checkedAt = time.Now()
	return info
}

func fetchUpdateInfo() UpdateInfo {
	current := vpsapp.Version

	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", updateOwner, updateRepo), nil)
	if err != nil {
		return updateError(current, err)
	}
	req.Header.Set("User-Agent", "vpsbox-desktop")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return updateError(current, fmt.Errorf("could not reach GitHub: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return updateError(current, fmt.Errorf("GitHub returned %s", resp.Status))
	}

	var release struct {
		TagName     string         `json:"tag_name"`
		HTMLURL     string         `json:"html_url"`
		PublishedAt string         `json:"published_at"`
		Assets      []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return updateError(current, fmt.Errorf("could not read release information: %w", err))
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	if !validReleaseVersion(latest) || parseSemver(latest) == nil {
		return updateError(current, fmt.Errorf("release %q does not contain a valid version", release.TagName))
	}

	info := UpdateInfo{
		Available:  isNewer(latest, current),
		Current:    current,
		Latest:     latest,
		URL:        release.HTMLURL,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
		ReleasedAt: release.PublishedAt,
	}
	if info.Available {
		describeInstallability(&info, release.Assets)
	}
	return info
}

// describeInstallability fills in the self-update fields: which asset this
// platform would download, and whether installing it in place is possible.
// Both questions can fail independently, and each failure is a message the
// UI shows next to the plain "download it yourself" link.
func describeInstallability(info *UpdateInfo, assets []releaseAsset) {
	asset, err := selectUpdateAsset(assets, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		info.Blocker = err.Error()
		return
	}
	info.AssetName = asset.Name
	info.AssetURL = asset.URL
	info.AssetSize = asset.Size

	checksums, ok := findReleaseChecksums(assets)
	if !ok {
		info.Blocker = "this release is not signed for in-app install — download it from the release page"
		return
	}
	info.checksums = checksums

	if err := canSelfUpdate(); err != nil {
		info.Blocker = err.Error()
		return
	}
	info.CanInstall = true
}

// selectUpdateAsset picks the release asset this OS/arch can install from.
// The names come from the release workflow: VPSBox-<version-dashes>-macos.zip
// / -macos.dmg / -windows-setup.exe / -windows.exe / -linux-<arch>.tar.gz.
// Matching is deliberately loose on the version part (it changes every
// release) and strict on the platform part (installing the wrong build is
// worse than not offering to install at all).
func selectUpdateAsset(assets []releaseAsset, goos, goarch string) (releaseAsset, error) {
	match := func(want func(name string) bool) (releaseAsset, bool) {
		for _, asset := range assets {
			if asset.URL == "" {
				continue
			}
			if want(strings.ToLower(asset.Name)) {
				return asset, true
			}
		}
		return releaseAsset{}, false
	}

	switch goos {
	case "darwin":
		// The zip is a ditto archive of the .app bundle, which is exactly
		// what the swap needs. The DMG is the fallback for older releases.
		if asset, ok := match(func(n string) bool {
			return strings.Contains(n, "macos") && strings.HasSuffix(n, ".zip")
		}); ok {
			return asset, nil
		}
		if asset, ok := match(func(n string) bool {
			return strings.Contains(n, "macos") && strings.HasSuffix(n, ".dmg")
		}); ok {
			return asset, nil
		}
	case "windows":
		if asset, ok := match(func(n string) bool {
			return strings.Contains(n, "windows") && strings.Contains(n, "setup") && strings.HasSuffix(n, ".exe")
		}); ok {
			return asset, nil
		}
		if asset, ok := match(func(n string) bool {
			return strings.Contains(n, "windows") && strings.HasSuffix(n, ".exe")
		}); ok {
			return asset, nil
		}
	case "linux":
		aliases := archAliases(goarch)
		if asset, ok := match(func(n string) bool {
			if !strings.Contains(n, "linux") || !(strings.HasSuffix(n, ".tar.gz") || strings.HasSuffix(n, ".tgz")) {
				return false
			}
			for _, alias := range aliases {
				if strings.Contains(n, alias) {
					return true
				}
			}
			return false
		}); ok {
			return asset, nil
		}
	}

	return releaseAsset{}, fmt.Errorf("this release has no download for %s/%s — install it by hand from the release page", goos, goarch)
}

// archAliases maps a Go arch to the spellings release artifacts tend to use.
func archAliases(goarch string) []string {
	switch goarch {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	default:
		return []string{goarch}
	}
}

func updateError(current string, err error) UpdateInfo {
	return UpdateInfo{
		Current:   current,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Error:     err.Error(),
	}
}

// isNewer returns true if latest is a higher semver than current.
func isNewer(latest, current string) bool {
	latestParts := parseSemver(latest)
	currentParts := parseSemver(current)
	if latestParts == nil || currentParts == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

// parseSemver extracts major.minor.patch from a version string,
// stripping any pre-release suffix (e.g. "0.1.0-dev" -> [0,1,0]).
func parseSemver(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}
