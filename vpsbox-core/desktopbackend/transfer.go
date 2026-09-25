package desktopbackend

import (
	"context"
	"fmt"
	"strings"

	vpsapp "github.com/stoicsoft/vpsbox/internal/app"
	"github.com/stoicsoft/vpsbox/internal/migrate"
)

// PushInput names a sandbox and the real VPS it should be pushed to.
type PushInput struct {
	Name    string `json:"name"`
	User    string `json:"user"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	KeyPath string `json:"keyPath"`
}

// ImportVPSInput names a real VPS to clone into a fresh sandbox.
type ImportVPSInput struct {
	User    string `json:"user"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	KeyPath string `json:"keyPath"`
	Name    string `json:"name"` // optional sandbox name
}

// TransferPlan is a migration plan shaped for the frontend.
type TransferPlan struct {
	Source      string            `json:"source"`
	Dest        string            `json:"dest"`
	Packages    []string          `json:"packages"`
	Services    []string          `json:"services"`
	Users       []string          `json:"users"`
	Paths       []TransferPath    `json:"paths"`
	Volumes     []TransferVolume  `json:"volumes"`
	Compose     []TransferCompose `json:"compose"`
	Notes       []string          `json:"notes"`
	EstimatedMB int               `json:"estimatedMB"`
}

type TransferPath struct {
	Path   string `json:"path"`
	SizeMB int    `json:"sizeMB"`
}

type TransferVolume struct {
	Name   string `json:"name"`
	SizeMB int    `json:"sizeMB"`
}

type TransferCompose struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

func transferTarget(user, host string, port int, keyPath string) (migrate.Target, error) {
	target, err := migrate.ParseTarget(strings.TrimSpace(user) + "@" + strings.TrimSpace(host))
	if err != nil {
		return migrate.Target{}, err
	}
	if port > 0 {
		target.Port = port
	}
	target.KeyPath = strings.TrimSpace(keyPath)
	return target, nil
}

func mapTransferPlan(plan *migrate.Plan) TransferPlan {
	out := TransferPlan{
		Source:      plan.Source,
		Dest:        plan.Dest,
		Packages:    plan.Packages,
		Services:    plan.Services,
		Notes:       plan.Notes,
		EstimatedMB: int(plan.EstimatedKB() / 1024),
	}
	for _, user := range plan.Users {
		out.Users = append(out.Users, user.Name)
	}
	for _, path := range plan.Paths {
		out.Paths = append(out.Paths, TransferPath{Path: path.Abs(), SizeMB: int(path.SizeKB / 1024)})
	}
	for _, volume := range plan.Volumes {
		out.Volumes = append(out.Volumes, TransferVolume{Name: volume.Name, SizeMB: int(volume.SizeKB / 1024)})
	}
	for _, project := range plan.Compose {
		out.Compose = append(out.Compose, TransferCompose{Name: project.Name, Running: project.Running})
	}
	return out
}

// PreviewPush computes the migration plan without changing anything. The
// frontend shows it in the push dialog, so clicking "Push" is informed
// consent rather than a leap.
func (a *App) PreviewPush(input PushInput) (TransferPlan, error) {
	if a.manager == nil {
		return TransferPlan{}, fmt.Errorf("desktop backend is not ready")
	}
	target, err := transferTarget(input.User, input.Host, input.Port, input.KeyPath)
	if err != nil {
		return TransferPlan{}, err
	}
	plan, err := a.manager.Push(a.ctx, vpsapp.PushOptions{
		Name:   input.Name,
		Target: target,
		DryRun: true,
	})
	if err != nil {
		return TransferPlan{}, err
	}
	return mapTransferPlan(plan), nil
}

