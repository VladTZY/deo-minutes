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

type Gateway struct {
	Kind    string `json:"kind"` // "bedrock" | "openai_compat"
	BaseURL string `json:"base_url"`
	Region  string `json:"region"`
	ModelID string `json:"model_id"`
	Auth    string `json:"auth"` // "bearer" | "sigv4"
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

type Output struct {
	Dir           string `json:"dir"`
	KeepAudio     bool   `json:"keep_audio"`
	RetentionDays int    `json:"retention_days"`
}

type Last struct {
	Project            string         `json:"project"`
	PreparedBy         string         `json:"prepared_by"`
	MeetingNoByProject map[string]int `json:"meeting_no_by_project"`
}

type Config struct {
	Gateway    Gateway    `json:"gateway"`
	Transcribe Transcribe `json:"transcribe"`
	Bot        Bot        `json:"bot"`
	Output     Output     `json:"output"`
	Last       Last       `json:"last"`
}

// Defaults returns the configuration used when no file exists yet.
func Defaults() Config {
	return Config{
		Gateway: Gateway{Kind: "bedrock", Region: "eu-central-1", Auth: "bearer"},
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
		Output: Output{Dir: filepath.Join(DocumentsDir(), "DEO Minute"), RetentionDays: 30},
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
