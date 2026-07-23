package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// init prepends ~/.vpsbox/bin to PATH so binaries the desktop installer
// drops there (mkcert, cloudflared on Linux/Windows) are findable by
// executil.LookPath without requiring system-wide install.
func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	binDir := filepath.Join(home, ".vpsbox", "bin")
	existing := os.Getenv("PATH")
	if existing == "" {
		_ = os.Setenv("PATH", binDir)
		return
	}
	_ = os.Setenv("PATH", binDir+string(os.PathListSeparator)+existing)
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "vpsbox",
		Width:     1280,
		Height:    860,
		MinWidth:  1100,
		MinHeight: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// The frontend draws its own unified toolbar, so on macOS we hide the
		// system title bar and inset the traffic lights over it. The toolbar's
		// lead cell reserves 76px for them (see .app-mac .toolbar-lead).
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.DefaultAppearance,
		},
		BackgroundColour: &options.RGBA{R: 236, G: 236, B: 238, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
