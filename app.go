package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/stoicsoft/vpsbox/desktopbackend"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type AppState = desktopbackend.AppState
type Requirement = desktopbackend.Requirement
type Sandbox = desktopbackend.Sandbox
type Job = desktopbackend.Job
type CreateSandboxInput = desktopbackend.CreateSandboxInput
type UpdateSandboxInput = desktopbackend.UpdateSandboxInput
type SSHKeys = desktopbackend.SSHKeys
type UpdateInfo = desktopbackend.UpdateInfo
type UpdateDownload = desktopbackend.UpdateDownload
type ServerLogs = desktopbackend.ServerLogs
type ServerLogEntry = desktopbackend.ServerLogEntry
type SnapshotList = desktopbackend.SnapshotList
type SnapshotEntry = desktopbackend.SnapshotEntry
type ServerDiff = desktopbackend.ServerDiff
type DiffEntry = desktopbackend.DiffEntry
type DeployTemplate = desktopbackend.DeployTemplate
type LiveApp = desktopbackend.LiveApp
type ObjectStore = desktopbackend.ObjectStore
type Bucket = desktopbackend.Bucket
type Workspace = desktopbackend.Workspace
type WorkspaceMember = desktopbackend.WorkspaceMember
type PushInput = desktopbackend.PushInput
type ImportVPSInput = desktopbackend.ImportVPSInput
type ImportPreview = desktopbackend.ImportPreview
type TransferPlan = desktopbackend.TransferPlan
type TransferPath = desktopbackend.TransferPath
type TransferVolume = desktopbackend.TransferVolume
type TransferCompose = desktopbackend.TransferCompose

type DesktopApp struct {
	ctx     context.Context
	backend *desktopbackend.App
}

func NewApp() *DesktopApp {
	return &DesktopApp{
		backend: desktopbackend.New(),
	}
}

func (a *DesktopApp) startup(ctx context.Context) {
	a.ctx = ctx
	a.backend.Startup(ctx)
}

func (a *DesktopApp) GetState() (AppState, error) {
	return a.backend.GetState()
}

func (a *DesktopApp) StartInstallPackages() (string, error) {
	return a.backend.StartInstallPackages()
}

func (a *DesktopApp) StartGenerateSSHKey(name string) (string, error) {
	return a.backend.StartGenerateSSHKey(name)
}

func (a *DesktopApp) StartUpdateSandbox(input UpdateSandboxInput) (string, error) {
	return a.backend.StartUpdateSandbox(input)
}

func (a *DesktopApp) StartCreateSandbox(input CreateSandboxInput) (string, error) {
	return a.backend.StartCreateSandbox(input)
}

func (a *DesktopApp) StartStartSandbox(name string) (string, error) {
	return a.backend.StartStartSandbox(name)
}

func (a *DesktopApp) StartStopSandbox(name string) (string, error) {
	return a.backend.StartStopSandbox(name)
}

func (a *DesktopApp) StartDestroySandbox(name string) (string, error) {
	return a.backend.StartDestroySandbox(name)
}

func (a *DesktopApp) StartFixLocalDomains() (string, error) {
	return a.backend.StartFixLocalDomains()
}

func (a *DesktopApp) OpenShell(name string) error {
	return a.backend.OpenShell(name)
}

func (a *DesktopApp) ReadSSHKeys(name string) (SSHKeys, error) {
	return a.backend.ReadSSHKeys(name)
}

func (a *DesktopApp) RevealKeyFolder(name string) error {
	return a.backend.RevealKeyFolder(name)
}

func (a *DesktopApp) CheckForUpdate() UpdateInfo {
	return a.backend.CheckForUpdate()
}

func (a *DesktopApp) StartDownloadUpdate() (string, error) {
	return a.backend.StartDownloadUpdate()
}

func (a *DesktopApp) CancelUpdateDownload() error {
	return a.backend.CancelUpdateDownload()
}

// ApplyUpdate installs the downloaded update. The backend hands the swap to a
// helper that waits for this process to exit, so quitting is part of applying —
// the helper relaunches the new version once we're gone. The quit is delayed a
// beat so the frontend's call can return and paint "Restarting…" first.
func (a *DesktopApp) ApplyUpdate() error {
	if err := a.backend.ApplyUpdate(); err != nil {
		return err
	}
	if a.ctx == nil {
		return nil
	}
	go func() {
		time.Sleep(600 * time.Millisecond)
		wruntime.Quit(a.ctx)
	}()
	return nil
}