// StartPushToVPS pushes a sandbox onto a real VPS as a tracked job. The
// frontend is expected to have shown PreviewPush's plan first.
func (a *App) StartPushToVPS(input PushInput) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if input.Name == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	target, err := transferTarget(input.User, input.Host, input.Port, input.KeyPath)
	if err != nil {
		return "", err
	}
	job := a.newJob("push", input.Name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, fmt.Sprintf("Pushing %s to %s", input.Name, target.Label()))
		_, err := a.manager.Push(context.Background(), vpsapp.PushOptions{
			Name:   input.Name,
			Target: target,
			Status: func(message string) { a.updateJobMessage(job.ID, message) },
			Output: func(line string) { a.appendJobLog(job.ID, line) },
		})
		if err == nil {
			a.updateJobMessage(job.ID, fmt.Sprintf("Pushed %s to %s", input.Name, target.Label()))
		}
		return err
	})
	return job.ID, nil
}

// ImportPreview is what an import will do, computed by only reading the
// server — shown in the import dialog before any VM exists.
type ImportPreview struct {
	Server      string            `json:"server"`      // "Ubuntu 26.04 LTS at root@host"
	OS          string            `json:"os"`          // pretty OS name
	SandboxName string            `json:"sandboxName"` // the name the sandbox will get
	CPUs        int               `json:"cpus"`        // sandbox sizing
	MemoryGB    int               `json:"memoryGB"`
	DiskGB      int               `json:"diskGB"`
	Packages    int               `json:"packages"` // manual packages on the server
	EstimatedMB int               `json:"estimatedMB"`
	Volumes     []TransferVolume  `json:"volumes"`
	Compose     []TransferCompose `json:"compose"`
}

// PreviewImportVPS inspects the server without changing anything, so the
// import dialog can show what will happen before a VM is created.
func (a *App) PreviewImportVPS(input ImportVPSInput) (ImportPreview, error) {
	if a.manager == nil {
		return ImportPreview{}, fmt.Errorf("desktop backend is not ready")
	}
	target, err := transferTarget(input.User, input.Host, input.Port, input.KeyPath)
	if err != nil {
		return ImportPreview{}, err
	}
	manifest, err := a.manager.InspectVPS(a.ctx, target)
	if err != nil {
		return ImportPreview{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		if name, err = a.manager.SuggestImportName(); err != nil {
			return ImportPreview{}, err
		}
	}

	var estimatedKB int64
	for _, path := range manifest.Paths {
		estimatedKB += path.SizeKB
	}
	preview := ImportPreview{
		OS:          manifest.OS.PrettyName,
		Server:      fmt.Sprintf("%s at %s", manifest.OS.PrettyName, target.Label()),
		SandboxName: name,
		Packages:    len(manifest.Packages),
	}
	for _, volume := range manifest.Volumes {
		estimatedKB += volume.SizeKB
		preview.Volumes = append(preview.Volumes, TransferVolume{Name: volume.Name, SizeMB: int(volume.SizeKB / 1024)})
	}
	for _, project := range manifest.Compose {
		preview.Compose = append(preview.Compose, TransferCompose{Name: project.Name, Running: project.Running})
	}
	preview.EstimatedMB = int(estimatedKB / 1024)
	preview.CPUs, preview.MemoryGB, preview.DiskGB = migrate.SandboxSize(manifest.Resources, estimatedKB)
	return preview, nil
}

// StartImportFromVPS clones a real VPS into a fresh sandbox as a tracked job.
// The sandbox name is resolved before the job starts, the way
// StartCreateSandbox does it, so the frontend can select the sandbox when the
// job finishes.
func (a *App) StartImportFromVPS(input ImportVPSInput) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	target, err := transferTarget(input.User, input.Host, input.Port, input.KeyPath)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(input.Name) == "" {
		name, err := a.manager.SuggestImportName()
		if err != nil {
			return "", err
		}
		input.Name = name
	}
	job := a.newJob("import", input.Name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, fmt.Sprintf("Importing %s into a local sandbox", target.Label()))
		instance, _, err := a.manager.ImportVPS(context.Background(), vpsapp.ImportVPSOptions{
			Target: target,
			Name:   input.Name,
			Status: func(message string) { a.updateJobMessage(job.ID, message) },
			Output: func(line string) { a.appendJobLog(job.ID, line) },
		})
		if err == nil {
			a.updateJobMessage(job.ID, fmt.Sprintf("%s is a local copy of %s", instance.Name, target.Label()))
		}
		return err
	})
	return job.ID, nil
}
