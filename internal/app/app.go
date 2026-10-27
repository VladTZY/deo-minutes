// Package app exposes the Go side to the Wails frontend. Every exported method
// on App becomes a JS binding; errors returned here are shown to the user, so
// they must be Romanian (i18n.UserError) and never contain secrets.
package app

import (
	"context"
	"log/slog"

	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
)

type App struct {
	ctx     context.Context
	version string
	cfg     config.Config
}

func New(version string) *App { return &App{version: version} }

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	cfg, err := config.Load(config.Path())
	if err != nil {
		slog.Error("config load", "err", err)
		cfg = config.Defaults()
	}
	a.cfg = cfg
}

func (a *App) Shutdown(context.Context) {}

// Translations returns the Romanian string table for the UI.
func (a *App) Translations() map[string]string { return i18n.All() }

// Version returns the build version.
func (a *App) Version() string { return a.version }
