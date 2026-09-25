package desktopbackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stoicsoft/vpsbox/internal/config"
)

// Self-update, end to end:
//
//	StartDownloadUpdate  → download the release asset for this platform into
//	                       ~/.vpsbox/updates, then stage it (unpack the .app,
//	                       untar the binary, keep the installer) and verify it.
//	ApplyUpdate          → hand the staged payload to a small helper script that
//	                       waits for this process to exit, swaps the app, and
//	                       relaunches it. The caller quits the app right after.
//
// The swap has to happen from outside the process being replaced, which is why
// it goes through a helper script rather than doing the work inline. Each
// platform supplies canSelfUpdate/stageUpdate/launchUpdateSwap in its own
// selfupdate_<os>.go.

// UpdateDownload is the live state of an in-app update, polled by the frontend
// through GetState.
type UpdateDownload struct {
	State      string `json:"state"` // idle | downloading | preparing | ready | installing | error
	Version    string `json:"version,omitempty"`
	Percent    int    `json:"percent"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Error      string `json:"error,omitempty"`
	JobID      string `json:"jobId,omitempty"`
}

// stagedUpdate is an unpacked, verified update waiting to be swapped in.
type stagedUpdate struct {
	Version string // version being installed, for logs and the relaunch message
	Source  string // what gets moved into place (new bundle, binary, or installer)
	Target  string // what it replaces (installed bundle or binary)
	Dir     string // staging directory to remove once the swap succeeds
}

// updateDownloadTimeout bounds a single download. Release artifacts are tens of
// megabytes; anything slower than this is a broken connection, not a slow one.
const updateDownloadTimeout = 30 * time.Minute

// errDownloadCancelled marks the one failure the user asked for.
var errDownloadCancelled = errors.New("download cancelled")

// swapWaitTicks is how many 100ms turns the helper spends waiting for the app
// to exit before giving up. 60s covers a slow teardown; past that the app is
// assumed to be stuck, and the helper aborts rather than replacing a bundle
// that a live process is still running from.
const swapWaitTicks = 600

// StartDownloadUpdate downloads and stages the pending update in the
// background, returning a job ID immediately. When it finishes, the update is
// ready for ApplyUpdate; progress is reported both on the job and in
// AppState.UpdateDownload.
func (a *App) StartDownloadUpdate() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateDownloadTimeout)

	// Everything from the busy check to claiming the slot happens under one
	// lock. Two callers can reach here at once — the banner button and the
	// Help menu both start downloads — and a check-then-act gap would let both
	// claim it, leak the first context, and race over the updates directory.
	a.mu.Lock()
	info := a.update
	reject := func(err error) (string, error) {
		a.mu.Unlock()
		cancel()
		return "", err
	}
	switch {
	case a.download.State == "downloading" || a.download.State == "preparing":
		return reject(fmt.Errorf("an update is already downloading"))
	case a.download.State == "installing":
		return reject(fmt.Errorf("an update is already being installed"))
	case info == nil || !info.Available:
		return reject(fmt.Errorf("no update is available — check for updates first"))
	case !info.CanInstall && info.Blocker != "":
		return reject(fmt.Errorf("%s", info.Blocker))
	case !info.CanInstall:
		return reject(fmt.Errorf("this build cannot install updates itself"))
	case info.AssetURL == "":
		return reject(fmt.Errorf("release %s has no downloadable build for this platform", info.Latest))
	case info.checksums.SumsURL == "" || info.checksums.SignatureURL == "":
		return reject(fmt.Errorf("VPSBox %s is not signed for in-app install — download it from the release page", info.Latest))
	case !validReleaseVersion(info.Latest):
		return reject(fmt.Errorf("release %q does not contain a valid version", info.Latest))
	}

	asset := releaseAsset{Name: info.AssetName, URL: info.AssetURL, Size: info.AssetSize}
	checksums := info.checksums
	version := info.Latest

	job := a.newJobLocked("app-update", version)
	a.staged = nil
	a.cancelDownload = cancel
	a.download = UpdateDownload{
		State:   "downloading",
		Version: version,
		Total:   asset.Size,
		JobID:   job.ID,
	}
	a.mu.Unlock()

	go a.runJob(job.ID, func(job *Job) error {
		defer cancel()
		staged, err := a.downloadAndStage(ctx, job.ID, asset, checksums, version)
		if err != nil {
			// Cancelling is a choice, not a failure: put the update back the
			// way it was so the button reads "Download and install" again.
			if errors.Is(err, errDownloadCancelled) {
				a.resetDownload()
			} else {
				a.setDownloadError(err)
			}
			return err
		}
		if err := a.finishStaging(staged); err != nil {
			a.setDownloadError(err)
			return err
		}
		a.updateJobMessage(job.ID, fmt.Sprintf("VPSBox %s is ready to install", version))
		return nil
	})

	return job.ID, nil
}

// finishStaging publishes a completed download, unless a newer release turned
// up while it was in flight. A long download can outlive the check that
// started it (watchForUpdates re-checks on a timer), and installing a build
// that has already been superseded is worse than making the user click again.
func (a *App) finishStaging(staged stagedUpdate) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.update != nil && a.update.Latest != staged.Version {
		a.download = UpdateDownload{}
		a.cancelDownload = nil
		a.staged = nil
		return fmt.Errorf("VPSBox %s was released while %s was downloading — check for updates again",
			a.update.Latest, staged.Version)
	}

	a.staged = &staged
	a.download.State = "ready"
	a.download.Percent = 100
	a.cancelDownload = nil
	return nil
}

// CancelUpdateDownload aborts an in-flight download. A staged update that has
// already finished is left alone — cancelling is about stopping the transfer,
// not undoing a completed one.
func (a *App) CancelUpdateDownload() error {
	a.mu.Lock()
	cancel := a.cancelDownload
	a.cancelDownload = nil
	a.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	return nil
}

// ApplyUpdate launches the swap helper for the staged update. It returns as
// soon as the helper is running: the helper waits for this process to exit, so
// the caller must quit the app immediately afterwards.
func (a *App) ApplyUpdate() error {
	// Claim the payload under the same lock that checks for it: the binding is
	// public and the UI can fire twice before the window closes, and two
	// helpers racing to move the same bundle leaves nothing coherent behind.
	a.mu.Lock()
	staged := a.staged
	if staged == nil {
		a.mu.Unlock()
		return fmt.Errorf("no downloaded update is ready to install")
	}
	a.staged = nil
	previous := a.download
	a.download.State = "installing"
	a.mu.Unlock()

	if err := launchUpdateSwap(*staged); err != nil {
		// The swap never started, so the payload is still good — hand it back
		// rather than making the user download it again.
		a.mu.Lock()
		a.staged = staged
		a.download = previous
		a.mu.Unlock()
		return err
	}
	return nil
}

// downloadAndStage fetches the asset, checks it against the release's signed
// checksums, and only then prepares it for the swap.
func (a *App) downloadAndStage(ctx context.Context, jobID string, asset releaseAsset, checksums releaseChecksums, version string) (stagedUpdate, error) {
	// The version names the staging directory and is written into the helper
	// script; it must be the plain x.y.z shape, not whatever a tag contained.
	if !validReleaseVersion(version) {
		return stagedUpdate{}, fmt.Errorf("release %q does not contain a valid version", version)
	}

	dir, err := updatesDir()
	if err != nil {
		return stagedUpdate{}, err
	}
	// Clear out anything an earlier update left behind before writing tens of
	// megabytes next to it.
	purgeUpdatesDir(dir)

	archive := filepath.Join(dir, sanitizeAssetName(asset.Name))
	a.updateJobMessage(jobID, fmt.Sprintf("Downloading VPSBox %s", version))

	downloadErr := downloadWithProgress(ctx, asset.URL, archive, asset.Size, func(downloaded, total int64) {
		a.mu.Lock()
		a.download.Downloaded = downloaded
		a.download.Total = total
		a.download.Percent = percentOf(downloaded, total)
		percent := a.download.Percent
		a.mu.Unlock()
		a.updateJobMessage(jobID, fmt.Sprintf("Downloading VPSBox %s — %s (%d%%)",
			version, formatTransfer(downloaded, total), percent))
	})
	if downloadErr != nil {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return stagedUpdate{}, fmt.Errorf("the download timed out after %s — check your connection and try again", updateDownloadTimeout)
		case ctx.Err() != nil:
			return stagedUpdate{}, errDownloadCancelled
		}
		return stagedUpdate{}, downloadErr
	}

	a.mu.Lock()
	a.download.State = "preparing"
	a.mu.Unlock()
	a.updateJobMessage(jobID, "Verifying the download")

	if err := verifyDownloadedAsset(ctx, updatePublicKey, checksums, version, asset.Name, archive); err != nil {
		os.Remove(archive)
		return stagedUpdate{}, err
	}
	a.appendJobLog(jobID, "Release signature and checksum verified")

	staged, err := stageUpdate(archive, version, func(message string) {
		a.appendJobLog(jobID, message)
	})
	if err != nil {
		return stagedUpdate{}, err
	}
	return staged, nil
}

func (a *App) setDownloadError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.download.State = "error"
	a.download.Error = err.Error()
	a.cancelDownload = nil
	a.staged = nil
}

// resetDownload clears everything back to "nothing downloaded yet".
func (a *App) resetDownload() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.download = UpdateDownload{}
	a.cancelDownload = nil
	a.staged = nil
}

// updateDownloadState returns a copy for GetState, normalising the zero value
// so the frontend only ever sees the four documented states.
func (a *App) updateDownloadState() UpdateDownload {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.download
	if state.State == "" {
		state.State = "idle"
	}
	return state
}

// downloadWithProgress streams url into dest, reporting progress as it goes.
// The file is written to a .part sibling and renamed on success so a partial
// transfer is never mistaken for a complete download.
func downloadWithProgress(ctx context.Context, url, dest string, expected int64, onProgress func(downloaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vpsbox-desktop")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not download the update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	total := expected
	if resp.ContentLength > 0 {
		total = resp.ContentLength
	}

	partial := dest + ".part"
	file, err := os.Create(partial)
	if err != nil {
		return err
	}

	counter := &progressWriter{total: total, onProgress: onProgress}
	_, copyErr := io.Copy(io.MultiWriter(file, counter), resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		os.Remove(partial)
		return fmt.Errorf("could not download the update: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(partial)
		return closeErr
	}

	if expected > 0 && counter.written != expected {
		os.Remove(partial)
		return fmt.Errorf("the download was incomplete (%d of %d bytes)", counter.written, expected)
	}

	// Report the final size even if the last chunk fell inside the throttle.
	if onProgress != nil {
		onProgress(counter.written, total)
	}

	os.Remove(dest)
	return os.Rename(partial, dest)
}

// progressWriter counts bytes and reports them at most every progressInterval,
// so a fast download doesn't spam the job log with hundreds of lines.
type progressWriter struct {
	written    int64
	total      int64
	lastReport time.Time
	onProgress func(downloaded, total int64)
}

const progressInterval = 400 * time.Millisecond

func (w *progressWriter) Write(p []byte) (int, error) {
	w.written += int64(len(p))
	if w.onProgress != nil && time.Since(w.lastReport) >= progressInterval {
		w.lastReport = time.Now()
		w.onProgress(w.written, w.total)
	}
	return len(p), nil
}

// updatesDir is where downloads and staged payloads live.
func updatesDir() (string, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(paths.UpdatesDir, 0o755); err != nil {
		return "", err
	}
	return paths.UpdatesDir, nil
}

// updateLogPath is where the swap helper writes its output. The helper runs
// after the app has exited, so this file is the only trace of a failed swap.
func updateLogPath() (string, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return "", err
	}
	return paths.UpdateLogPath(), nil
}

// updateStatusPath is where the swap helper records how the install went. The
// helper runs after the app has quit, so without this a failed swap would only
// ever be visible in a log file nobody opens.
func updateStatusPath() (string, error) {
	dir, err := updatesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "apply-status"), nil
}

// reportPreviousUpdate turns the status the helper left behind into a job, so
// the first thing the relaunched app can say is whether the update landed. The
// file is consumed either way: it describes one attempt, not a standing state.
func (a *App) reportPreviousUpdate() {
	path, err := updateStatusPath()
	if err != nil {
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	os.Remove(path)

	outcome, message, _ := strings.Cut(strings.TrimSpace(string(body)), " ")
	message = strings.TrimSpace(message)
	if outcome != "ok" && outcome != "failed" {
		return
	}

	state, text := "done", fmt.Sprintf("Updated to VPSBox %s", message)
	if outcome == "failed" {
		if message == "" {
			message = "The update could not be installed."
		}
		state, text = "error", message
	}

	a.setJob(&Job{
		ID:         "app-update-result",
		Kind:       "app-update",
		Target:     message,
		State:      state,
		Message:    text,
		StartedAt:  nowString(),
		FinishedAt: nowString(),
		Log:        []string{text},
	})
}

// purgeUpdatesDir empties the updates directory, ignoring failures — a file we
// can't remove is wasted disk, not a reason to block the update. The status
// file is kept: it belongs to the previous attempt and is consumed at startup.
func purgeUpdatesDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.Name() == "apply-status" {
			continue
		}
		os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}

// sanitizeAssetName keeps a release asset name from escaping the updates
// directory. GitHub names are tame, but this is a remote-supplied filename.
func sanitizeAssetName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "vpsbox-update.download"
	}
	return name
}

func percentOf(done, total int64) int {
	if total <= 0 {
		return 0
	}
	percent := int(done * 100 / total)
	if percent > 100 {
		return 100
	}
	if percent < 0 {
		return 0
	}
	return percent
}

// formatTransfer renders "12.3 MB of 48.0 MB", or just the amount downloaded
// when the server didn't tell us how big the file is.
func formatTransfer(done, total int64) string {
	if total <= 0 {
		return formatBytes(done)
	}
	return fmt.Sprintf("%s of %s", formatBytes(done), formatBytes(total))
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit && exp < 3; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// writeHelperScript writes an executable helper next to the staged payload.
func writeHelperScript(dir, name, body string) (string, error) {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// dirIsWritable reports whether this process can create files in dir. It's the
// cheapest reliable answer across platforms: permission bits alone don't
// account for ACLs, read-only mounts, or macOS's sealed system volume.
func dirIsWritable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".vpsbox-update-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}

// currentExecutable resolves the running binary, following symlinks so the
// swap targets the real file rather than a link to it.
func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}
