package main

import (
	_ "embed"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed build/appicon.png
var aboutIcon []byte

// applicationMenu builds the macOS menu bar: the standard App/Edit/Window menus
// plus a Help menu. "About vpsbox" is contributed to the App menu by Wails from
// aboutInfo() — the App menu role itself can't take custom items.
func (a *DesktopApp) applicationMenu() *menu.Menu {
	root := menu.NewMenu()
	root.Append(menu.AppMenu())
	root.Append(menu.EditMenu())
	root.Append(menu.WindowMenu())

	help := root.AddSubmenu("Help")
	// The check hits the network and opens a modal, so keep it off the main thread.
	help.AddText("Check for Updates…", nil, func(*menu.CallbackData) {
		go a.checkForUpdatesFromMenu()
	})
	help.AddSeparator()
	help.AddText("Server Compass Website", nil, func(*menu.CallbackData) {
		a.OpenExternal(websiteURL)
	})
	help.AddText("Release Notes", nil, func(*menu.CallbackData) {
		a.OpenExternal(releasesURL)
	})
	return root
}

func aboutInfo() *mac.AboutInfo {
	return &mac.AboutInfo{
		Title: "vpsbox",
		Message: fmt.Sprintf(
			"Version %s\n\nLocal Ubuntu sandboxes for trying deploy tools.\n\n%s",
			appVersion(), websiteURL),
		Icon: aboutIcon,
	}
}
