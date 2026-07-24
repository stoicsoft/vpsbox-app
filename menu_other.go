//go:build !darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

// Windows and Linux draw a native menu bar inside the window, which would sit
// on top of the frontend's own toolbar. Those platforms reach the same actions
// (about/version, update check, links) from the Environment view instead.
func (a *DesktopApp) applicationMenu() *menu.Menu { return nil }

func aboutInfo() *mac.AboutInfo { return nil }
