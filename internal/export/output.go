package export

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// BaseName builds "YYMMDD_<Proiect>_Minuta_<Nr>", matching the user's
// "260407_Project workbook_MASTER" naming.
func BaseName(date time.Time, project string, no int) string {
	return fmt.Sprintf("%s_%s_Minuta_%d", date.Format("060102"), safeName(project), no)
}

// safeName keeps a project name usable as a Windows file name.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case strings.ContainsRune(`<>:"/\|?*`, r), unicode.IsControl(r):
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, ". ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	if s == "" {
		s = "Proiect"
	}
	return s
}

// UniqueBase returns base, or base_v2, base_v3… so that no file starting with
// it already exists in dir. Existing minutes are never overwritten.
func UniqueBase(dir, base string) (string, error) {
	for v := 1; v < 1000; v++ {
		cand := base
		if v > 1 {
			cand = fmt.Sprintf("%s_v%d", base, v)
		}
		_, err := os.Stat(filepath.Join(dir, cand+".xlsx"))
		if errors.Is(err, fs.ErrNotExist) {
			return cand, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("prea multe versiuni ale aceluiași fișier")
}

// WriteFileAtomic writes data next to path and renames it into place.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".partial"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
