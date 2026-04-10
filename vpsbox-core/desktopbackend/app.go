package desktopbackend

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	vpsapp "github.com/stoicsoft/vpsbox/internal/app"
	"github.com/stoicsoft/vpsbox/internal/doctor"
)

type App struct {
	ctx     context.Context
	manager *vpsapp.Manager

	mu     sync.Mutex
	jobs   map[string]*Job
	update *UpdateInfo
}

type AppState struct {
	AppVersion   string        `json:"appVersion"`
	Platform     string        `json:"platform"`
	Requirements []Requirement `json:"requirements"`
	Instances    []Sandbox     `json:"instances"`
	Jobs         []Job         `json:"jobs"`
	Update       *UpdateInfo   `json:"update,omitempty"`
}

type Requirement struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Details     string `json:"details"`
	Installed   bool   `json:"installed"`
	Description string `json:"description"`
}

type Sandbox struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	Host           string `json:"host"`
	Hostname       string `json:"hostname"`
	Username       string `json:"username"`
	PrivateKeyPath string `json:"privateKeyPath"`
	HasPrivateKey  bool   `json:"hasPrivateKey"`
	Backend        string `json:"backend"`
	CreatedAt      string `json:"createdAt"`
	CPUs           int    `json:"cpus"`
	MemoryGB       int    `json:"memoryGB"`
	DiskGB         int    `json:"diskGB"`
	Imported       bool   `json:"imported"`
}

type SSHKeys struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

type CreateSandboxInput struct {
	Name       string `json:"name"`
	CPUs       int    `json:"cpus"`
	MemoryGB   int    `json:"memoryGB"`
	DiskGB     int    `json:"diskGB"`
	SelfSigned bool   `json:"selfSigned"`
}

type UpdateSandboxInput struct {
	Name     string `json:"name"`
	CPUs     int    `json:"cpus"`
	MemoryGB int    `json:"memoryGB"`
	DiskGB   int    `json:"diskGB"`
}

