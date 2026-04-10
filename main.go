package main

import (
	"embed"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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
		BackgroundColour: &options.RGBA{R: 248, G: 250, B: 252, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
