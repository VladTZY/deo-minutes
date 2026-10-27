// Package app exposes the Go side to the Wails frontend. Every exported method
// on App becomes a JS binding; errors returned here are shown to the user, so
// they must be Romanian (i18n.UserError) and never contain secrets.
package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
)

type App struct {
	ctx     context.Context
	version string

	mu      sync.Mutex
	cfg     config.Config
	session *session // nil when idle
	status  Status

	downloading bool // speech model download in progress

	// emit sends events to the frontend; replaced in tests.
	emit func(event string, data any)
	// llmFactory builds the model client; nil means a.newLLM (tests inject a fake).
	llmFactory func(ctx context.Context) (llm.Client, error)
}

func New(version string) *App {
	return &App{version: version, status: Status{State: StateIdle, Label: i18n.T("state." + StateIdle)}}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.emit = func(event string, data any) { wruntime.EventsEmit(ctx, event, data) }
	cfg, err := config.Load(config.Path())
	if err != nil {
		slog.Error("config load", "err", err)
		cfg = config.Defaults()
	}
	a.cfg = cfg
	go a.cleanupOld()
}

// Shutdown leaves a running meeting cleanly; the pipeline can be resumed later.
func (a *App) Shutdown(context.Context) {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil {
		s.stop()
	}
}

// Translations returns the Romanian string table for the UI.
func (a *App) Translations() map[string]string { return i18n.All() }

// Version returns the build version.
func (a *App) Version() string { return a.version }

// userMessage turns any error into Romanian text for the UI.
func userMessage(err error) string {
	var ue *i18n.UserError
	if errors.As(err, &ue) {
		return ue.Error()
	}
	return i18n.T("err.unexpected")
}
