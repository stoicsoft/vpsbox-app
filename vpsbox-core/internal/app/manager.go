package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stoicsoft/vpsbox/internal/backend"
	"github.com/stoicsoft/vpsbox/internal/config"
	"github.com/stoicsoft/vpsbox/internal/doctor"
	"github.com/stoicsoft/vpsbox/internal/domain"
	"github.com/stoicsoft/vpsbox/internal/executil"
	"github.com/stoicsoft/vpsbox/internal/prep"
	"github.com/stoicsoft/vpsbox/internal/registry"
	"github.com/stoicsoft/vpsbox/internal/share"
	vpstls "github.com/stoicsoft/vpsbox/internal/tls"
)

var Version = "0.1.0-dev"

type UpOptions struct {
	Name       string
	CPUs       int
	MemoryGB   int
	DiskGB     int
	Image      string
	User       string
	SelfSigned bool
	Progress   func(string)
}

type UpdateSandboxOptions struct {
	Name     string
	CPUs     int
	MemoryGB int
	DiskGB   int
	Progress func(string)
}

type Manager struct {
	paths    config.Paths
	store    *registry.Store
	tls      *vpstls.Manager
	shares   *share.Manager
	backend  backend.Backend
	attempts []string
}

func NewManager(ctx context.Context) (*Manager, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	if err := paths.Ensure(); err != nil {
		return nil, err
	}

	store := registry.NewStore(paths)
	shares := share.NewManager(paths, store)
	tlsManager := vpstls.NewManager()

	selected, attempted, err := backend.Detect(ctx)
	if err != nil {
		return nil, err
	}

	return &Manager{
		paths:    paths,
		store:    store,
		tls:      tlsManager,
		shares:   shares,
		backend:  selected,
		attempts: attempted,
	}, nil
}

func (m *Manager) Paths() config.Paths {
	return m.paths
}

func (m *Manager) SuggestName() (string, error) {
	return m.nextName()
}

func (m *Manager) BackendName() string {
	if m.backend == nil {
		return "unknown"
	}
	return m.backend.Name()
}

func (m *Manager) Up(ctx context.Context, opts UpOptions) (*registry.Instance, error) {
	report := func(message string) {
		if opts.Progress != nil {
			opts.Progress(message)
		}
	}

	if opts.Name == "" {
		name, err := m.nextName()
		if err != nil {
			return nil, err
		}
		opts.Name = name
	}
	if opts.CPUs == 0 {
		opts.CPUs = 2
	}
	if opts.MemoryGB == 0 {
		opts.MemoryGB = 2
	}
	if opts.DiskGB == 0 {
		opts.DiskGB = 10
	}
	if opts.Image == "" {
		opts.Image = "24.04"
	}
	if opts.User == "" {
		opts.User = "root"
	}

	report("Checking VM backend")
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}

	if existing, err := m.store.GetInstance(opts.Name); err == nil {
		report("Starting existing sandbox")
		if err := m.backend.Start(ctx, opts.Name); err != nil && !strings.Contains(err.Error(), "already running") {
			return nil, err
		}
		return m.waitAndRefreshInstance(ctx, *existing, false, report)
	}

	if imported, err := m.importBackendInstance(ctx, opts.Name); err == nil && imported != nil {
		report("Importing existing sandbox")
		if err := m.backend.Start(ctx, opts.Name); err != nil && !strings.Contains(err.Error(), "already running") {
			return nil, err
		}
		return m.waitAndRefreshInstance(ctx, *imported, false, report)
	}

	report("Generating SSH key")
	pubKey, err := m.ensureSSHKey(ctx, opts.Name)
	if err != nil {
		return nil, err
	}

	hostname := fmt.Sprintf("%s.vpsbox.local", opts.Name)
	report("Preparing Ubuntu bootstrap script")
	if err := prep.WriteCloudInit(m.paths.CloudInitPath(opts.Name), prep.CloudInitData{
		InstanceName: opts.Name,
		Hostname:     hostname,
		User:         opts.User,
		PublicKey:    pubKey,
	}); err != nil {
		return nil, err
	}

	report("Launching Ubuntu VM")
	if err := m.backend.Create(ctx, backend.CreateRequest{
		Name:          opts.Name,
		Image:         opts.Image,
		CPUs:          opts.CPUs,
		MemoryGB:      opts.MemoryGB,
		DiskGB:        opts.DiskGB,
		CloudInitPath: m.paths.CloudInitPath(opts.Name),
	}); err != nil {
		return nil, err
	}

	instance := registry.Instance{
		Name:           opts.Name,
		Status:         string(backend.StatusRunning),
		Port:           22,
		Username:       opts.User,
		PrivateKeyPath: m.paths.KeyPath(opts.Name),
		PublicKeyPath:  m.paths.PublicKeyPath(opts.Name),
		Image:          opts.Image,
		Labels:         []string{"sandbox", "vpsbox"},
		Backend:        m.backend.Name(),
		CPUs:           opts.CPUs,
		MemoryGB:       opts.MemoryGB,
		DiskGB:         opts.DiskGB,
		Hostname:       hostname,
		SandboxMarker:  "/etc/vpsbox-info",
		CloudInitPath:  m.paths.CloudInitPath(opts.Name),
	}

	return m.waitAndRefreshInstance(ctx, instance, opts.SelfSigned, report)
}

