//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// DocumentsDir returns ~/Documents on non-Windows dev machines.
func DocumentsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents")
}
