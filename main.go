package main

import (
	"embed"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"deo-minutes/internal/app"
	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/logx"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

// gpuOffArgs keeps WebView2 off the GPU (constraint C7). Set through the
// environment as well as WebviewGpuIsDisabled so compositing is disabled too.
const gpuOffArgs = "--disable-gpu --disable-gpu-compositing"

func main() {
	if closer, err := logx.Setup(config.LogsDir()); err == nil {
		defer func() { _ = closer.Close() }()
	}
	slog.Info("start", "version", version)

	_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", gpuOffArgs)

	a := app.New(version)
	err := wails.Run(&options.App{
		Title:     i18n.T("app.title"),
		Width:     1100,
		Height:    780,
		MinWidth:  860,
		MinHeight: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 247, G: 247, B: 245, A: 1},
		OnStartup:        a.Startup,
		OnShutdown:       a.Shutdown,
		Bind:             []interface{}{a},
		Windows: &windows.Options{
			WebviewGpuIsDisabled: true,
			WebviewUserDataPath:  config.WebviewDir(),
			Theme:                windows.Light,
		},
	})
	if err != nil {
		slog.Error("wails run", "err", err)
		os.Exit(1)
	}
}
