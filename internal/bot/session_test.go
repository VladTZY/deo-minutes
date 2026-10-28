package bot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/i18n"
)

// TestJoinFakeWebex drives a real Chrome/Edge through a fake Webex page:
// landing → name/email → password → preview → lobby → meeting → ended.
// Skipped when no browser is installed or with -short.
func TestJoinFakeWebex(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a browser")
	}
	if _, err := FindBrowser(""); err != nil {
		t.Skip("no Edge/Chrome installed")
	}
	html, err := os.ReadFile("testdata/fakewebex.html")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	}))
	defer ts.Close()

	var mu sync.Mutex
	var states []State
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Link:         MeetingLink{URL: ts.URL + "/?endAfter=7000", Password: "Parola123"},
		DisplayName:  "Asistent Minute",
		Email:        "bot@example.com",
		Headless:     true,
		Selectors:    DefaultSelectors,
		AllowedHosts: []string{"127.0.0.1"},
		JoinTimeout:  45 * time.Second,
		OnState: func(st State) {
			mu.Lock()
			states = append(states, st)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	defer func() { _ = s.Close() }()

	entered, err := eval[map[string]any](s, "window.__entered")
	if err != nil {
		t.Fatal(err)
	}
	if entered["name"] != "Asistent Minute" || entered["email"] != "bot@example.com" || entered["muted"] != true || entered["videoOff"] != true {
		t.Errorf("join form not filled as expected: %v", entered)
	}

	var loud, chunks int
	var roster Roster
	spoke := map[string]bool{}
loop:
	for {
		select {
		case c := <-s.Audio():
			chunks++
			if len(c.PCM) != audio.BytesPerSecond {
				t.Errorf("chunk size %d", len(c.PCM))
			}
			if audio.RMS(c.PCM) > 0.05 {
				loud++
			}
			if d := time.Since(c.TS); d < 0 || d > 10*time.Second {
				t.Errorf("chunk timestamp off by %v", d)
			}
		case r := <-s.RosterUpdates():
			roster = r
			for _, n := range r.Speakers {
				spoke[n] = true
			}
		case <-s.Done():
			break loop
		case <-ctx.Done():
			t.Fatal("meeting never ended")
		}
	}

	if loud < 3 {
		t.Errorf("captured %d chunks, %d with the tone; want ≥3 loud", chunks, loud)
	}
	if !slices.Contains(roster.Participants, "Ana Pop") || !slices.Contains(roster.Participants, "Ion Ionescu") {
		t.Errorf("participants = %v", roster.Participants)
	}
	if len(spoke) != 1 || !spoke["Ana Pop"] {
		t.Errorf("speakers seen = %v", spoke)
	}
	var ue *i18n.UserError
	if err := s.Err(); !errors.Is(err, ErrEnded) || !errors.As(err, &ue) {
		t.Errorf("end reason = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []State{StateStarting, StateConnecting, StateLobby, StateInMeeting, StateEnded}
	if !slices.Equal(states, want) {
		t.Errorf("states = %v, want %v", states, want)
	}
}

func TestJoinWrongPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a browser")
	}
	if _, err := FindBrowser(""); err != nil {
		t.Skip("no Edge/Chrome installed")
	}
	html, _ := os.ReadFile("testdata/fakewebex.html")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(html) }))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := Join(ctx, Options{
		Link: MeetingLink{URL: ts.URL}, Password: "gresit", DisplayName: "Bot", Headless: true,
		Selectors: DefaultSelectors, JoinTimeout: 40 * time.Second, AllowedHosts: []string{"127.0.0.1"},
	})
	var ue *i18n.UserError
	if !errors.As(err, &ue) || ue.Key != "err.webex.wrongPassword" {
		t.Fatalf("want wrong password error, got %v", err)
	}
}

func TestSelftestFakeWebex(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a browser")
	}
	if _, err := FindBrowser(""); err != nil {
		t.Skip("no Edge/Chrome installed")
	}
	html, _ := os.ReadFile("testdata/fakewebex.html")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(html) }))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	rep, err := Selftest(ctx, Options{
		Link: MeetingLink{URL: ts.URL + "/?endAfter=60000", Password: "Parola123"}, DisplayName: "Bot",
		Headless: true, Selectors: DefaultSelectors, JoinTimeout: 40 * time.Second, AllowedHosts: []string{"127.0.0.1"},
	}, dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if rep.LoudChunks < 2 || rep.SpeakersSeen["Ana Pop"] == 0 || rep.JoinError != "" {
		t.Errorf("report: %+v", rep)
	}
	for _, f := range []string{"audio.wav", "roster.jsonl", "report.json", "raport.txt"} {
		if _, err := os.Stat(dir + "/" + f); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

func TestJoinAbortsWhenPageLeavesAllowedHost(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a browser")
	}
	if _, err := FindBrowser(""); err != nil {
		t.Skip("no Edge/Chrome installed")
	}
	html, _ := os.ReadFile("testdata/fakewebex.html")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(html) }))
	defer ts.Close()
	evil := strings.Replace(ts.URL, "127.0.0.1", "localhost", 1) + "/phish"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := Join(ctx, Options{
		Link: MeetingLink{URL: ts.URL + "/?evil=" + url.QueryEscape(evil)}, Password: "Parola123", DisplayName: "Bot",
		Headless: true, Selectors: DefaultSelectors, JoinTimeout: 40 * time.Second, AllowedHosts: []string{"127.0.0.1"},
	})
	var ue *i18n.UserError
	if !errors.As(err, &ue) || ue.Key != "err.webex.join" || !strings.Contains(err.Error(), "webex.com") {
		t.Fatalf("want abort when leaving the host, got %v", err)
	}
}