func (m *Manager) Down(ctx context.Context, name string) (*registry.Instance, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}
	if err := m.backend.Stop(ctx, instance.Name); err != nil {
		return nil, err
	}
	instance.Status = string(backend.StatusStopped)
	if err := m.store.UpsertInstance(*instance); err != nil {
		return nil, err
	}
	if err := m.syncHosts(); err != nil {
		return nil, err
	}
	return instance, nil
}

func (m *Manager) Destroy(ctx context.Context, name string, force bool) error {
	instance, err := m.requireInstance(name)
	if err != nil {
		return err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return err
	}
	if !force {
		snapshots, err := m.backend.ListSnapshots(ctx, instance.Name)
		if err == nil && len(snapshots) > 0 {
			return fmt.Errorf("instance %s has snapshots; rerun with --force", instance.Name)
		}
	}
	if err := m.backend.Delete(ctx, instance.Name); err != nil {
		return err
	}
	if err := m.store.DeleteInstance(instance.Name); err != nil {
		return err
	}
	return m.syncHosts()
}

func (m *Manager) List(ctx context.Context) ([]registry.Instance, error) {
	instances, err := m.store.LoadInstances()
	if err != nil {
		return nil, err
	}
	if ok, err := m.backend.Available(ctx); err == nil && ok {
		backendInstances, err := m.backend.List(ctx)
		if err == nil {
			byName := map[string]backend.InstanceInfo{}
			for _, inst := range backendInstances {
				byName[inst.Name] = inst
			}
			filtered := make([]registry.Instance, 0, len(instances))
			for _, instance := range instances {
				if containsLabel(instance.Labels, "imported") && byName[instance.Name].Name == "" {
					continue
				}
				filtered = append(filtered, instance)
			}
			instances = filtered
			for _, inst := range backendInstances {
				if !containsInstance(instances, inst.Name) {
					imported := m.makeImportedInstance(inst)
					instances = append(instances, imported)
				}
			}
			for i := range instances {
				if latest, ok := byName[instances[i].Name]; ok {
					applyBackendInfo(&instances[i], latest)
				}
			}
			_ = m.store.SaveInstances(instances)
		}
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return instances, nil
}

func (m *Manager) Info(ctx context.Context, name string) (*registry.Instance, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	return m.waitAndRefreshInstance(ctx, *instance, false, nil)
}

func (m *Manager) Snapshot(ctx context.Context, name, snapshotName, comment string) error {
	instance, err := m.requireInstance(name)
	if err != nil {
		return err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return err
	}

	wasRunning := strings.EqualFold(instance.Status, string(backend.StatusRunning))
	if wasRunning {
		if err := m.backend.Stop(ctx, instance.Name); err != nil {
			return err
		}
	}
	if err := m.backend.Snapshot(ctx, instance.Name, snapshotName, comment); err != nil {
		return err
	}
	if wasRunning {
		if err := m.backend.Start(ctx, instance.Name); err != nil {
			return err
		}
	}

	instance.SnapshotsEnabled = true
	return m.store.UpsertInstance(*instance)
}

func (m *Manager) Reset(ctx context.Context, name, snapshotName string) (*registry.Instance, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}

	if snapshotName == "" {
		snapshots, err := m.backend.ListSnapshots(ctx, instance.Name)
		if err != nil {
			return nil, err
		}
		if len(snapshots) == 0 {
			return nil, errors.New("no snapshots available")
		}
		snapshotName = snapshots[len(snapshots)-1].Name
	}

	_ = m.backend.Stop(ctx, instance.Name)
	if err := m.backend.Restore(ctx, instance.Name, snapshotName); err != nil {
		return nil, err
	}
	if err := m.backend.Start(ctx, instance.Name); err != nil {
		return nil, err
	}
	return m.waitAndRefreshInstance(ctx, *instance, false, nil)
}

