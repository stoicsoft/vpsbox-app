package share

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/executil"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

var (
	tunnelURLPattern = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	adjectives       = []string{"coral", "ember", "meadow", "cinder", "frost", "aurora", "harbor", "signal", "pine", "silver"}
	nouns            = []string{"harbor", "ridge", "field", "spring", "trail", "grove", "glade", "crest", "delta", "meadow"}
)

type Manager struct {
	paths config.Paths
	store *registry.Store
}

func NewManager(paths config.Paths, store *registry.Store) *Manager {
	return &Manager{paths: paths, store: store}
}

func (m *Manager) EnsureInstalled(ctx context.Context) error {
	if executil.LookPath("cloudflared") {
		return nil
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		if executil.LookPath("brew") {
			_, err := executil.Run(ctx, "brew", "install", "cloudflared")
			return err
		}
	case "windows":
		if executil.LookPath("winget") {
			_, err := executil.Run(ctx, "winget", "install", "--id", "Cloudflare.cloudflared", "-e")
			return err
		}
	}

	return fmt.Errorf("cloudflared is not installed")
}

func (m *Manager) Create(ctx context.Context, rawTarget string, ttl time.Duration, explicitName string) (*registry.Share, error) {
	if err := m.EnsureInstalled(ctx); err != nil {
		return nil, err
	}

	target, err := NormalizeTarget(rawTarget)
	if err != nil {
		return nil, err
	}

	name := explicitName
	if name == "" {
		name = randomSlug()
	}

	logFile, err := os.OpenFile(m.paths.ShareLogPath(name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()

	var capture bytes.Buffer
	cmd := exec.CommandContext(ctx, "cloudflared", "tunnel", "--url", target)
	cmd.Stdout = io.MultiWriter(logFile, &capture)
	cmd.Stderr = io.MultiWriter(logFile, &capture)
	cmd.SysProcAttr = processGroupAttrs()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if match := tunnelURLPattern.FindString(capture.String()); match != "" {
			share := registry.Share{
				Name:      name,
				URL:       match,
				TargetURL: target,
				Provider:  "trycloudflare",
				PID:       cmd.Process.Pid,
				LogPath:   m.paths.ShareLogPath(name),
				CreatedAt: time.Now().UTC(),
			}
			if ttl > 0 {
				expiresAt := time.Now().Add(ttl).UTC()
				share.ExpiresAt = &expiresAt
			}
			if err := m.store.UpsertShare(share); err != nil {
				return nil, err
			}
			_ = cmd.Process.Release()
			return &share, nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return nil, errors.New("cloudflared exited before returning a public URL")
		}
		time.Sleep(250 * time.Millisecond)
	}

	_ = cmd.Process.Kill()
	return nil, errors.New("timed out waiting for cloudflared tunnel URL")
}

func (m *Manager) List() ([]registry.Share, error) {
	shares, err := m.store.LoadShares()
	if err != nil {
		return nil, err
	}

	alive := make([]registry.Share, 0, len(shares))
	changed := false
	for _, share := range shares {
		if share.ExpiresAt != nil && time.Now().After(*share.ExpiresAt) {
			_ = m.Unshare(share.Name)
			changed = true
			continue
		}
		if share.PID > 0 && !processAlive(share.PID) {
			changed = true
			continue
		}
		alive = append(alive, share)
	}
	if changed {
		_ = m.store.SaveShares(alive)
	}
	return alive, nil
}

func (m *Manager) Unshare(name string) error {
	shares, err := m.store.LoadShares()
	if err != nil {
		return err
	}

	for _, share := range shares {
		if share.Name == name {
			if share.PID > 0 {
				_ = killProcessGroup(share.PID)
			}
			return m.store.DeleteShare(name)
		}
	}

	return os.ErrNotExist
}

func NormalizeTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("target URL is required")
	}

	if !strings.Contains(raw, "://") {
		switch {
		case strings.Contains(raw, "localhost:"),
			strings.Contains(raw, "127.0.0.1:"),
			strings.Count(raw, ":") == 1:
			raw = "http://" + raw
		case strings.HasPrefix(raw, "dev-"),
			strings.Contains(raw, ".vpsbox.local"):
			raw = "https://" + raw
		default:
			raw = "http://" + raw
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid share target %q", raw)
	}

	return parsed.String(), nil
}

func randomSlug() string {
	seeded := rand.New(rand.NewSource(time.Now().UnixNano()))
	return adjectives[seeded.Intn(len(adjectives))] + "-" + nouns[seeded.Intn(len(nouns))]
}
