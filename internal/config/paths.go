package config

import (
	"os"
	"path/filepath"
)

const appDirName = "DEOMinutes"

// AppDataDir is %LOCALAPPDATA%\DEOMinutes on Windows (per-user, no admin needed).
func AppDataDir() string {
	base, err := os.UserCacheDir() // %LocalAppData% on Windows
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, appDirName)
}

func LogsDir() string    { return filepath.Join(AppDataDir(), "logs") }
func BrowserDir() string { return filepath.Join(AppDataDir(), "browser") }
func WebviewDir() string { return filepath.Join(AppDataDir(), "webview") }
func SessionsDir() string {
	return filepath.Join(AppDataDir(), "sessions")
}
