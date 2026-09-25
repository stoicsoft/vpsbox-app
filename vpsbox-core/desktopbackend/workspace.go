package desktopbackend

import (
	"context"
	"fmt"
	"strings"
)

// Workspace is a private network, shaped for the frontend.
type Workspace struct {
	Name    string            `json:"name"`
	CIDR    string            `json:"cidr"`
	Gateway string            `json:"gateway"`
	Members []WorkspaceMember `json:"members"`
}

// WorkspaceMember is one sandbox on a private network.
type WorkspaceMember struct {
	Instance  string `json:"instance"`
	PrivateIP string `json:"privateIp"`
	Hostname  string `json:"hostname"`
	Status    string `json:"status"`
}

// ListWorkspaces returns every private network. It reads local state only, so it
// is cheap enough for a UI poll.
func (a *App) ListWorkspaces() ([]Workspace, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("desktop backend is not ready")
	}

	infos, err := a.manager.ListWorkspaces(context.Background())
	if err != nil {
		return nil, err
	}

	out := make([]Workspace, 0, len(infos))
	for _, info := range infos {
		workspace := Workspace{
			Name:    info.Name,
			CIDR:    info.CIDR,
			Gateway: info.Gateway,
			Members: make([]WorkspaceMember, 0, len(info.Members)),
		}
		for _, member := range info.Members {
			workspace.Members = append(workspace.Members, WorkspaceMember{
				Instance:  member.Instance,
				PrivateIP: member.PrivateIP,
				Hostname:  member.Hostname,
				Status:    member.Status,
			})
		}
		out = append(out, workspace)
	}
	return out, nil
}

// CreateWorkspace allocates a private network. Nothing is applied to a sandbox
// until one joins, so this is fast and does not need to be a job.
func (a *App) CreateWorkspace(name string) (Workspace, error) {
	if a.manager == nil {
		return Workspace{}, fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return Workspace{}, fmt.Errorf("workspace name is required")
	}

	info, err := a.manager.CreateWorkspace(context.Background(), name)
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{Name: info.Name, CIDR: info.CIDR, Gateway: info.Gateway, Members: []WorkspaceMember{}}, nil
}

// StartJoinWorkspace puts a sandbox on a private network. It SSHes into every
// member to re-sync names, so it streams progress like a deploy.
func (a *App) StartJoinWorkspace(name, instanceName string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(instanceName) == "" {
		return "", fmt.Errorf("workspace and sandbox names are required")
	}

	job := a.newJob("workspace-join", instanceName)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Connecting "+instanceName+" to "+name)
		_, err := a.manager.JoinWorkspace(context.Background(), name, instanceName,
			func(message string) { a.updateJobMessage(job.ID, message) },
			func(line string) { a.appendJobLog(job.ID, line) },
		)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, instanceName+" is on "+name)
		return nil
	})
	return job.ID, nil
}

// StartLeaveWorkspace takes a sandbox off a private network.
func (a *App) StartLeaveWorkspace(name, instanceName string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" || strings.TrimSpace(instanceName) == "" {
		return "", fmt.Errorf("workspace and sandbox names are required")
	}

	job := a.newJob("workspace-leave", instanceName)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Removing "+instanceName+" from "+name)
		_, err := a.manager.LeaveWorkspace(context.Background(), name, instanceName,
			func(message string) { a.updateJobMessage(job.ID, message) },
			func(line string) { a.appendJobLog(job.ID, line) },
		)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, instanceName+" is off "+name)
		return nil
	})
	return job.ID, nil
}

// StartSyncWorkspace re-applies a private network to every member — the repair
// action for sandboxes that were stopped when something changed.
func (a *App) StartSyncWorkspace(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("workspace name is required")
	}

	job := a.newJob("workspace-sync", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Re-applying "+name)
		_, err := a.manager.SyncWorkspace(context.Background(), name,
			func(message string) { a.updateJobMessage(job.ID, message) },
			func(line string) { a.appendJobLog(job.ID, line) },
		)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, name+" is in sync")
		return nil
	})
	return job.ID, nil
}

// StartDestroyWorkspace removes a private network and takes every member off it.
// The sandboxes themselves are left alone.
func (a *App) StartDestroyWorkspace(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("workspace name is required")
	}

	job := a.newJob("workspace-destroy", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Removing workspace "+name)
		if err := a.manager.DestroyWorkspace(context.Background(), name,
			func(message string) { a.updateJobMessage(job.ID, message) },
			func(line string) { a.appendJobLog(job.ID, line) },
		); err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Workspace "+name+" removed")
		return nil
	})
	return job.ID, nil
}

// CheckWorkspaceReachability pings every member from one of them, so the UI can
// show whether the network actually works rather than only that it is configured.
func (a *App) CheckWorkspaceReachability(name, fromInstance string) (map[string]bool, error) {
	if a.manager == nil {
		return nil, fmt.Errorf("desktop backend is not ready")
	}
	return a.manager.WorkspaceReachability(context.Background(), name, fromInstance)
}
