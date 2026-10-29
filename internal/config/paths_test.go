package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

func TestMigrateMovesLegacyDataNextToApp(t *testing.T) {
	legacy, root := t.TempDir(), t.TempDir()
	write(t, filepath.Join(legacy, "config.json"), `{"x":1}`)
	write(t, filepath.Join(legacy, "sessions", "s1", "transcript.jsonl"), "t")
	write(t, filepath.Join(legacy, "logs", "deominute.log"), "l")
	write(t, filepath.Join(legacy, "models", "model.bin"), "m")
	write(t, filepath.Join(root, "models", "vad.bin"), "v") // shipped in the zip
	write(t, filepath.Join(legacy, "browser", "Default", "x"), "cache")

	moved, err := migrate(legacy, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"config.json", "sessions", "logs", "models"} {
		if !slices.Contains(moved, want) {
			t.Errorf("%s not moved: %v", want, moved)
		}
	}
	data := filepath.Join(root, "data")
	if read(t, filepath.Join(data, "config.json")) != `{"x":1}` {
		t.Error("config not moved")
	}
	read(t, filepath.Join(data, "sessions", "s1", "transcript.jsonl"))
	read(t, filepath.Join(data, "logs", "deominute.log"))
	read(t, filepath.Join(root, "models", "model.bin"))
	read(t, filepath.Join(root, "models", "vad.bin"))
	if _, err := os.Stat(filepath.Join(data, "browser")); err == nil {
		t.Error("browser cache should not be moved")
	}
}

func TestMigrateKeepsExistingPortableData(t *testing.T) {
	legacy, root := t.TempDir(), t.TempDir()
	write(t, filepath.Join(legacy, "config.json"), "old")
	write(t, filepath.Join(legacy, "sessions", "s1", "a"), "a")
	write(t, filepath.Join(root, "data", "config.json"), "new")

	moved, err := migrate(legacy, root)
	if err != nil || len(moved) != 0 {
		t.Fatalf("moved %v, err %v", moved, err)
	}
	if read(t, filepath.Join(root, "data", "config.json")) != "new" {
		t.Error("existing config overwritten")
	}
	read(t, filepath.Join(legacy, "sessions", "s1", "a"))
}

func TestMigrateWithoutLegacyFolder(t *testing.T) {
	moved, err := migrate(filepath.Join(t.TempDir(), "missing"), t.TempDir())
	if err != nil || len(moved) != 0 {
		t.Fatalf("moved %v, err %v", moved, err)
	}
}