type Job struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	State      string `json:"state"`
	Message    string `json:"message"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

func New() *App {
	return &App{
		jobs: map[string]*Job{},
	}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	manager, err := vpsapp.NewManager(ctx)
	if err != nil {
		a.setJob(&Job{
			ID:        "bootstrap-error",
			Kind:      "bootstrap",
			State:     "error",
			Message:   err.Error(),
			StartedAt: nowString(),
		})
		return
	}
	a.manager = manager

	// Check for updates in the background so startup isn't blocked.
	go func() {
		info := checkForUpdate()
		a.mu.Lock()
		a.update = &info
		a.mu.Unlock()
	}()
}

func (a *App) GetState() (AppState, error) {
	if a.manager == nil {
		return AppState{
			AppVersion: vpsapp.Version,
			Platform:   runtime.GOOS + "/" + runtime.GOARCH,
			Jobs:       a.listJobs(),
		}, nil
	}

	checks := a.manager.Doctor(a.ctx)
	requirements := make([]Requirement, 0, len(checks))
	for _, check := range checks {
		requirements = append(requirements, mapRequirement(check))
	}

	instances, err := a.manager.List(a.ctx)
	if err != nil {
		return AppState{}, err
	}

	items := make([]Sandbox, 0, len(instances))
	for _, instance := range instances {
		created := ""
		if !instance.CreatedAt.IsZero() {
			created = instance.CreatedAt.Local().Format(time.RFC822)
		}
		items = append(items, Sandbox{
			Name:           instance.Name,
			Status:         instance.Status,
			Host:           instance.Host,
			Hostname:       instance.Hostname,
			Username:       instance.Username,
			PrivateKeyPath: instance.PrivateKeyPath,
			HasPrivateKey:  fileExists(instance.PrivateKeyPath),
			Backend:        instance.Backend,
			CreatedAt:      created,
			CPUs:           instance.CPUs,
			MemoryGB:       instance.MemoryGB,
			DiskGB:         instance.DiskGB,
			Imported:       containsLabel(instance.Labels, "imported"),
		})
	}

	a.mu.Lock()
	update := a.update
	a.mu.Unlock()

	return AppState{
		AppVersion:   vpsapp.Version,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		Requirements: requirements,
		Instances:    items,
		Jobs:         a.listJobs(),
		Update:       update,
	}, nil
}

func (a *App) StartInstallPackages() (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	job := a.newJob("install", "system")
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Checking and installing Multipass, mkcert, and cloudflared")
		return installAllPrerequisites(func(message string) {
			a.updateJobMessage(job.ID, message)
		})
	})
	return job.ID, nil
}

func (a *App) StartGenerateSSHKey(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if name == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	job := a.newJob("sshkey", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Generating SSH key")
		_, err := a.manager.GenerateSSHKey(context.Background(), name, func(message string) {
			a.updateJobMessage(job.ID, message)
		})
		if err == nil {
			a.updateJobMessage(job.ID, "SSH key is ready")
		}
		return err
	})
	return job.ID, nil
}

func (a *App) StartUpdateSandbox(input UpdateSandboxInput) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if input.Name == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	job := a.newJob("update", input.Name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Updating server resources")
		_, err := a.manager.UpdateSandbox(context.Background(), vpsapp.UpdateSandboxOptions{
			Name:     input.Name,
			CPUs:     input.CPUs,
			MemoryGB: input.MemoryGB,
			DiskGB:   input.DiskGB,
			Progress: func(message string) {
				a.updateJobMessage(job.ID, message)
			},
		})
		if err == nil {
			a.updateJobMessage(job.ID, "Server updated")
		}
		return err
	})
	return job.ID, nil
}

func (a *App) StartCreateSandbox(input CreateSandboxInput) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	target := input.Name
	if target == "" {
		name, err := a.manager.SuggestName()
		if err != nil {
			return "", err
		}
		target = name
		input.Name = name
	}

	job := a.newJob("create", target)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Preparing local server creation")
		_, err := a.manager.Up(context.Background(), vpsapp.UpOptions{
			Name:       input.Name,
			CPUs:       input.CPUs,
			MemoryGB:   input.MemoryGB,
			DiskGB:     input.DiskGB,
			SelfSigned: input.SelfSigned,
			Progress: func(message string) {
				a.updateJobMessage(job.ID, message)
			},
		})
		if err == nil {
			a.updateJobMessage(job.ID, "Sandbox is ready")
		}
		return err
	})

	return job.ID, nil
}

func (a *App) StartStartSandbox(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	job := a.newJob("start", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Starting sandbox")
		_, err := a.manager.Up(context.Background(), vpsapp.UpOptions{
			Name: name,
			Progress: func(message string) {
				a.updateJobMessage(job.ID, message)
			},
		})
		return err
	})
	return job.ID, nil
}

func (a *App) StartStopSandbox(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	job := a.newJob("stop", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Stopping sandbox")
		_, err := a.manager.Down(context.Background(), name)
		return err
	})
	return job.ID, nil
}

func (a *App) StartDestroySandbox(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	job := a.newJob("destroy", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Destroying sandbox")
		return a.manager.Destroy(context.Background(), name, true)
	})
	return job.ID, nil
}

func (a *App) StartFixLocalDomains() (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	job := a.newJob("domains", "localhost")
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Updating /etc/hosts")
		return syncLocalDomainsWithPrivileges(a.manager)
	})
	return job.ID, nil
}

func (a *App) OpenShell(name string) error {
	if a.manager == nil {
		return fmt.Errorf("desktop backend is not ready")
	}

	instance, err := a.manager.Info(context.Background(), name)
	if err != nil {
		return err
	}

	host := instance.Host
	if host == "" {
		host = instance.Hostname
	}
	command := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no %s@%s", singleQuote(instance.PrivateKeyPath), instance.Username, host)
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`tell application "Terminal" to do script %q`, command)
		return exec.Command("osascript", "-e", script).Run()
	default:
		return fmt.Errorf("OpenShell is only implemented on macOS in this desktop build")
	}
}

func (a *App) ReadSSHKeys(name string) (SSHKeys, error) {
	if a.manager == nil {
		return SSHKeys{}, fmt.Errorf("desktop backend is not ready")
	}

	instance, err := a.manager.Info(context.Background(), name)
	if err != nil {
		return SSHKeys{}, err
	}

	privPath := instance.PrivateKeyPath
	pubPath := privPath + ".pub"

	privBytes, err := os.ReadFile(privPath)
	if err != nil {
		return SSHKeys{}, fmt.Errorf("cannot read private key: %w", err)
	}

	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		return SSHKeys{}, fmt.Errorf("cannot read public key: %w", err)
	}

	return SSHKeys{
		PrivateKey: string(privBytes),
		PublicKey:  string(pubBytes),
	}, nil
}

func (a *App) RevealKeyFolder(name string) error {
	if a.manager == nil {
		return fmt.Errorf("desktop backend is not ready")
	}

	instance, err := a.manager.Info(context.Background(), name)
	if err != nil {
		return err
	}

	folder := filepath.Dir(instance.PrivateKeyPath)
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", folder).Run()
	default:
		return fmt.Errorf("RevealKeyFolder is only implemented on macOS in this desktop build")
	}
}

func (a *App) CheckForUpdate() UpdateInfo {
	info := checkForUpdate()
	a.mu.Lock()
	a.update = &info
	a.mu.Unlock()
	return info
}

func (a *App) newJob(kind, target string) *Job {
	a.mu.Lock()
	defer a.mu.Unlock()

	id := fmt.Sprintf("%s-%d", kind, time.Now().UnixNano())
	job := &Job{
		ID:        id,
		Kind:      kind,
		Target:    target,
		State:     "running",
		Message:   "Queued",
		StartedAt: nowString(),
	}
	a.jobs[id] = job
	return job
}

func (a *App) runJob(id string, fn func(job *Job) error) {
	job := a.getJob(id)
	if job == nil {
		return
	}
	if err := fn(job); err != nil {
		a.finishJob(id, "error", err.Error())
		return
	}
	a.finishJob(id, "done", job.Message)
}

func (a *App) finishJob(id, state, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if job, ok := a.jobs[id]; ok {
		job.State = state
		job.Message = message
		job.FinishedAt = nowString()
	}
}

func (a *App) updateJobMessage(id, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if job, ok := a.jobs[id]; ok {
		job.Message = message
	}
}

func (a *App) getJob(id string) *Job {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.jobs[id]
}

func (a *App) setJob(job *Job) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.jobs[job.ID] = job
}

func (a *App) listJobs() []Job {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Job, 0, len(a.jobs))
	for _, job := range a.jobs {
		out = append(out, *job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

func mapRequirement(check doctor.Check) Requirement {
	description := check.Details
	switch check.Name {
	case "multipass":
		description = "Virtual machine engine for local Ubuntu sandboxes"
	case "mkcert":
		description = "Local certificate authority used for trusted HTTPS"
	case "cloudflared":
		description = "Share tunnels and public preview links"
	case "hosts":
		description = "Optional local hostname aliases written to /etc/hosts"
	}
	return Requirement{
		Name:        check.Name,
		Status:      string(check.Status),
		Details:     check.Details,
		Installed:   check.Status == doctor.StatusOK || check.Name == "hosts",
		Description: description,
	}
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func containsLabel(labels []string, value string) bool {
	for _, label := range labels {
		if label == value {
			return true
		}
	}
	return false
}
