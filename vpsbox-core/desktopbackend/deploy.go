package desktopbackend

import (
	"context"
	"fmt"
	"strings"

	"github.com/stoicsoft/vpsbox/internal/templates"
)

// DeployTemplate is one installable template, shaped for the frontend. It mirrors
// templates.Template but with JSON tags and a couple of derived hints.
type DeployTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Category    string `json:"category"` // platform | app
	Kind        string `json:"kind"`     // installer | compose | starter
	Icon        string `json:"icon"`
	Port        int    `json:"port"`
	MinMemoryMB int    `json:"minMemoryMB"`
	Note        string `json:"note,omitempty"`
}

// ListTemplates returns the deploy catalog, platforms first, for the Deploy tab.
func (a *App) ListTemplates() []DeployTemplate {
	catalog := templates.List()
	out := make([]DeployTemplate, 0, len(catalog))
	for _, t := range catalog {
		out = append(out, DeployTemplate{
			ID:          t.ID,
			Name:        t.Name,
			Summary:     t.Summary,
			Category:    string(t.Category),
			Kind:        string(t.Kind),
			Icon:        t.Icon,
			Port:        t.Port,
			MinMemoryMB: t.MinMemoryMB,
			Note:        t.Note,
		})
	}
	return out
}

// StartDeploy installs a template into the sandbox over SSH. Installs can run
// for several minutes (image pulls, swarm setup), so it returns a job id and
// streams progress like the other long-running actions.
func (a *App) StartDeploy(name, templateID string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("sandbox name is required")
	}
	tpl, ok := templates.Templates[templateID]
	if !ok {
		return "", fmt.Errorf("unknown template %q", templateID)
	}

	job := a.newJob("deploy", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Installing "+tpl.Name)
		if err := a.manager.Deploy(context.Background(), name, templateID, func(message string) {
			a.updateJobMessage(job.ID, message)
		}); err != nil {
			return err
		}
		a.updateJobMessage(job.ID, tpl.Name+" is up on :"+fmt.Sprint(tpl.Port))
		return nil
	})
	return job.ID, nil
}
