package bot

import (
	"strings"
	"testing"
)

func TestParseLink(t *testing.T) {
	ok := []struct{ in, pwd string }{
		{"https://firma.webex.com/meet/ana.pop", ""},
		{" https://firma.webex.com/firma/j.php?MTID=m123&pwd=Secr3t ", "Secr3t"},
		{"https://web.webex.com/meet?xyz", ""},
	}
	for _, c := range ok {
		l, err := ParseLink(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if l.Password != c.pwd {
			t.Errorf("%q: pwd %q", c.in, l.Password)
		}
	}
	bad := []string{
		"",
		"http://firma.webex.com/meet/a",
		"https://webex.com.evil.io/meet/a",
		"https://evilwebex.com/meet/a",
		"https://user:pw@firma.webex.com/meet/a",
		"https://firma.webex.com:8443/meet/a",
		"javascript:alert(1)",
		"file:///C:/x",
	}
	for _, in := range bad {
		if _, err := ParseLink(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestBrowserCandidatesPreferChrome(t *testing.T) {
	firstEdge, lastChrome := -1, -1
	for i, p := range browserCandidates() {
		lp := strings.ToLower(p)
		if strings.Contains(lp, "edge") && firstEdge < 0 {
			firstEdge = i
		}
		if strings.Contains(lp, "chrome") && !strings.Contains(lp, "edge") {
			lastChrome = i
		}
	}
	if firstEdge >= 0 && lastChrome > firstEdge {
		t.Fatalf("Chrome must come before Edge: %v", browserCandidates())
	}
}