func (a *DesktopApp) GetServerLogs(name string) (ServerLogs, error) {
	return a.backend.GetServerLogs(name)
}

func (a *DesktopApp) StartCheckpoint(name string, label string) (string, error) {
	return a.backend.StartCheckpoint(name, label)
}

func (a *DesktopApp) StartRestoreSnapshot(name string, snapshot string) (string, error) {
	return a.backend.StartRestoreSnapshot(name, snapshot)
}

func (a *DesktopApp) StartDeleteSnapshot(name string, snapshot string) (string, error) {
	return a.backend.StartDeleteSnapshot(name, snapshot)
}

func (a *DesktopApp) ListSnapshots(name string) (SnapshotList, error) {
	return a.backend.ListSnapshots(name)
}

func (a *DesktopApp) GetServerDiff(name string) (ServerDiff, error) {
	return a.backend.GetServerDiff(name)
}

func (a *DesktopApp) ListTemplates() []DeployTemplate {
	return a.backend.ListTemplates()
}

func (a *DesktopApp) StartDeploy(name string, templateID string) (string, error) {
	return a.backend.StartDeploy(name, templateID)
}

func (a *DesktopApp) ListApps(name string) ([]LiveApp, error) {
	return a.backend.ListApps(name)
}

func (a *DesktopApp) GetObjectStore() (ObjectStore, error) {
	return a.backend.GetObjectStore()
}

func (a *DesktopApp) StartCreateBucket(name string) (string, error) {
	return a.backend.StartCreateBucket(name)
}

func (a *DesktopApp) StartDeleteBucket(name string, force bool) (string, error) {
	return a.backend.StartDeleteBucket(name, force)
}

func (a *DesktopApp) StartAttachBuckets(name string) (string, error) {
	return a.backend.StartAttachBuckets(name)
}

func (a *DesktopApp) StartStopObjectStore() (string, error) {
	return a.backend.StartStopObjectStore()
}

func (a *DesktopApp) ListWorkspaces() ([]Workspace, error) {
	return a.backend.ListWorkspaces()
}

func (a *DesktopApp) CreateWorkspace(name string) (Workspace, error) {
	return a.backend.CreateWorkspace(name)
}

func (a *DesktopApp) StartJoinWorkspace(name string, instanceName string) (string, error) {
	return a.backend.StartJoinWorkspace(name, instanceName)
}

func (a *DesktopApp) StartLeaveWorkspace(name string, instanceName string) (string, error) {
	return a.backend.StartLeaveWorkspace(name, instanceName)
}

func (a *DesktopApp) StartSyncWorkspace(name string) (string, error) {
	return a.backend.StartSyncWorkspace(name)
}

func (a *DesktopApp) StartDestroyWorkspace(name string) (string, error) {
	return a.backend.StartDestroyWorkspace(name)
}

func (a *DesktopApp) CheckWorkspaceReachability(name string, fromInstance string) (map[string]bool, error) {
	return a.backend.CheckWorkspaceReachability(name, fromInstance)
}

func (a *DesktopApp) PreviewPush(input PushInput) (TransferPlan, error) {
	return a.backend.PreviewPush(input)
}

func (a *DesktopApp) StartPushToVPS(input PushInput) (string, error) {
	return a.backend.StartPushToVPS(input)
}

func (a *DesktopApp) StartImportFromVPS(input ImportVPSInput) (string, error) {
	return a.backend.StartImportFromVPS(input)
}

func (a *DesktopApp) PreviewImportVPS(input ImportVPSInput) (ImportPreview, error) {
	return a.backend.PreviewImportVPS(input)
}

// PickSSHKey opens a native file dialog for choosing a private key, starting
// in ~/.ssh where keys actually live. Returns "" when the user cancels.
func (a *DesktopApp) PickSSHKey() (string, error) {
	if a.ctx == nil {
		return "", nil
	}
	home, _ := os.UserHomeDir()
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:                "Choose an SSH private key",
		DefaultDirectory:     filepath.Join(home, ".ssh"),
		ShowHiddenFiles:      true,
		CanCreateDirectories: false,
	})
}

func (a *DesktopApp) OpenExternal(url string) {
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, url)
	}
}
