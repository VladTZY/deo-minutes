package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/anthropics/anthropic-sdk-go"

	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
	"deo-minutes/internal/secrets"
	"deo-minutes/internal/stt"
)

// File names of the local transcriber, looked up next to DEOMinute.exe and
// then in the app data folder (portable zip layout: whisper\, models\).
const (
	whisperExeName = "whisper-cli"
	modelFileName  = "ggml-whisper-ro.bin"
	vadFileName    = "ggml-silero-v6.2.0.bin"
)

func jsonEncoder(w io.Writer) *json.Encoder { return json.NewEncoder(w) }

// newLLM builds the AI client for the current mode: OpenRouter or OpenAI in
// DEMO, Claude on Bedrock in PROD.
func (a *App) newLLM(ctx context.Context) (llm.Client, error) {
	a.mu.Lock()
	c := a.cfg
	a.mu.Unlock()
	if c.Mode == config.ModeDemo && c.DemoAI == config.DemoAIOpenAI {
		key, err := secrets.Get(secrets.OpenAI)
		if err != nil {
			return nil, i18n.Errorf(err, "err.openai.notConfigured")
		}
		return llm.NewOpenAI(key, c.OpenAI.Model, c.OpenAI.Effort)
	}
	if c.Mode == config.ModeDemo {
		key, err := secrets.Get(secrets.OpenRouter)
		if err != nil {
			return nil, i18n.Errorf(err, "err.openrouter.notConfigured")
		}
		return llm.NewOpenRouter(key, c.OpenRouter.Model, c.OpenRouter.Effort)
	}
	g := c.Gateway
	cfg := llm.Config{BaseURL: g.BaseURL, Region: g.Region, Model: g.ModelID, FallbackModel: g.FallbackModel,
		Auth: g.Auth, Effort: anthropic.OutputConfigEffort(g.Effort)}
	switch g.Auth {
	case llm.AuthSigV4:
		creds, err := secrets.GetAWS()
		if err != nil {
			return nil, i18n.Errorf(err, "err.gateway.notConfigured")
		}
		cfg.AWS = creds
	default:
		key, err := secrets.Get(secrets.Gateway)
		if err != nil {
			return nil, i18n.Errorf(err, "err.gateway.notConfigured")
		}
		cfg.Key = key
	}
	return llm.NewAnthropic(ctx, cfg)
}

// newTranscriber opens Amazon Transcribe streams (PROD).
func (a *App) newTranscriber() (stt.Opener, error) {
	creds, err := secrets.GetAWS()
	if err != nil {
		return nil, i18n.Errorf(err, "err.transcribe.creds")
	}
	a.mu.Lock()
	t := a.cfg.Transcribe
	a.mu.Unlock()
	return stt.NewTranscribeOpener(stt.TranscribeConfig{Region: t.Region, Language: t.Language, MultiLanguage: t.MultiLanguage}, creds), nil
}

// localPaths resolves the local transcriber files.
func (a *App) localPaths() (exe, model, vad string) {
	a.mu.Lock()
	l := a.cfg.LocalSTT
	a.mu.Unlock()
	exeName := whisperExeName
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exe = firstExisting(l.WhisperExe, filepath.Join("whisper", exeName))
	model = firstExisting(l.Model, filepath.Join("models", modelFileName))
	vad = firstExisting(l.VADModel, filepath.Join("models", vadFileName))
	return exe, model, vad
}

// firstExisting returns override if set, else rel next to the executable,
// else rel in the app data folder or the pre-portable one; "" when none exists.
func firstExisting(override, rel string) string {
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override
		}
		return ""
	}
	var bases []string
	if exe, err := os.Executable(); err == nil {
		bases = append(bases, filepath.Dir(exe))
	}
	bases = append(bases, config.AppDataDir(), config.LegacyDataDir())
	for _, b := range bases {
		p := filepath.Join(b, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// newLocal builds the local transcriber (DEMO step 2).
func (a *App) newLocal() (*stt.Local, error) {
	exe, model, vad := a.localPaths()
	if exe == "" || model == "" {
		return nil, i18n.Errorf(nil, "err.local.missing")
	}
	a.mu.Lock()
	l := a.cfg.LocalSTT
	a.mu.Unlock()
	return &stt.Local{Exe: exe, Model: model, VADModel: vad, Language: l.Language, Threads: l.Threads}, nil
}

// LocalStatus tells the UI whether the local transcriber is installed.
func (a *App) LocalStatus() LocalStatus {
	exe, model, vad := a.localPaths()
	return LocalStatus{WhisperFound: exe != "", ModelFound: model != "", VADFound: vad != "", ModelPath: model}
}
