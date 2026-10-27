package bot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

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
		Link:        MeetingLink{URL: ts.URL + "/?endAfter=7000", Password: "Parola123"},
		DisplayName: "Asistent Minute",
		Email:       "bot@example.com",
		Headless:    true,
		Selectors:   DefaultSelectors,
		JoinTimeout: 45 * time.Second,
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
		Selectors: DefaultSelectors, JoinTimeout: 40 * time.Second,
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
		Headless: true, Selectors: DefaultSelectors, JoinTimeout: 40 * time.Second,
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
