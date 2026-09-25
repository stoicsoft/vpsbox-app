package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stoicsoft/vpsbox/internal/migrate"
	"github.com/stoicsoft/vpsbox/internal/registry"
)

// ErrMigrationCancelled is returned when a Confirm callback declines the plan.
// Nothing has been touched on either machine at that point.
var ErrMigrationCancelled = errors.New("migration cancelled")

// PushOptions describes a sandbox → real VPS migration.
type PushOptions struct {
	Name   string         // sandbox to push
	Target migrate.Target // the real VPS
	DryRun bool           // stop after computing the plan
	// Confirm, when set, is shown the plan before anything is applied and can
	// abort the push. This is the "are you sure" hook — the destination is a
	// machine somebody cares about.
	Confirm func(*migrate.Plan) bool
	Status  func(string)
	Output  func(string)
}

// ImportVPSOptions describes a real VPS → fresh sandbox migration.
type ImportVPSOptions struct {
	Target     migrate.Target
	Name       string // sandbox name; empty allocates vps-1, vps-2, …
	CPUs       int    // 0 = size from the VPS
	MemoryGB   int
	DiskGB     int
	SelfSigned bool
	Confirm    func(*migrate.Plan) bool
	Status     func(string)
	Output     func(string)
}

// sshEndpoint is one side of a migration: a label for messages and the SSH
// argument list that reaches the machine. Sandboxes and real VPSes both
// reduce to this, which is what lets one engine serve push and import.
type sshEndpoint struct {
	label string
	args  []string
}

func (m *Manager) endpointForInstance(instance *registry.Instance) (sshEndpoint, error) {
	args, err := m.sshArgs(instance)
	if err != nil {
		return sshEndpoint{}, err
	}
	return sshEndpoint{label: instance.Name, args: args}, nil
}

// endpointForTarget builds SSH arguments for a machine vpsbox does not manage.
// accept-new rather than no: an unknown VPS host key is recorded on first
// contact, but a changed one is still refused.
func (m *Manager) endpointForTarget(t migrate.Target) sshEndpoint {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + m.paths.KnownHosts,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
	}
	if t.Port != 0 && t.Port != 22 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	if t.KeyPath != "" {
		args = append(args, "-i", t.KeyPath)
	}
	args = append(args, t.User+"@"+t.Host)
	return sshEndpoint{label: t.Label(), args: args}
}

// captureManifest runs the capture script on one machine and parses the result.
func captureManifest(ctx context.Context, ep sshEndpoint) (*migrate.Manifest, error) {
	stdout, stderr, err := runSSH(ctx, ep.args, migrate.CaptureScript())
	if err != nil {
		detail := strings.TrimSpace(stderr)
		if detail != "" {
			return nil, fmt.Errorf("could not inspect %s: %w\n%s", ep.label, err, detail)
		}
		return nil, fmt.Errorf("could not inspect %s: %w", ep.label, err)
	}
	manifest, err := migrate.ParseManifest(stdout)
	if err != nil {
		return nil, fmt.Errorf("could not inspect %s: %w", ep.label, err)
	}
	return manifest, nil
}

// Push replicates a sandbox's work onto a real VPS: missing packages, app and
// config files, docker volume data, enabled services, and running compose
// projects. See internal/migrate for what deliberately does not move.
func (m *Manager) Push(ctx context.Context, opts PushOptions) (*migrate.Plan, error) {
	report := func(message string) {
		if opts.Status != nil {
			opts.Status(message)
		}
	}

	instance, err := m.requireRunningInstance(ctx, opts.Name)
	if err != nil {
		return nil, err
	}
	src, err := m.endpointForInstance(instance)
	if err != nil {
		return nil, err
	}
	dst := m.endpointForTarget(opts.Target)

	report(fmt.Sprintf("Inspecting %s…", dst.label))
	dstManifest, err := captureManifest(ctx, dst)
	if err != nil {
		return nil, err
	}
	if err := dstManifest.Supported(); err != nil {
		return nil, fmt.Errorf("%s: %w", dst.label, err)
	}

	report(fmt.Sprintf("Inspecting sandbox %s…", instance.Name))
	srcManifest, err := captureManifest(ctx, src)
	if err != nil {
		return nil, err
	}

	plan := migrate.BuildPlan(srcManifest, dstManifest, instance.Name, dst.label)
	if opts.DryRun {
		return plan, nil
	}
	if opts.Confirm != nil && !opts.Confirm(plan) {
		return plan, ErrMigrationCancelled
	}

	if err := m.applyMigration(ctx, src, dst, plan, opts.Status, opts.Output); err != nil {
		return plan, err
	}
	report(fmt.Sprintf("Push complete — %s now carries what %s had", dst.label, instance.Name))
	return plan, nil
}

