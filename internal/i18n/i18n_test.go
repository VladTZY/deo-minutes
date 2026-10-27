package i18n

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestT(t *testing.T) {
	if got := T("err.webex.lobbyTimeout", 15); !strings.Contains(got, "15 minute") {
		t.Fatalf("got %q", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Fatalf("got %q", got)
	}
}

func TestUserError(t *testing.T) {
	cause := errors.New("boom")
	err := Errorf(cause, "err.excel.write", "disc plin")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "disc plin") {
		t.Fatalf("bad error %v", err)
	}
}

// Every key referenced from Go (i18n.T / i18n.Errorf / Key: "…") and from the
// frontend (t("…")) must exist in ro.json.
func TestAllReferencedKeysExist(t *testing.T) {
	root := filepath.Join("..", "..")
	goRe := regexp.MustCompile(`(?:i18n\.T\(|i18n\.Errorf\([^,]+,\s*|Key:\s*)"([a-zA-Z][\w.]+)"`)
	tsRe := regexp.MustCompile(`\bt\(\s*["']([a-zA-Z][\w.]+)["']`)
	var missing []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", ".git", "wailsjs", "build", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		var re *regexp.Regexp
		switch {
		case strings.HasSuffix(p, ".go"):
			re = goRe
		case strings.HasSuffix(p, ".tsx"), strings.HasSuffix(p, ".ts"):
			re = tsRe
		default:
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			// A key ending in "." is a prefix completed at run time ("state." + s).
			if !Has(m[1]) && !strings.HasSuffix(m[1], ".") && !strings.HasSuffix(p, "_test.go") {
				missing = append(missing, p+": "+m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Fatalf("keys missing from ro.json:\n%s", strings.Join(missing, "\n"))
	}
}
