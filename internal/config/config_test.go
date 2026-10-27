package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingWritesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Transcribe.Language != "ro-RO" || c.Bot.DisplayName == "" {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("defaults not written: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Defaults()
	c.Last.Project = "Proiect Ș"
	c.Last.MeetingNoByProject["Proiect Ș"] = 4
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextMeetingNo("Proiect Ș") != 5 {
		t.Fatalf("next meeting no = %d", got.NextMeetingNo("Proiect Ș"))
	}
}

func TestLoadPartialKeepsDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"bot":{"display_name":"X"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Bot.DisplayName != "X" || c.Transcribe.RotateMin != 60 {
		t.Fatalf("merge wrong: %+v", c)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	_ = os.WriteFile(p, []byte(`{`), 0o600)
	if _, err := Load(p); err == nil {
		t.Fatal("expected error")
	}
}
