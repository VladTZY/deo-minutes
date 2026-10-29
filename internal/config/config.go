// Package config loads and saves the user's settings. Secrets never live here;
// see package secrets.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Gateway is how the app reaches Claude: Bedrock's Messages endpoint
// directly (empty BaseURL) or the company gateway in front of it.
type Gateway struct {
	BaseURL       string `json:"base_url"`
	Region        string `json:"region"`
	ModelID       string `json:"model_id"`
	FallbackModel string `json:"fallback_model"` // used only if the main model refuses
	Auth          string `json:"auth"`           // "sigv4" (AWS keys) | "api_key" (Bedrock API key) | "bearer" (gateway key)
	Effort        string `json:"effort"`         // low | medium | high | xhigh | max
}

type Transcribe struct {
	Region        string   `json:"region"`
	Language      string   `json:"language"`
	MultiLanguage []string `json:"multi_language"`
	RotateMin     int      `json:"rotate_min"`
}

type Bot struct {
	DisplayName     string `json:"display_name"`
	Email           string `json:"email"` // Webex asks guests for one
	BrowserPath     string `json:"browser_path"`
	LobbyTimeoutMin int    `json:"lobby_timeout_min"`
	SilenceEndMin   int    `json:"silence_end_min"`
	PostChatNotice  bool   `json:"post_chat_notice"`
	Headless        bool   `json:"headless"`
}

// Modes. DEMO: local Romanian transcriber + OpenRouter, run step by step.
// PROD: Amazon Transcribe (live) + Claude on Bedrock, fully automatic.
const (
	ModeDemo = "demo"
	ModeProd = "prod"
)

// DEMO-mode AI providers.
const (
	DemoAIOpenRouter = "openrouter"
	DemoAIOpenAI     = "openai"
)

// OpenRouter is a DEMO-mode AI provider (the default).
type OpenRouter struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// OpenAI is the other DEMO-mode AI provider: an OpenAI key, called directly.
type OpenAI struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// LocalSTT is the DEMO-mode transcriber (whisper.cpp + a Romanian model).
// Empty paths mean "next to DEOMinute.exe, else in the app data folder".
type LocalSTT struct {
	WhisperExe string `json:"whisper_exe"`
	Model      string `json:"model"`
	VADModel   string `json:"vad_model"`
	Threads    int    `json:"threads"` // 0 = all cores but one
	Language   string `json:"language"`
}

type Output struct {
	Dir           string `json:"dir"`
	Minutes       bool   `json:"minutes"` // generate the Excel minutes
	Summary       bool   `json:"summary"` // generate the Word summary
	KeepAudio     bool   `json:"keep_audio"`
	RetentionDays int    `json:"retention_days"`
}

type Last struct {
	Project            string         `json:"project"`
	PreparedBy         string         `json:"prepared_by"`
	MeetingNoByProject map[string]int `json:"meeting_no_by_project"`
}

type Config struct {
	Mode       string     `json:"mode"`
	DemoAI     string     `json:"demo_ai"` // DemoAIOpenRouter | DemoAIOpenAI
	OpenRouter OpenRouter `json:"openrouter"`
	OpenAI     OpenAI     `json:"openai"`
	LocalSTT   LocalSTT   `json:"local_stt"`
	Gateway    Gateway    `json:"gateway"`
	Transcribe Transcribe `json:"transcribe"`
	Bot        Bot        `json:"bot"`
	Output     Output     `json:"output"`
	Last       Last       `json:"last"`
}

// Defaults returns the configuration used when no file exists yet.
func Defaults() Config {
	return Config{
		Mode:       ModeDemo,
		DemoAI:     DemoAIOpenRouter,
		OpenRouter: OpenRouter{Model: "anthropic/claude-opus-5.5", Effort: "high"},
		OpenAI:     OpenAI{Model: "gpt-6-astra", Effort: "high"},
		LocalSTT:   LocalSTT{Language: "ro"},
		Gateway: Gateway{
			Region:        "eu-central-1",
			ModelID:       "anthropic.claude-opus-5-5",
			FallbackModel: "anthropic.claude-opus-4-8",
			Auth:          "sigv4",
			Effort:        "high",
		},
		Transcribe: Transcribe{
			Region:        "eu-central-1",
			Language:      "ro-RO",
			MultiLanguage: []string{"ro-RO", "en-US"},
			RotateMin:     60,
		},
		Bot: Bot{
			DisplayName:     "Asistent Minute (înregistrează)",
			Email:           "asistent.minute@example.com",
			LobbyTimeoutMin: 15,
			SilenceEndMin:   10,
			Headless:        true,
		},
		Output: Output{Dir: filepath.Join(DocumentsDir(), "DEO Minute"), RetentionDays: 30, Minutes: true, Summary: true},
		Last:   Last{MeetingNoByProject: map[string]int{}},
	}
}

// Path is the location of config.json inside the app data dir.
func Path() string { return filepath.Join(AppDataDir(), "config.json") }

// Load reads the config at path. A missing file is created with defaults.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		c := Defaults()
		return c, Save(path, c)
	}
	if err != nil {
		return Config{}, fmt.Errorf("citire config: %w", err)
	}
	c := Defaults()
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("config invalid: %w", err)
	}
	if c.Mode != ModeDemo && c.Mode != ModeProd {
		c.Mode = ModeDemo
	}
	if c.Last.MeetingNoByProject == nil {
		c.Last.MeetingNoByProject = map[string]int{}
	}
	return c, nil
}

// Save writes the config atomically with user-only permissions.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NextMeetingNo suggests the next meeting number for a project.
func (c Config) NextMeetingNo(project string) int {
	return c.Last.MeetingNoByProject[project] + 1
}