func (m *Manager) ListSnapshots(ctx context.Context, name string) ([]backend.SnapshotInfo, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}
	return m.backend.ListSnapshots(ctx, instance.Name)
}

func (m *Manager) Export(ctx context.Context, name, format string) (string, error) {
	instance, err := m.Info(ctx, name)
	if err != nil {
		return "", err
	}

	switch format {
	case "", "json", "sc":
		out, err := json.MarshalIndent(map[string]any{
			"name":             instance.Name + " (vpsbox)",
			"host":             instance.Host,
			"hostname":         instance.Hostname,
			"port":             instance.Port,
			"username":         instance.Username,
			"auth_type":        "key",
			"private_key_path": instance.PrivateKeyPath,
			"labels":           instance.Labels,
		}, "", "  ")
		if err != nil {
			return "", err
		}
		return string(out), nil
	case "env":
		lines := []string{
			"export VPSBOX_NAME=" + instance.Name,
			"export VPSBOX_HOST=" + instance.Host,
			"export VPSBOX_HOSTNAME=" + instance.Hostname,
			"export VPSBOX_PORT=22",
			"export VPSBOX_USER=" + instance.Username,
			"export VPSBOX_PRIVATE_KEY=" + instance.PrivateKeyPath,
		}
		return strings.Join(lines, "\n"), nil
	default:
		return "", fmt.Errorf("unknown export format %q", format)
	}
}

func (m *Manager) SSH(ctx context.Context, name string, remoteCommand []string) error {
	instance, err := m.Info(ctx, name)
	if err != nil {
		return err
	}

	host := instance.Host
	if host == "" {
		host = instance.Hostname
	}
	if host == "" {
		return errors.New("instance has no host address yet")
	}
	if strings.TrimSpace(instance.PrivateKeyPath) == "" {
		return errors.New("no SSH key is configured for this sandbox; generate one first")
	}

	args := []string{
		"-i", instance.PrivateKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=" + m.paths.KnownHosts,
		fmt.Sprintf("%s@%s", instance.Username, host),
	}
	if len(remoteCommand) > 0 {
		args = append(args, remoteCommand...)
	}

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) Doctor(ctx context.Context) []doctor.Check {
	return doctor.Run(ctx, m.paths)
}

func (m *Manager) CreateShare(ctx context.Context, target string, ttl time.Duration, name string) (*registry.Share, error) {
	return m.shares.Create(ctx, target, ttl, name)
}

func (m *Manager) Shares() ([]registry.Share, error) {
	return m.shares.List()
}

