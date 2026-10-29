package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
	"deo-minutes/internal/secrets"
	"deo-minutes/internal/stt"
)

// Defaults pre-fills the main page from the last session.
func (a *App) Defaults() StartDefaults {
	a.mu.Lock()
	defer a.mu.Unlock()
	return StartDefaults{
		Project:    a.cfg.Last.Project,
		MeetingNo:  a.cfg.NextMeetingNo(a.cfg.Last.Project),
		PreparedBy: a.cfg.Last.PreparedBy,
	}
}

// SuggestMeetingNo returns the next meeting number for a project.
func (a *App) SuggestMeetingNo(project string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.NextMeetingNo(strings.TrimSpace(project))
}

// GetSettings returns the editable settings and which secrets are stored.
func (a *App) GetSettings() Settings {
	a.mu.Lock()
	c := a.cfg
	a.mu.Unlock()
	return Settings{
		Mode:       c.Mode,
		DemoAI:     c.DemoAI,
		OpenRouter: OpenRouterSettings{Model: c.OpenRouter.Model},
		OpenAI:     OpenAISettings{Model: c.OpenAI.Model},
		Gateway:    GatewaySettings{BaseURL: c.Gateway.BaseURL, Region: c.Gateway.Region, ModelID: c.Gateway.ModelID, Auth: c.Gateway.Auth},
		Transcribe: TranscribeSettings{Region: c.Transcribe.Region, MultiLanguage: len(c.Transcribe.MultiLanguage) >= 2},
		Bot: BotSettings{DisplayName: c.Bot.DisplayName, Email: c.Bot.Email, BrowserPath: c.Bot.BrowserPath,
			LobbyTimeoutMin: c.Bot.LobbyTimeoutMin, SilenceEndMin: c.Bot.SilenceEndMin, Headless: c.Bot.Headless},
		Output:  OutputSettings{Dir: c.Output.Dir, RetentionDays: c.Output.RetentionDays, KeepAudio: c.Output.KeepAudio},
		Secrets: SecretStatus{OpenRouterKey: secrets.IsSet(secrets.OpenRouter), OpenAIKey: secrets.IsSet(secrets.OpenAI), GatewayKey: secrets.IsSet(secrets.Gateway), AWS: secrets.IsSet(secrets.AWS)},
	}
}

// SaveSettings validates and stores the settings.
func (a *App) SaveSettings(s Settings) error {
	if s.Gateway.BaseURL != "" && !strings.HasPrefix(s.Gateway.BaseURL, "https://") {
		return i18n.Errorf(nil, "err.settings.gatewayUrl")
	}
	if s.Mode != config.ModeDemo && s.Mode != config.ModeProd {
		return i18n.Errorf(nil, "err.settings.invalid")
	}
	if s.DemoAI != config.DemoAIOpenRouter && s.DemoAI != config.DemoAIOpenAI {
		return i18n.Errorf(nil, "err.settings.invalid")
	}
	switch s.Gateway.Auth {
	case llm.AuthSigV4, llm.AuthAPIKey, llm.AuthBearer:
	default:
		return i18n.Errorf(nil, "err.settings.invalid")
	}
	if s.Bot.LobbyTimeoutMin < 1 || s.Bot.SilenceEndMin < 1 || s.Output.RetentionDays < 1 || strings.TrimSpace(s.Bot.DisplayName) == "" {
		return i18n.Errorf(nil, "err.settings.invalid")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.cfg
	c.Mode = s.Mode
	c.DemoAI = s.DemoAI
	if m := strings.TrimSpace(s.OpenRouter.Model); m != "" {
		c.OpenRouter.Model = m
	}
	if m := strings.TrimSpace(s.OpenAI.Model); m != "" {
		c.OpenAI.Model = m
	}
	c.Gateway.BaseURL = strings.TrimRight(strings.TrimSpace(s.Gateway.BaseURL), "/")
	c.Gateway.Region, c.Gateway.ModelID, c.Gateway.Auth = strings.TrimSpace(s.Gateway.Region), strings.TrimSpace(s.Gateway.ModelID), s.Gateway.Auth
	c.Transcribe.Region = strings.TrimSpace(s.Transcribe.Region)
	if s.Transcribe.MultiLanguage {
		c.Transcribe.MultiLanguage = []string{"ro-RO", "en-US"}
	} else {
		c.Transcribe.MultiLanguage = nil
	}
	c.Bot.DisplayName, c.Bot.Email, c.Bot.BrowserPath = strings.TrimSpace(s.Bot.DisplayName), strings.TrimSpace(s.Bot.Email), strings.TrimSpace(s.Bot.BrowserPath)
	c.Bot.LobbyTimeoutMin, c.Bot.SilenceEndMin, c.Bot.Headless = s.Bot.LobbyTimeoutMin, s.Bot.SilenceEndMin, s.Bot.Headless
	c.Output.Dir, c.Output.RetentionDays, c.Output.KeepAudio = strings.TrimSpace(s.Output.Dir), s.Output.RetentionDays, s.Output.KeepAudio
	if err := config.Save(config.Path(), c); err != nil {
		return i18n.Errorf(err, "err.config.save", err.Error())
	}
	a.cfg = c
	return nil
}

// SetGatewayKey stores the gateway / Bedrock API key in Credential Manager.
func (a *App) SetGatewayKey(key string) error {
	if err := secrets.Set(secrets.Gateway, strings.TrimSpace(key)); err != nil {
		return i18n.Errorf(err, "err.secret.save")
	}
	return nil
}

func (a *App) DeleteGatewayKey() error { return secrets.Delete(secrets.Gateway) }

// SetMode switches between DEMO and PROD and saves it.
func (a *App) SetMode(mode string) error {
	if mode != config.ModeDemo && mode != config.ModeProd {
		return i18n.Errorf(nil, "err.settings.invalid")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		return i18n.Errorf(nil, "err.busy")
	}
	c := a.cfg
	c.Mode = mode
	if err := config.Save(config.Path(), c); err != nil {
		return i18n.Errorf(err, "err.config.save", err.Error())
	}
	a.cfg = c
	return nil
}

// Mode returns the current mode ("demo" | "prod").
func (a *App) Mode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Mode
}

