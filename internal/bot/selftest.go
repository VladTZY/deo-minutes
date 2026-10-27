package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"deo-minutes/internal/audio"
)

// SelftestReport summarizes a --selftest run (PLAN.md S1).
type SelftestReport struct {
	Started      time.Time           `json:"started"`
	Browser      string              `json:"browser"`
	PageStates   []string            `json:"page_states"`
	JoinError    string              `json:"join_error,omitempty"`
	AudioChunks  int                 `json:"audio_chunks"`
	LoudChunks   int                 `json:"loud_chunks"`
	Participants []string            `json:"participants"`
	SpeakersSeen map[string]int      `json:"speakers_seen"` // name → snapshots marked speaking
	PanelFound   int                 `json:"panel_found_snapshots"`
	Snapshots    int                 `json:"roster_snapshots"`
	EndReason    string              `json:"end_reason,omitempty"`
	UnknownPages map[string][]string `json:"unknown_page_controls,omitempty"`
}

// Selftest joins the meeting, records for d, and writes screenshots,
// audio.wav, roster.jsonl and report.json into dir. It never sends anything
// anywhere; everything stays in dir.
func Selftest(ctx context.Context, opts Options, dir string, d time.Duration) (*SelftestReport, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	rep := &SelftestReport{Started: time.Now(), SpeakersSeen: map[string]int{}, UnknownPages: map[string][]string{}}
	rep.Browser, _ = FindBrowser(opts.BrowserPath)

	var mu sync.Mutex
	var s *Session
	step := 0
	shot := func(name string) {
		if s == nil {
			return
		}
		if png, err := s.Screenshot(); err == nil {
			step++
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%02d_%s.jpg", step, name)), png, 0o600)
		}
	}
	opts.OnPageState = func(st string) {
		mu.Lock()
		rep.PageStates = append(rep.PageStates, time.Now().Format("15:04:05")+" "+st)
		mu.Unlock()
		shot(st)
		if st == "unknown" || st == "loading" {
			if labels, err := eval[[]string](s, controlsJS); err == nil {
				mu.Lock()
				rep.UnknownPages[time.Now().Format("15:04:05")] = labels
				mu.Unlock()
			}
		}
	}

	var err error
	s, err = Start(ctx, opts)
	if err != nil {
		return rep, err
	}
	defer func() { _ = s.Close() }()
	if err := s.JoinMeeting(ctx); err != nil {
		rep.JoinError = err.Error()
		shot("join_error")
		return rep, writeReport(dir, rep)
	}
	shot("in_meeting")

	wav, err := audio.CreateWAV(filepath.Join(dir, "audio.wav"))
	if err != nil {
		return rep, err
	}
	defer func() { _ = wav.Close() }()
	rf, err := os.Create(filepath.Join(dir, "roster.jsonl"))
	if err != nil {
		return rep, err
	}
	defer func() { _ = rf.Close() }()
	enc := json.NewEncoder(rf)
	names := map[string]bool{}

	timer := time.NewTimer(d)
	defer timer.Stop()
	shotTick := time.NewTicker(time.Minute)
	defer shotTick.Stop()
loop:
	for {
		select {
		case c := <-s.Audio():
			rep.AudioChunks++
			if audio.RMS(c.PCM) > 0.01 {
				rep.LoudChunks++
			}
			_, _ = wav.Write(c.PCM)
		case r := <-s.RosterUpdates():
			rep.Snapshots++
			if r.Panel {
				rep.PanelFound++
			}
			for _, n := range r.Participants {
				names[n] = true
			}
			for _, n := range r.Speakers {
				rep.SpeakersSeen[n]++
			}
			_ = enc.Encode(struct {
				TS string `json:"ts"`
				Roster
			}{r.TS.Format("15:04:05.000"), r})
		case <-shotTick.C:
			shot("meeting")
		case <-s.Done():
			if s.Err() != nil {
				rep.EndReason = s.Err().Error()
			}
			break loop
		case <-timer.C:
			rep.EndReason = "selftest duration reached"
			shot("before_leave")
			_ = s.Leave()
			break loop
		case <-ctx.Done():
			break loop
		}
	}
	for n := range names {
		rep.Participants = append(rep.Participants, n)
	}
	sort.Strings(rep.Participants)
	return rep, writeReport(dir, rep)
}

// controlsJS lists the labels of visible buttons, links and inputs, so an
// unrecognized Webex page can be mapped to selectors without dumping the page.
const controlsJS = `(() => { const P = window.__deoPage; if (!P) return [];
  return P.allElements().filter((el) => P.visible(el) && (["BUTTON","A","INPUT","TEXTAREA"].includes(el.tagName) || el.getAttribute("role") === "button"))
    .map((el) => el.tagName.toLowerCase() + ": " + P.labelOf(el).slice(0, 120)).slice(0, 80); })()`

func writeReport(dir string, rep *SelftestReport) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), b, 0o600); err != nil {
		return err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "DEO Minute – test Webex (%s)\r\n\r\n", rep.Started.Format("2006-01-02 15:04"))
	fmt.Fprintf(&sb, "Browser: %s\r\n", rep.Browser)
	fmt.Fprintf(&sb, "Pași: %s\r\n", strings.Join(rep.PageStates, " → "))
	if rep.JoinError != "" {
		fmt.Fprintf(&sb, "EROARE la intrare: %s\r\n", rep.JoinError)
	}
	fmt.Fprintf(&sb, "Audio: %d secunde, %d cu sunet\r\n", rep.AudioChunks, rep.LoudChunks)
	fmt.Fprintf(&sb, "Participanți găsiți: %d (panou găsit în %d/%d citiri)\r\n", len(rep.Participants), rep.PanelFound, rep.Snapshots)
	fmt.Fprintf(&sb, "Vorbitori detectați: %d\r\n", len(rep.SpeakersSeen))
	fmt.Fprintf(&sb, "Final: %s\r\n", rep.EndReason)
	return os.WriteFile(filepath.Join(dir, "raport.txt"), []byte(sb.String()), 0o600)
}
