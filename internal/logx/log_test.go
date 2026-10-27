package logx

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, 0)
	l.Info("join",
		"link", "https://firma.webex.com/firma/j.php?MTID=m1234abcd&pwd=secret",
		"api_key", "sk-very-secret",
		"token", "abc",
		"transcript", "Ion: buget aprobat",
		"err", errors.New("auth failed for AKIAABCDEFGHIJKLMNOP with Bearer eyJhbGciOi.x.y"),
		"count", 3,
	)
	out := buf.String()
	for _, bad := range []string{"MTID=", "m1234abcd", "sk-very-secret", "\"abc\"", "buget aprobat", "AKIAABCDEFGHIJKLMNOP", "eyJhbGciOi"} {
		if strings.Contains(out, bad) {
			t.Errorf("leaked %q in %s", bad, out)
		}
	}
	if !strings.Contains(out, "firma.webex.com") || !strings.Contains(out, `"count":3`) {
		t.Errorf("over-redacted: %s", out)
	}
}

func TestURL(t *testing.T) {
	if got := URL("https://u:p@x.webex.com/meet/ana?x=1#f"); got != "https://x.webex.com/meet/ana?…" {
		t.Fatalf("got %q", got)
	}
	if got := URL("not a url"); got != redacted {
		t.Fatalf("got %q", got)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	r, err := openRotating(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 7; i++ {
		if _, err := r.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(r.path(1)); err != nil {
		t.Fatal("expected rotated file")
	}
}
