package main

import (
	"context"

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

func (a *DesktopApp) OpenExternal(url string) {
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, url)
	}
}