func TestBindingLimits(t *testing.T) {
	s := &Session{audio: make(chan AudioChunk, 100), roster: make(chan Roster, 10)}
	good := `{"ts":1,"pcm":"` + base64.StdEncoding.EncodeToString(make([]byte, 32000)) + `"}`
	short := `{"ts":1,"pcm":"` + base64.StdEncoding.EncodeToString(make([]byte, 31998)) + `"}`

	s.onEvent(cdpruntime.EventBindingCalled{Name: "deoAudio", Payload: good})
	if len(s.audio) != 0 {
		t.Fatal("data must be ignored until the page is verified on an allowed host")
	}
	s.onWebex.Store(true)
	s.onEvent(cdpruntime.EventBindingCalled{Name: "deoAudio", Payload: short})
	if len(s.audio) != 0 {
		t.Fatal("chunks of the wrong size must be dropped")
	}
	for i := 0; i < 50; i++ { // a burst far above real time
		s.onEvent(cdpruntime.EventBindingCalled{Name: "deoAudio", Payload: good})
	}
	if n := len(s.audio); n < 1 || n > 7 {
		t.Fatalf("rate limit let %d chunks through instantly", n)
	}
	long := strings.Repeat("x", 500)
	names := make([]string, 300)
	for i := range names {
		names[i] = long
	}
	b, _ := json.Marshal(map[string]any{"ts": 1, "participants": names, "speakers": names})
	s.onEvent(cdpruntime.EventBindingCalled{Name: "deoRoster", Payload: string(b)})
	r := <-s.roster
	if len(r.Participants) != maxParticipants || len([]rune(r.Participants[0])) != maxNameLen || len(r.Speakers) != 10 {
		t.Fatalf("roster not capped: %d names, %d chars", len(r.Participants), len(r.Participants[0]))
	}
}

// TestLaunchFallsBackToPort: when the pipe start fails (as with some Edge
// builds on Windows), Launch retries over the debugging port.
func TestLaunchFallsBackToPort(t *testing.T) {
	if testing.Short() {
		t.Skip("needs a browser")
	}
	path, err := FindBrowser("")
	if err != nil {
		t.Skip("no Edge/Chrome installed")
	}
	saved := transports
	defer func() { transports = saved }()
	transports = append(transports[:0:0], saved...)
	transports[0].opts = []chromedp.ExecAllocatorOption{chromedp.ExecPath(filepath.Join(t.TempDir(), "missing"))}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := Launch(ctx, path, true, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer func() { _ = b.Close() }()
	if got, err := chromedp.Run(b.Ctx, chromedp.Evaluate[int]("1+1")); err != nil || got != 2 {
		t.Fatalf("evaluate over port: %v %v", got, err)
	}
}

// fakeWebexServer serves testdata/fakewebex.html.
func fakeWebexServer(t *testing.T) *httptest.Server {
	t.Helper()
	html, err := os.ReadFile("testdata/fakewebex.html")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// joinVia serves outer (with %s replaced by the fake client URL) and checks
// that the bot gets into the meeting and fills the name.
func joinVia(t *testing.T, outer string) {
	if testing.Short() {
		t.Skip("needs a browser")
	}
	if _, err := FindBrowser(""); err != nil {
		t.Skip("no Edge/Chrome installed")
	}
	client := fakeWebexServer(t)
	page := strings.ReplaceAll(outer, "%s", client.URL+"/?endAfter=20000")
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer front.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{
		Link:         MeetingLink{URL: front.URL + "/", Password: "Parola123"},
		DisplayName:  "Asistent Minute",
		Headless:     true,
		Selectors:    DefaultSelectors,
		AllowedHosts: []string{"127.0.0.1"},
		JoinTimeout:  45 * time.Second,
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	defer func() { _ = s.Close() }()
	entered, err := evalIn[map[string]any](s, s.activeContext(), "window.__entered")
	if err != nil {
		t.Fatal(err)
	}
	if entered["name"] != "Asistent Minute" {
		t.Errorf("name not filled: %v", entered)
	}
}

// TestJoinClientInCrossOriginIframe: the web client sits in an iframe from
// another origin of the same site (as another webex.com subdomain would).
func TestJoinClientInCrossOriginIframe(t *testing.T) {
	joinVia(t, `<!doctype html><title>Meeting</title>
		<iframe src="%s" style="width:1200px;height:700px" allow="microphone; camera; autoplay"></iframe>`)
}

// TestJoinClientInNewTab: "Join from browser" opens the client in a new tab.
func TestJoinClientInNewTab(t *testing.T) {
	joinVia(t, `<!doctype html><title>Meeting</title>
		<a href="%s" target="_blank">Join from your browser</a>`)
}