// ImportVPS clones a real VPS into a fresh local sandbox: it inspects the VPS,
// creates a sandbox sized to hold what runs there, and migrates the work in.
func (m *Manager) ImportVPS(ctx context.Context, opts ImportVPSOptions) (*registry.Instance, *migrate.Plan, error) {
	report := func(message string) {
		if opts.Status != nil {
			opts.Status(message)
		}
	}

	src := m.endpointForTarget(opts.Target)
	report(fmt.Sprintf("Inspecting %s…", src.label))
	srcManifest, err := captureManifest(ctx, src)
	if err != nil {
		return nil, nil, err
	}
	if err := srcManifest.Supported(); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", src.label, err)
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name, err = m.nextImportName()
		if err != nil {
			return nil, nil, err
		}
	}

	var estimatedKB int64
	for _, path := range srcManifest.Paths {
		estimatedKB += path.SizeKB
	}
	for _, volume := range srcManifest.Volumes {
		estimatedKB += volume.SizeKB
	}
	cpus, memoryGB, diskGB := migrate.SandboxSize(srcManifest.Resources, estimatedKB)
	if opts.CPUs > 0 {
		cpus = opts.CPUs
	}
	if opts.MemoryGB > 0 {
		memoryGB = opts.MemoryGB
	}
	if opts.DiskGB > 0 {
		diskGB = opts.DiskGB
	}

	report(fmt.Sprintf("Creating sandbox %s (%d CPU, %d GB memory, %d GB disk)…", name, cpus, memoryGB, diskGB))
	instance, err := m.Up(ctx, UpOptions{
		Name:       name,
		CPUs:       cpus,
		MemoryGB:   memoryGB,
		DiskGB:     diskGB,
		Image:      migrate.ImageForOS(srcManifest.OS),
		SelfSigned: opts.SelfSigned,
		Progress:   opts.Status,
	})
	if err != nil {
		return nil, nil, err
	}

	dst, err := m.endpointForInstance(instance)
	if err != nil {
		return instance, nil, err
	}
	report("Inspecting the new sandbox…")
	dstManifest, err := captureManifest(ctx, dst)
	if err != nil {
		return instance, nil, err
	}

	plan := migrate.BuildPlan(srcManifest, dstManifest, src.label, instance.Name)
	if opts.Confirm != nil && !opts.Confirm(plan) {
		return instance, plan, ErrMigrationCancelled
	}

	if err := m.applyMigration(ctx, src, dst, plan, opts.Status, opts.Output); err != nil {
		return instance, plan, err
	}

	// Record where the sandbox came from, so lists and exports can say it.
	if !containsLabel(instance.Labels, "vps-import") {
		instance.Labels = append(instance.Labels, "vps-import", "from:"+opts.Target.Host)
		if err := m.store.UpsertInstance(*instance); err != nil {
			return instance, plan, err
		}
	}
	report(fmt.Sprintf("Import complete — %s is a local copy of %s", instance.Name, src.label))
	return instance, plan, nil
}

// InspectVPS captures a remote machine's manifest without changing anything on
// it — the desktop's import preview, so "Import" can say what it will do
// before any VM exists.
func (m *Manager) InspectVPS(ctx context.Context, target migrate.Target) (*migrate.Manifest, error) {
	ep := m.endpointForTarget(target)
	manifest, err := captureManifest(ctx, ep)
	if err != nil {
		return nil, err
	}
	if err := manifest.Supported(); err != nil {
		return nil, fmt.Errorf("%s: %w", ep.label, err)
	}
	return manifest, nil
}

// SuggestImportName exposes the vps-N allocator so callers that need the name
// before starting the work (the desktop resolves it up front, the way
// StartCreateSandbox does) agree with what ImportVPS would pick.
func (m *Manager) SuggestImportName() (string, error) {
	return m.nextImportName()
}