func (m *Manager) SyncLocalDomains() error {
	return m.syncHosts()
}

func (m *Manager) UpdateSandbox(ctx context.Context, opts UpdateSandboxOptions) (*registry.Instance, error) {
	instance, err := m.requireInstance(opts.Name)
	if err != nil {
		return nil, err
	}
	report := func(message string) {
		if opts.Progress != nil {
			opts.Progress(message)
		}
	}
	report("Checking backend")
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}

	wasRunning := strings.EqualFold(instance.Status, string(backend.StatusRunning))
	if wasRunning {
		report("Stopping sandbox for resource update")
		if err := m.backend.Stop(ctx, instance.Name); err != nil {
			return nil, err
		}
	}

	report("Applying CPU, memory, and disk changes")
	if err := m.backend.UpdateResources(ctx, backend.UpdateResourcesRequest{
		Name:     instance.Name,
		CPUs:     opts.CPUs,
		MemoryGB: opts.MemoryGB,
		DiskGB:   opts.DiskGB,
	}); err != nil {
		return nil, err
	}

	instance.CPUs = opts.CPUs
	instance.MemoryGB = opts.MemoryGB
	instance.DiskGB = opts.DiskGB

	if wasRunning {
		report("Restarting sandbox")
		if err := m.backend.Start(ctx, instance.Name); err != nil {
			return nil, err
		}
	}

	report("Refreshing sandbox details")
	return m.waitAndRefreshInstance(ctx, *instance, false, report)
}

func (m *Manager) GenerateSSHKey(ctx context.Context, name string, progress func(string)) (*registry.Instance, error) {
	instance, err := m.requireInstance(name)
	if err != nil {
		return nil, err
	}
	report := func(message string) {
		if progress != nil {
			progress(message)
		}
	}
	report("Checking backend")
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}
	report("Generating local SSH key")
	pubKey, err := m.ensureSSHKey(ctx, instance.Name)
	if err != nil {
		return nil, err
	}
	report("Installing public key into sandbox")
	if err := m.backend.InstallSSHKey(ctx, backend.InstallSSHKeyRequest{
		Name:      instance.Name,
		Username:  instance.Username,
		PublicKey: pubKey,
	}); err != nil {
		return nil, err
	}
	instance.PrivateKeyPath = m.paths.KeyPath(instance.Name)
	instance.PublicKeyPath = m.paths.PublicKeyPath(instance.Name)
	report("Refreshing sandbox details")
	return m.waitAndRefreshInstance(ctx, *instance, false, report)
}

func (m *Manager) Unshare(name string) error {
	return m.shares.Unshare(name)
}

