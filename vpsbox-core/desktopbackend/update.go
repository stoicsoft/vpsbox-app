package desktopbackend

import (
	"encoding/json"
	"fmt"
	"net/http"
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
}

type updateCache struct {
	mu        sync.Mutex
	info      *UpdateInfo
	checkedAt time.Time
}

var cachedUpdate updateCache

func checkForUpdate() UpdateInfo {
	cachedUpdate.mu.Lock()
	defer cachedUpdate.mu.Unlock()

	if cachedUpdate.info != nil && time.Since(cachedUpdate.checkedAt) < updateCheckPeriod {
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
		return UpdateInfo{Current: current}
	}
	req.Header.Set("User-Agent", "vpsbox-desktop")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{Current: current}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return UpdateInfo{Current: current}
	}

	var release struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return UpdateInfo{Current: current}
	}

	latest := strings.TrimPrefix(release.TagName, "v")

	return UpdateInfo{
		Available:  isNewer(latest, current),
		Current:    current,
		Latest:     latest,
		URL:        release.HTMLURL,
		CheckedAt:  time.Now().UTC().Format(time.RFC3339),
		ReleasedAt: release.PublishedAt,
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