// nextImportName allocates vps-1, vps-2, … the way nextName allocates dev-N,
// so imported clones read differently from scratch sandboxes in every list.
func (m *Manager) nextImportName() (string, error) {
	instances, err := m.store.LoadInstances()
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, inst := range instances {
		seen[inst.Name] = true
	}
	for next := 1; ; next++ {
		candidate := fmt.Sprintf("vps-%d", next)
		if !seen[candidate] {
			return candidate, nil
		}
	}
}

// applyMigration is the shared engine: prepare the destination, stream the
// files, restore volume data, enable services, restart compose projects.
// The host machine relays every byte (ssh src tar -c | ssh dst tar -x), so
// the two machines never need to reach each other.
func (m *Manager) applyMigration(ctx context.Context, src, dst sshEndpoint, plan *migrate.Plan, status, output func(string)) error {
	report := func(message string) {
		if status != nil {
			status(message)
		}
	}
	say := func(message string) {
		if output != nil {
			output(message)
		}
	}

	var warnings []string
	markerLine := func(line string) {
		switch {
		case strings.HasPrefix(line, migrate.MarkerPackageFailed):
			warnings = append(warnings, "package"+strings.TrimPrefix(line, migrate.MarkerPackageFailed)+" could not be installed")
		case strings.HasPrefix(line, migrate.MarkerServiceFailed):
			warnings = append(warnings, "service"+strings.TrimPrefix(line, migrate.MarkerServiceFailed)+" failed")
		case strings.HasPrefix(line, migrate.MarkerServiceSkip):
			warnings = append(warnings, "service"+strings.TrimPrefix(line, migrate.MarkerServiceSkip)+" has no unit file on the destination — skipped")
		case strings.HasPrefix(line, migrate.MarkerComposeFailed):
			warnings = append(warnings, "compose project"+strings.TrimPrefix(line, migrate.MarkerComposeFailed)+" failed to start")
		case strings.HasPrefix(line, migrate.MarkerComposeSkip):
			warnings = append(warnings, "compose project"+strings.TrimPrefix(line, migrate.MarkerComposeSkip)+" config missing after copy — not started")
		default:
			say(line)
			return
		}
		say(warnings[len(warnings)-1])
	}

	// The apt repository configuration has to reach the destination before the
	// package install: docker-ce and friends live in third-party repositories
	// the destination has never heard of.
	aptPaths, dataPaths := migrate.SplitPlanPaths(plan.Paths)
	if len(aptPaths) > 0 && len(plan.Packages) > 0 {
		report(fmt.Sprintf("Copying package repository configuration to %s…", dst.label))
		if err := m.pipeBetween(ctx, src, migrate.TarCreateCommand(relPaths(aptPaths)), dst, migrate.TarExtractCommand(), nil); err != nil {
			return fmt.Errorf("copy apt sources: %w", err)
		}
	} else {
		// No package install planned — the apt paths can travel with the rest.
		dataPaths = plan.Paths
	}

	if len(plan.Users) > 0 || len(plan.Packages) > 0 {
		report(fmt.Sprintf("Preparing %s: %d package(s), %d user(s)…", dst.label, len(plan.Packages), len(plan.Users)))
		tail, err := streamSSH(ctx, dst.args, migrate.PrepScript(plan.Users, plan.Packages), markerLine)
		if err != nil {
			return fmt.Errorf("prepare %s: %w\n%s", dst.label, err, tail)
		}
	}

	if len(dataPaths) > 0 {
		report(fmt.Sprintf("Copying files to %s (~%s)…", dst.label, humanKB(plan.EstimatedKB())))
		err := m.pipeBetween(ctx, src, migrate.TarCreateCommand(relPaths(dataPaths)), dst, migrate.TarExtractCommand(), func(copied int64) {
			report(fmt.Sprintf("Copying files to %s… %s so far", dst.label, humanBytes(copied)))
		})
		if err != nil {
			return fmt.Errorf("copy files: %w", err)
		}
	}

	if len(plan.Volumes) > 0 {
		// Restoring data underneath a running container corrupts both; compose
		// projects come back below, anything else stays stopped (the plan said so).
		report(fmt.Sprintf("Stopping containers on %s before restoring volumes…", dst.label))
		if _, _, err := runSSH(ctx, dst.args, migrate.StopContainersScript()); err != nil {
			return fmt.Errorf("stop containers on %s: %w", dst.label, err)
		}
		for _, volume := range plan.Volumes {
			report(fmt.Sprintf("Copying docker volume %s (~%s)…", volume.Name, humanKB(volume.SizeKB)))
			err := m.pipeBetween(ctx, src, migrate.VolumeTarCreate(volume.Name), dst, migrate.VolumeTarExtract(volume.Name), func(copied int64) {
				report(fmt.Sprintf("Copying docker volume %s… %s so far", volume.Name, humanBytes(copied)))
			})
			if err != nil {
				return fmt.Errorf("copy volume %s: %w", volume.Name, err)
			}
		}
	}

	if len(plan.Services) > 0 {
		report(fmt.Sprintf("Enabling %d service(s) on %s…", len(plan.Services), dst.label))
		tail, err := streamSSH(ctx, dst.args, migrate.ServiceScript(plan.Services), markerLine)
		if err != nil {
			return fmt.Errorf("enable services on %s: %w\n%s", dst.label, err, tail)
		}
	}

	if len(plan.Compose) > 0 {
		report(fmt.Sprintf("Starting compose projects on %s…", dst.label))
		tail, err := streamSSH(ctx, dst.args, migrate.ComposeUpScript(plan.Compose), markerLine)
		if err != nil {
			return fmt.Errorf("start compose projects on %s: %w\n%s", dst.label, err, tail)
		}
	}

	if len(warnings) > 0 {
		say(fmt.Sprintf("Finished with %d warning(s) — see the lines above", len(warnings)))
	}
	return nil
}

