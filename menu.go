package main

import (
	"fmt"

	"github.com/stoicsoft/vpsbox/desktopbackend"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	websiteURL  = "https://servercompass.app"
	releasesURL = "https://github.com/stoicsoft/vpsbox-app/releases"
)

func appVersion() string {
	return desktopbackend.Version()
}

// checkForUpdatesFromMenu runs a forced update check and reports the result in
// a native dialog. It also refreshes the backend's cached UpdateInfo, so the
// in-app banner picks up the same answer on the next GetState poll.
//
// Callers must run this off the main thread: the check makes a network call and
// the dialog blocks until dismissed.
func (a *DesktopApp) checkForUpdatesFromMenu() {
	info := a.backend.CheckForUpdate()
	if a.ctx == nil {
		return
	}

	switch {
	case info.Error != "":
		_, _ = wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
			Type:    wruntime.ErrorDialog,
			Title:   "Update check failed",
			Message: info.Error,
		})
	case info.Available:
		choice, err := wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
			Type:          wruntime.QuestionDialog,
			Title:         "Update available",
			Message:       fmt.Sprintf("vpsbox %s is available.\n\nYou have %s.", info.Latest, info.Current),
			Buttons:       []string{"Download", "Later"},
			DefaultButton: "Download",
			CancelButton:  "Later",
		})
		if err == nil && choice == "Download" {
			url := info.URL
			if url == "" {
				url = releasesURL
			}
			a.OpenExternal(url)
		}
	default:
		_, _ = wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
			Type:    wruntime.InfoDialog,
			Title:   "You're up to date",
			Message: fmt.Sprintf("vpsbox %s is the latest version.", info.Current),
		})
	}
}
