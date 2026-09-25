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
		err := a.manager.Deploy(context.Background(), name, templateID,
			func(message string) { a.updateJobMessage(job.ID, message) },
			func(line string) { a.appendJobLog(job.ID, line) },
		)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, tpl.Name+" is up on :"+fmt.Sprint(tpl.Port))
		return nil
	})
	return job.ID, nil
}

// LiveApp is one app published by the sandbox, shaped for the frontend.
type LiveApp struct {
	TemplateID string `json:"templateId,omitempty"`
	Name       string `json:"name"`
	Icon       string `json:"icon,omitempty"`
	Port       int    `json:"port"`
	URL        string `json:"url"`
	Running    bool   `json:"running"`
	Container  string `json:"container,omitempty"`
}

// ListApps reports what is live on the sandbox right now. It SSHes in, so it is
// a second or two of work — the Deploy tab calls it on open and after installs
// rather than on every poll.
func (a *App) ListApps(name string) ([]LiveApp, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}
	live, err := a.manager.LiveApps(context.Background(), name)
	if err != nil {
		return nil, err
	}
	out := make([]LiveApp, 0, len(live))
	for _, app := range live {
		out = append(out, LiveApp{
			TemplateID: app.TemplateID,
			Name:       app.Name,
			Icon:       app.Icon,
			Port:       app.Port,
			URL:        app.URL,
			Running:    app.Running,
			Container:  app.Container,
		})
	}
	return out, nil
}