func relPaths(paths []migrate.PathInfo) []string {
	rels := make([]string, 0, len(paths))
	for _, path := range paths {
		rels = append(rels, path.Rel)
	}
	return rels
}

// pipeBetween streams `ssh src srcCmd | ssh dst dstCmd` with this machine in
// the middle. Progress reports the bytes relayed so far.
func (m *Manager) pipeBetween(ctx context.Context, src sshEndpoint, srcCmd string, dst sshEndpoint, dstCmd string, progress func(int64)) error {
	srcExec := exec.CommandContext(ctx, "ssh", append(append([]string{}, src.args...), srcCmd)...)
	dstExec := exec.CommandContext(ctx, "ssh", append(append([]string{}, dst.args...), dstCmd)...)

	pr, pw := io.Pipe()
	srcExec.Stdout = pw
	dstExec.Stdin = &progressReader{r: pr, report: progress, lastTime: time.Now()}

	var srcStderr, dstStderr boundedBuffer
	srcExec.Stderr = &srcStderr
	dstExec.Stderr = &dstStderr

	if err := dstExec.Start(); err != nil {
		return fmt.Errorf("start writer to %s: %w", dst.label, err)
	}
	if err := srcExec.Start(); err != nil {
		pr.Close()
		_ = dstExec.Wait()
		return fmt.Errorf("start reader from %s: %w", src.label, err)
	}

	// If the destination dies mid-stream, the source's writes into the pipe
	// would block forever — closing the read side unblocks them with an error.
	dstDone := make(chan error, 1)
	go func() {
		err := dstExec.Wait()
		if err != nil {
			pr.CloseWithError(fmt.Errorf("%s closed the stream", dst.label))
		} else {
			pr.Close()
		}
		dstDone <- err
	}()

	srcErr := srcExec.Wait()
	pw.CloseWithError(srcErr) // nil closes cleanly: the destination sees EOF and finishes
	dstErr := <-dstDone

	if srcErr != nil {
		return fmt.Errorf("read from %s: %w\n%s", src.label, srcErr, srcStderr.String())
	}
	if dstErr != nil {
		return fmt.Errorf("write to %s: %w\n%s", dst.label, dstErr, dstStderr.String())
	}
	return nil
}

// progressReader counts bytes flowing to the destination, reporting at most
// every few seconds so long copies show life without flooding the log.
type progressReader struct {
	r        io.Reader
	total    int64
	lastTime time.Time
	report   func(int64)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	p.total += int64(n)
	if p.report != nil && time.Since(p.lastTime) >= 3*time.Second {
		p.lastTime = time.Now()
		p.report(p.total)
	}
	return n, err
}

// boundedBuffer keeps the tail of a stream — enough stderr to diagnose a
// failed transfer without holding an installer's full output in memory.
type boundedBuffer struct {
	mu   sync.Mutex
	data []byte
}

const boundedBufferLimit = 8 << 10

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > boundedBufferLimit {
		b.data = b.data[len(b.data)-boundedBufferLimit:]
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func humanKB(kb int64) string {
	return humanBytes(kb << 10)
}