// SetOpenRouterKey stores the OpenRouter API key (DEMO) in Credential Manager.
func (a *App) SetOpenRouterKey(key string) error {
	if err := secrets.Set(secrets.OpenRouter, strings.TrimSpace(key)); err != nil {
		return i18n.Errorf(err, "err.secret.save")
	}
	return nil
}

func (a *App) DeleteOpenRouterKey() error { return secrets.Delete(secrets.OpenRouter) }

// SetOpenAIKey stores the OpenAI API key (DEMO) in Credential Manager.
func (a *App) SetOpenAIKey(key string) error {
	if err := secrets.Set(secrets.OpenAI, strings.TrimSpace(key)); err != nil {
		return i18n.Errorf(err, "err.secret.save")
	}
	return nil
}

func (a *App) DeleteOpenAIKey() error { return secrets.Delete(secrets.OpenAI) }

// SetAWSCredentials stores the AWS keys used by Transcribe (and Bedrock with sigv4).
func (a *App) SetAWSCredentials(accessKeyID, secretAccessKey, sessionToken string) error {
	err := secrets.SetAWS(secrets.AWSCredentials{
		AccessKeyID:     strings.TrimSpace(accessKeyID),
		SecretAccessKey: strings.TrimSpace(secretAccessKey),
		SessionToken:    strings.TrimSpace(sessionToken),
	})
	if err != nil {
		return i18n.Errorf(err, "err.secret.save")
	}
	return nil
}

func (a *App) DeleteAWSCredentials() error { return secrets.Delete(secrets.AWS) }

// TestAI sends a tiny structured-output request to the current mode's model
// (OpenRouter or OpenAI in DEMO, Bedrock in PROD).
func (a *App) TestAI() error {
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	c, err := a.newLLM(ctx)
	if err != nil {
		return err
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
		"required": []string{"ok"}, "additionalProperties": false}
	_, err = c.JSON(ctx, llm.Request{System: "Answer with ok=true.", User: "Test de conexiune.", Schema: schema})
	return err
}

// TestTranscribe opens a stream and sends one second of silence.
func (a *App) TestTranscribe() error {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	open, err := a.newTranscriber()
	if err != nil {
		return err
	}
	st, err := open(ctx)
	if err != nil {
		return transcribeErr(err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Send(ctx, make([]byte, 6400)); err != nil {
		return transcribeErr(err)
	}
	_ = st.Finish()
	for range st.Results() {
	}
	if err := st.Err(); err != nil {
		return transcribeErr(err)
	}
	return nil
}

func transcribeErr(err error) error {
	if errors.Is(err, stt.ErrFatal) {
		return i18n.Errorf(err, "err.transcribe.auth")
	}
	return i18n.Errorf(err, "err.transcribe.failed", "conexiune eșuată")
}

// ChooseOutputDir opens a folder picker.
func (a *App) ChooseOutputDir() (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: i18n.T("settings.output.dir")})
}

// OpenPath opens a file or folder the app produced, in Excel/Explorer.
// Only paths inside the output folder or the app data folder are allowed.
func (a *App) OpenPath(path string) error {
	a.mu.Lock()
	out := a.cfg.Output.Dir
	a.mu.Unlock()
	clean, err := filepath.Abs(path)
	if err != nil || !within(clean, out) && !within(clean, config.AppDataDir()) {
		return i18n.Errorf(nil, "err.unexpected")
	}
	st, err := os.Stat(clean)
	if err != nil {
		return i18n.Errorf(err, "err.unexpected")
	}
	// Only folders and the documents the app writes: never launch programs.
	if ext := strings.ToLower(filepath.Ext(clean)); !st.IsDir() && ext != ".xlsx" && ext != ".txt" {
		return i18n.Errorf(nil, "err.unexpected")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", clean)
	case "darwin":
		cmd = exec.Command("open", clean)
	default:
		cmd = exec.Command("xdg-open", clean)
	}
	if err := cmd.Start(); err != nil {
		slog.Error("open path", "err", err)
		return i18n.Errorf(err, "err.unexpected")
	}
	return nil
}

func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	d, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(d, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
