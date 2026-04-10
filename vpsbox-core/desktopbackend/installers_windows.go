//go:build windows

package desktopbackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/executil"
)

func isWingetAlreadyInstalled(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "already installed") ||
		strings.Contains(lower, "no available upgrade found")
}

func installAllPrerequisites(progress func(string)) error {
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}

	if !executil.LookPath("multipass") {
		report("Installing Multipass (UAC will prompt for admin)")
		if err := installMultipassWindows(); err != nil {
			return err
		}
	}

	report("Installing mkcert into ~/.vpsbox/bin")
	if err := installMKCertWindows(); err != nil {
		return err
	}

	if !executil.LookPath("cloudflared") {
		report("Installing cloudflared into ~/.vpsbox/bin")
		if err := installCloudflaredWindows(); err != nil {
			return err
		}
	}

	report("System packages are installed")
	return nil
}

func installMultipassWindows() error {
	// Prefer winget when available — it's the cleanest user experience and
	// handles UAC, uninstall, and updates for us.
	if executil.LookPath("winget") {
		_, err := executil.Run(context.Background(),
			"winget", "install",
			"--id", "Canonical.Multipass",
			"-e",
			"--accept-source-agreements",
			"--accept-package-agreements",
		)
		if err != nil && isWingetAlreadyInstalled(err.Error()) {
			return nil
		}
		return err
	}

	// Fall back to direct installer download. The Multipass Windows installer
	// triggers UAC itself, so we just need to launch it elevated.
	asset, err := latestGitHubAsset("canonical", "multipass", regexp.MustCompile(`multipass-.*\+win-Win64\.exe$`))
	if err != nil {
		return err
	}
	exePath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(exePath))
	return runAdminCommand(exePath)
}

func installMKCertWindows() error {
	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`mkcert-.*-windows-%s\.exe$`, arch))
	asset, err := latestGitHubAsset("FiloSottile", "mkcert", pattern)
	if err != nil {
		return err
	}
	exePath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(exePath))
	binDir, err := vpsboxBinDir()
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, "mkcert.exe")
	return copyFile(exePath, target, 0o755)
	// Note: mkcert -install on Windows writes to the system root store and
	// requires admin. We skip it here — the TLS manager has a self-signed
	// fallback. Users can run `mkcert -install` manually if they want a
	// trusted local CA.
}

func installCloudflaredWindows() error {
	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	pattern := regexp.MustCompile(fmt.Sprintf(`cloudflared-windows-%s\.exe$`, arch))
	asset, err := latestGitHubAsset("cloudflare", "cloudflared", pattern)
	if err != nil {
		return err
	}
	exePath, err := downloadAsset(asset.URL, asset.Name)
	if err != nil {
		return err
	}
	defer os.RemoveAll(filepath.Dir(exePath))
	binDir, err := vpsboxBinDir()
	if err != nil {
		return err
	}
	target := filepath.Join(binDir, "cloudflared.exe")
	return copyFile(exePath, target, 0o755)
}