func (m *Manager) Login(email, token, apiBase string) (*registry.AuthSession, error) {
	if email == "" {
		return nil, errors.New("email is required")
	}
	if apiBase == "" {
		apiBase = "https://api.vpsbox.dev"
	}
	session := registry.AuthSession{
		Email:     email,
		Token:     token,
		APIBase:   apiBase,
		CreatedAt: time.Now().UTC(),
	}
	if err := m.store.SaveAuth(session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (m *Manager) Logout() error {
	return m.store.DeleteAuth()
}

func (m *Manager) Upgrade(ctx context.Context) error {
	switch runtime.GOOS {
	case "darwin", "linux":
		if executil.LookPath("brew") {
			_, err := executil.Run(ctx, "brew", "upgrade", "vpsbox")
			return err
		}
	case "windows":
		if executil.LookPath("winget") {
			_, err := executil.Run(ctx, "winget", "upgrade", "--id", "vpsbox", "-e")
			return err
		}
	}

	return errors.New("automatic upgrade is not configured for this install method")
}

func (m *Manager) PrintTable(w *tabwriter.Writer, headers []string, rows [][]string) {
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

func (m *Manager) ensureBackend(ctx context.Context) error {
	ok, err := m.backend.Available(ctx)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return m.backend.EnsureInstalled(ctx)
}

func (m *Manager) ensureSSHKey(ctx context.Context, name string) (string, error) {
	keyPath := m.paths.KeyPath(name)
	if _, err := os.Stat(keyPath); errors.Is(err, os.ErrNotExist) {
		_, err := executil.Run(ctx, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", keyPath)
		if err != nil {
			return "", err
		}
	}
	pubKeyBytes, err := os.ReadFile(m.paths.PublicKeyPath(name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(pubKeyBytes)), nil
}

func (m *Manager) nextName() (string, error) {
	instances, err := m.store.LoadInstances()
	if err != nil {
		return "", err
	}
	next := 1
	seen := map[string]bool{}
	for _, inst := range instances {
		seen[inst.Name] = true
	}
	for {
		candidate := fmt.Sprintf("dev-%d", next)
		if !seen[candidate] {
			return candidate, nil
		}
		next++
	}
}

func (m *Manager) requireInstance(name string) (*registry.Instance, error) {
	if name == "" {
		instances, err := m.store.LoadInstances()
		if err != nil {
			return nil, err
		}
		if len(instances) == 1 {
			name = instances[0].Name
		}
	}
	if name == "" {
		return nil, errors.New("instance name is required")
	}
	instance, err := m.store.GetInstance(name)
	if err == nil {
		return instance, nil
	}
	return m.importBackendInstance(context.Background(), name)
}

func (m *Manager) refreshInstance(ctx context.Context, instance registry.Instance) (*registry.Instance, error) {
	return m.refreshInstanceWithTLSPreference(ctx, instance, false, nil)
}

func (m *Manager) waitAndRefreshInstance(ctx context.Context, instance registry.Instance, preferSelfSigned bool, progress func(string)) (*registry.Instance, error) {
	deadline := time.Now().Add(20 * time.Minute)
	var lastErr error
	reportedWaiting := false
	for time.Now().Before(deadline) {
		refreshed, err := m.refreshInstanceWithTLSPreference(ctx, instance, preferSelfSigned, progress)
		if err == nil && refreshed.Host != "" {
			return refreshed, nil
		}
		lastErr = err
		if progress != nil && !reportedWaiting {
			progress("Waiting for Ubuntu initialization")
			reportedWaiting = true
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("instance did not become ready before timeout")
}

func (m *Manager) refreshInstanceWithTLSPreference(ctx context.Context, instance registry.Instance, preferSelfSigned bool, progress func(string)) (*registry.Instance, error) {
	if err := m.ensureBackend(ctx); err != nil {
		return nil, err
	}
	info, err := m.backend.Info(ctx, instance.Name)
	if err != nil {
		return nil, err
	}
	applyBackendInfo(&instance, *info)

	ip := firstIPv4(info.IPv4)
	if ip != "" {
		if progress != nil {
			progress("Finalizing local networking and certificates")
		}
		names := domain.NamesForInstance(instance.Name)
		instance.Host = ip
		instance.Hostname = names.Hostname

		if err := m.syncHostsWith(instance.Name, domain.Record{Host: names.Hostname, IP: ip}); err != nil && !errors.Is(err, domain.ErrPrivilegesRequired) {
			return nil, err
		}

		cert, err := m.tls.EnsureCertificate(ctx, m.paths.CertPath(instance.Name), m.paths.CertKeyPath(instance.Name), []string{
			names.Hostname,
			"*." + names.Hostname,
		}, preferSelfSigned)
		if err != nil {
			return nil, err
		}
		instance.CertPath = cert.CertPath
		instance.CertKeyPath = cert.KeyPath
	}

	if err := m.store.UpsertInstance(instance); err != nil {
		return nil, err
	}
	if progress != nil {
		progress("Writing local registry")
	}
	return &instance, nil
}

func (m *Manager) syncHosts() error {
	instances, err := m.store.LoadInstances()
	if err != nil {
		return err
	}
	records := make([]domain.Record, 0, len(instances))
	for _, inst := range instances {
		if inst.Hostname != "" && inst.Host != "" && strings.EqualFold(inst.Status, string(backend.StatusRunning)) {
			records = append(records, domain.Record{Host: inst.Hostname, IP: inst.Host})
		}
	}
	return domain.SyncHosts(m.paths.HostsFilePath, records)
}

func (m *Manager) syncHostsWith(instanceName string, record domain.Record) error {
	instances, err := m.store.LoadInstances()
	if err != nil {
		return err
	}
	records := make([]domain.Record, 0, len(instances)+1)
	added := false
	for _, inst := range instances {
		if inst.Name == instanceName {
			records = append(records, record)
			added = true
			continue
		}
		if inst.Hostname != "" && inst.Host != "" && strings.EqualFold(inst.Status, string(backend.StatusRunning)) {
			records = append(records, domain.Record{Host: inst.Hostname, IP: inst.Host})
		}
	}
	if !added {
		records = append(records, record)
	}
	return domain.SyncHosts(m.paths.HostsFilePath, records)
}

func applyBackendInfo(instance *registry.Instance, info backend.InstanceInfo) {
	instance.Status = string(info.Status)
	instance.Backend = info.Backend
	if ip := firstIPv4(info.IPv4); ip != "" {
		instance.Host = ip
	}
}

func firstIPv4(addresses []string) string {
	for _, addr := range addresses {
		if ip := net.ParseIP(addr); ip != nil && ip.To4() != nil {
			return addr
		}
	}
	return ""
}

func (m *Manager) importBackendInstance(ctx context.Context, name string) (*registry.Instance, error) {
	info, err := m.backend.Info(ctx, name)
	if err != nil {
		return nil, err
	}
	instance := m.makeImportedInstance(*info)
	if err := m.store.UpsertInstance(instance); err != nil {
		return nil, err
	}
	copy := instance
	return &copy, nil
}

func (m *Manager) makeImportedInstance(info backend.InstanceInfo) registry.Instance {
	privateKeyPath := ""
	publicKeyPath := ""
	if _, err := os.Stat(m.paths.KeyPath(info.Name)); err == nil {
		privateKeyPath = m.paths.KeyPath(info.Name)
	}
	if _, err := os.Stat(m.paths.PublicKeyPath(info.Name)); err == nil {
		publicKeyPath = m.paths.PublicKeyPath(info.Name)
	}
	instance := registry.Instance{
		Name:             info.Name,
		Status:           string(info.Status),
		Port:             22,
		Username:         "root",
		PrivateKeyPath:   privateKeyPath,
		PublicKeyPath:    publicKeyPath,
		Image:            fallback(info.Release, "24.04"),
		Labels:           []string{"sandbox", "vpsbox", "imported"},
		Backend:          info.Backend,
		CPUs:             valueOr(info.CPUCount, 2),
		MemoryGB:         memoryGBFromMiB(info.MemoryMiB),
		DiskGB:           10,
		Hostname:         fmt.Sprintf("%s.vpsbox.local", info.Name),
		SandboxMarker:    "/etc/vpsbox-info",
		SnapshotsEnabled: info.Snapshots > 0,
	}
	if ip := firstIPv4(info.IPv4); ip != "" {
		instance.Host = ip
	}
	return instance
}

func containsInstance(instances []registry.Instance, name string) bool {
	for _, instance := range instances {
		if instance.Name == name {
			return true
		}
	}
	return false
}

func containsLabel(labels []string, value string) bool {
	for _, label := range labels {
		if label == value {
			return true
		}
	}
	return false
}

func valueOr(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func memoryGBFromMiB(value int) int {
	if value <= 0 {
		return 2
	}
	gb := int((float64(value) / 1024.0) + 0.5)
	if gb < 1 {
		return 1
	}
	return gb
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
