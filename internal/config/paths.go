package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const appDirName = "DEOMinutes"

// The app is portable: on Windows its data (config, sessions, logs, the
// bot's browser profile) lives in a "data" folder next to DEOMinute.exe and
// the downloaded model in "models", beside the bundled "whisper". When the
// exe's folder cannot be written, and on dev machines, everything stays in
// %LOCALAPPDATA%\DEOMinutes as before.

var (
	resolveOnce sync.Once
	rootDir     string // exe folder in portable mode, else ""
	migrated    []string
	migrateErr  error
)

func resolve() {
	resolveOnce.Do(func() {
		if runtime.GOOS != "windows" {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			return
		}
		root := filepath.Dir(exe)
		data := filepath.Join(root, "data")
		if !writable(data) {
			return
		}
		rootDir = root
		migrated, migrateErr = migrate(LegacyDataDir(), root)
	})
}

// AppDataDir is where config, sessions and logs live (see above).
// The fallback is not cached: tests point it elsewhere per test.
func AppDataDir() string {
	resolve()
	if rootDir != "" {
		return filepath.Join(rootDir, "data")
	}
	return LegacyDataDir()
}

// LegacyDataDir is %LOCALAPPDATA%\DEOMinutes, used before the app went portable.
func LegacyDataDir() string {
	base, err := os.UserCacheDir() // %LocalAppData% on Windows
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, appDirName)
}

// ModelsDir is where the transcription model is downloaded.
func ModelsDir() string {
	resolve()
	if rootDir != "" {
		return filepath.Join(rootDir, "models")
	}
	return filepath.Join(LegacyDataDir(), "models")
}

// Migration reports what was moved from LegacyDataDir on this start, for
// the log (which is not open yet when the move happens).
func Migration() ([]string, error) {
	resolve()
	return migrated, migrateErr
}

func LogsDir() string    { return filepath.Join(AppDataDir(), "logs") }
func BrowserDir() string { return filepath.Join(AppDataDir(), "browser") }
func WebviewDir() string { return filepath.Join(AppDataDir(), "webview") }
func SessionsDir() string {
	return filepath.Join(AppDataDir(), "sessions")
}

func writable(dir string) bool {
	if os.MkdirAll(dir, 0o700) != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name) == nil
}

// migrate moves the user's data from the legacy folder into the portable
// layout the first time the app starts from a new place: config, sessions,
// logs and selftest results into root\data, a downloaded model into
// root\models. Nothing is overwritten. Folders on another drive cannot be
// renamed; config.json is copied then, the rest stays where it was (the
// model is still found there, see LegacyDataDir).
func migrate(legacy, root string) ([]string, error) {
	if _, err := os.Stat(legacy); err != nil {
		return nil, nil
	}
	data := filepath.Join(root, "data")
	if _, err := os.Stat(filepath.Join(data, "config.json")); err == nil {
		return nil, nil // already set up here
	}
	if err := os.MkdirAll(data, 0o700); err != nil {
		return nil, err
	}
	moves := []struct{ from, to string }{
		{"config.json", filepath.Join(data, "config.json")},
		{"sessions", filepath.Join(data, "sessions")},
		{"logs", filepath.Join(data, "logs")},
		{"selftest", filepath.Join(data, "selftest")},
		{"models", filepath.Join(root, "models")},
	}
	var done []string
	var errs []error
	for _, m := range moves {
		src := filepath.Join(legacy, m.from)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if _, err := os.Stat(m.to); err == nil {
			if m.from != "models" {
				continue
			}
			// The zip may ship a models folder; move the files inside.
			if err := moveChildren(src, m.to); err != nil {
				errs = append(errs, err)
			} else {
				done = append(done, m.from)
			}
			continue
		}
		err := os.Rename(src, m.to)
		if err != nil && m.from == "config.json" {
			err = copyFile(src, m.to)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		done = append(done, m.from)
	}
	return done, errors.Join(errs...)
}

func moveChildren(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		to := filepath.Join(dst, e.Name())
		if _, err := os.Stat(to); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(src, e.Name()), to); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
