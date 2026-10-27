// Package bot joins a Webex meeting as a guest through the Webex web client in
// a hidden Edge/Chrome, and streams the meeting audio and the participant /
// active-speaker roster back to Go.
package bot

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"deo-minutes/internal/i18n"
)

//go:embed js/page.js
var pageJS string

//go:embed js/capture.js
var captureJS string

//go:embed js/roster.js
var rosterJS string

// State is where the bot is in the join flow.
type State string

const (
	StateStarting   State = "starting_browser"
	StateConnecting State = "connecting"
	StateLobby      State = "lobby"
	StateInMeeting  State = "in_meeting"
	StateEnded      State = "ended"
)

// AudioChunk is ~1 s of 16 kHz mono s16le PCM; TS is when it started.
type AudioChunk struct {
	TS  time.Time
	PCM []byte
}

// Roster is one snapshot of who is in the meeting and who is talking.
type Roster struct {
	TS           time.Time `json:"-"`
	Participants []string  `json:"participants"`
	Speakers     []string  `json:"speakers"`
	Panel        bool      `json:"panel"`
}

// Options configure one bot session.
type Options struct {
	Link         MeetingLink
	Password     string // overrides Link.Password when set
	DisplayName  string
	Email        string
	BrowserPath  string
	Headless     bool
	LobbyTimeout time.Duration
	JoinTimeout  time.Duration // time allowed outside the lobby to get in
	Selectors    Selectors
	// AllowedHosts are the domains (and their subdomains) the bot may be on.
	// Empty means webex.com. Leaving them aborts the session.
	AllowedHosts []string
	OnState      func(State) // called on every state change
	OnPageState  func(string)
}

var (
	ErrEnded   = errors.New("meeting ended")
	ErrRemoved = errors.New("removed from meeting")
)

// Session is a running bot in a meeting.
type Session struct {
	b      *Browser
	opts   Options
	audio  chan AudioChunk
	roster chan Roster

	dropped atomic.Int64
	// onWebex is true while the main frame is on an allowed host; binding
	// data from any other page is ignored.
	onWebex  atomic.Bool
	firstPCM atomic.Int64 // unix ms of the first accepted chunk
	accepted atomic.Int64 // chunks accepted, for rate limiting
	events   atomic.Int64 // capture log lines written
	done     chan struct{}
	doneErr  error
	once     sync.Once
}

const (
	maxBindingPayload = 512 << 10
	audioBuffer       = 3600 // 1 h of chunks; the consumer writes to disk immediately
)

// Join launches the browser and walks the Webex join flow until the bot is in
// the meeting. The returned session keeps streaming until the meeting ends or
// Leave is called.
func Join(ctx context.Context, opts Options) (*Session, error) {
	s, err := Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	if err := s.JoinMeeting(ctx); err != nil {
		_ = s.b.Close()
		return nil, err
	}
	return s, nil
}

// Start launches the browser with the capture scripts installed.
func Start(ctx context.Context, opts Options) (*Session, error) {
	if opts.LobbyTimeout == 0 {
		opts.LobbyTimeout = 15 * time.Minute
	}
	if opts.JoinTimeout == 0 {
		opts.JoinTimeout = 3 * time.Minute
	}
	if opts.Password == "" {
		opts.Password = opts.Link.Password
	}
	notify(opts, StateStarting)
	path, err := FindBrowser(opts.BrowserPath)
	if err != nil {
		return nil, err
	}
	b, err := Launch(ctx, path, opts.Headless)
	if err != nil {
		return nil, err
	}
	s := &Session{
		b:      b,
		opts:   opts,
		audio:  make(chan AudioChunk, audioBuffer),
		roster: make(chan Roster, 64),
		done:   make(chan struct{}),
	}
	if err := s.prepare(); err != nil {
		_ = b.Close()
		return nil, i18n.Errorf(err, "err.browser.blocked")
	}
	return s, nil
}

// JoinMeeting runs the join flow, then starts the roster and end watcher.
func (s *Session) JoinMeeting(ctx context.Context) error {
	notify(s.opts, StateConnecting)
	if err := s.joinLoop(ctx); err != nil {
		return err
	}
	notify(s.opts, StateInMeeting)
	if _, err := eval[bool](s, fmt.Sprintf("window.__deoRosterStart(%s)", mustJSONString(s.opts.Selectors))); err != nil {
		slog.Warn("roster start", "err", err)
	}
	go s.watch(ctx)
	return nil
}

// Close stops the browser without leaving the meeting first.
func (s *Session) Close() error { return s.b.Close() }

func notify(o Options, st State) {
	if o.OnState != nil {
		o.OnState(st)
	}
}

func mustJSONString(v any) string {
	b, _ := json.Marshal(v)
	s, _ := json.Marshal(string(b))
	return string(s)
}

// prepare installs scripts and bindings before the first navigation and
// denies the camera so the bot never sends video.
func (s *Session) prepare() error {
	ctx := s.b.Ctx
	for _, name := range []string{"deoAudio", "deoRoster"} {
		if _, err := chromedp.Call(ctx, cdpruntime.AddBinding, cdpruntime.AddBindingParams{Name: name}); err != nil {
			return err
		}
	}
	for _, js := range []string{pageJS, captureJS, rosterJS} {
		if _, err := chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: js}); err != nil {
			return err
		}
	}
	if _, err := chromedp.CallBrowser(ctx, browser.SetPermission, browser.SetPermissionParams{
		Permission: &browser.PermissionDescriptor{Name: "camera"},
		Setting:    browser.PermissionSettingDenied,
	}); err != nil {
		slog.Warn("deny camera", "err", err) // not fatal: the join flow also clicks "stop video"
	}
	events := chromedp.Events(ctx, cdpruntime.BindingCalled)
	go func() {
		for ev, err := range events {
			if err != nil {
				return
			}
			s.onEvent(ev)
		}
	}()
	return nil
}

// Limits on what the page can push through the bindings.
const (
	chunkBytes      = 32000 // exactly 1 s of 16 kHz s16le, as capture.js sends
	maxRealtime     = 1.5   // accept at most 1.5 s of audio per wall-clock second
	maxNameLen      = 100
	maxParticipants = 200
	maxEventLogs    = 50
)

func (s *Session) onEvent(e cdpruntime.EventBindingCalled) {
	if len(e.Payload) > maxBindingPayload || !s.onWebex.Load() {
		return
	}
	switch e.Name {
	case "deoAudio":
		var m struct {
			TS      float64 `json:"ts"`
			PCM     string  `json:"pcm"`
			Event   string  `json:"event"`
			Message string  `json:"message"`
		}
		if json.Unmarshal([]byte(e.Payload), &m) != nil {
			return
		}
		if m.Event != "" {
			if s.events.Add(1) <= maxEventLogs {
				slog.Info("capture", "event", trunc(m.Event, 40), "detail", trunc(m.Message, 200))
			}
			return
		}
		pcm, err := base64.StdEncoding.DecodeString(m.PCM)
		if err != nil || len(pcm) != chunkBytes || !s.allowChunk() {
			return
		}
		select {
		case s.audio <- AudioChunk{TS: msToTime(m.TS), PCM: pcm}:
		default:
			if s.dropped.Add(1)%60 == 1 {
				slog.Error("audio buffer full, dropping chunks", "dropped", s.dropped.Load())
			}
		}
	case "deoRoster":
		var r Roster
		var ts struct {
			TS float64 `json:"ts"`
		}
		if json.Unmarshal([]byte(e.Payload), &r) != nil || json.Unmarshal([]byte(e.Payload), &ts) != nil {
			return
		}
		r.TS = msToTime(ts.TS)
		r.Participants = capNames(r.Participants, maxParticipants)
		r.Speakers = capNames(r.Speakers, 10)
		select {
		case s.roster <- r:
		default: // roster snapshots are frequent; a dropped one is harmless
		}
	}
}

// allowChunk rate-limits audio to maxRealtime × wall clock (plus a small burst).
func (s *Session) allowChunk() bool {
	now := time.Now().UnixMilli()
	s.firstPCM.CompareAndSwap(0, now)
	elapsed := float64(now-s.firstPCM.Load()) / 1000
	if float64(s.accepted.Load()) > elapsed*maxRealtime+5 {
		return false
	}
	s.accepted.Add(1)
	return true
}

func capNames(names []string, max int) []string {
	out := make([]string, 0, min(len(names), max))
	for _, n := range names {
		if len(out) == max {
			break
		}
		if n = trunc(n, maxNameLen); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// hostOK reports whether the main frame is on an allowed host.
func (s *Session) hostOK() (string, bool) {
	href, err := eval[string](s, "location.href")
	if err != nil {
		return "", false
	}
	u, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	allowed := s.opts.AllowedHosts
	if len(allowed) == 0 {
		allowed = []string{"webex.com"}
	}
	for _, a := range allowed {
		if u.Scheme == "https" || a != "webex.com" { // tests use http://127.0.0.1
			if host == a || strings.HasSuffix(host, "."+a) {
				return host, true
			}
		}
	}
	return host, false
}

// checkHost updates onWebex; a page that left the allowed hosts ends the session.
func (s *Session) checkHost() error {
	host, ok := s.hostOK()
	if ok {
		s.onWebex.Store(true)
		return nil
	}
	s.onWebex.Store(false)
	if host == "" { // still loading, or about:blank
		return nil
	}
	slog.Warn("bot left allowed hosts", "host", host)
	return i18n.Errorf(nil, "err.webex.join", "pagina a părăsit domeniul webex.com")
}

func msToTime(ms float64) time.Time {
	return time.UnixMilli(int64(ms))
}

// eval runs a JS expression in the page and decodes its result.
func eval[T any](s *Session, js string) (T, error) {
	ctx, cancel := context.WithTimeout(s.b.Ctx, 10*time.Second)
	defer cancel()
	return chromedp.Run(ctx, chromedp.Evaluate[T](js))
}

func (s *Session) pageState() string {
	st, err := eval[string](s, fmt.Sprintf("window.__deoPage ? window.__deoPage.state(%s) : 'loading'", mustJSON(s.opts.Selectors)))
	if err != nil {
		return "loading"
	}
	return st
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func (s *Session) click(needles []string) bool {
	ok, _ := eval[bool](s, fmt.Sprintf("window.__deoPage.click(%s)", mustJSON(needles)))
	return ok
}

func (s *Session) fill(needles []string, value string) bool {
	ok, _ := eval[bool](s, fmt.Sprintf("window.__deoPage.fill(%s, %s)", mustJSON(needles), mustJSON(value)))
	return ok
}

// clickUnless clicks the first match whose label contains none of skip
// (e.g. "mute" but not "unmute").
func (s *Session) clickUnless(needles, skip []string) bool {
	js := fmt.Sprintf(`(() => { const P = window.__deoPage; const el = P.find("clickable", %s);
		if (!el) return false; const l = P.labelOf(el); if (%s.some((x) => l.includes(x))) return false; el.click(); return true; })()`,
		mustJSON(needles), mustJSON(skip))
	ok, _ := eval[bool](s, js)
	return ok
}

func (s *Session) joinLoop(ctx context.Context) error {
	sel := s.opts.Selectors
	if err := chromedp.Do(s.b.Ctx, chromedp.Navigate(s.opts.Link.URL)); err != nil {
		return i18n.Errorf(err, "err.webex.join", "pagina nu s-a încărcat")
	}
	var (
		last          string
		lobbySince    time.Time
		activeElapsed time.Duration
		passwordTries int
	)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
		if err := s.checkHost(); err != nil {
			return err
		}
		if !s.onWebex.Load() {
			continue
		}
		st := s.pageState()
		if st != last {
			slog.Info("webex page state", "state", st)
			if s.opts.OnPageState != nil {
				s.opts.OnPageState(st)
			}
			last = st
		}
		if st != "lobby" {
			activeElapsed += time.Second
			if activeElapsed > s.opts.JoinTimeout {
				return i18n.Errorf(nil, "err.webex.join", "pagina Webex nu a fost recunoscută ("+st+")")
			}
		}
		switch st {
		case "removed":
			return i18n.Errorf(ErrRemoved, "err.webex.removed")
		case "ended":
			return i18n.Errorf(ErrEnded, "err.webex.ended")
		case "wrong_password":
			return i18n.Errorf(nil, "err.webex.wrongPassword")
		case "in_meeting":
			return nil
		case "lobby":
			if lobbySince.IsZero() {
				lobbySince = time.Now()
				notify(s.opts, StateLobby)
			}
			if time.Since(lobbySince) > s.opts.LobbyTimeout {
				return i18n.Errorf(nil, "err.webex.lobbyTimeout", int(s.opts.LobbyTimeout.Minutes()))
			}
		case "password":
			if s.opts.Password == "" {
				return i18n.Errorf(nil, "err.webex.passwordRequired")
			}
			if passwordTries++; passwordTries > 3 {
				return i18n.Errorf(nil, "err.webex.wrongPassword")
			}
			s.fill(sel.PasswordInput, s.opts.Password)
			_ = s.click(sel.Next) || s.click(sel.JoinButton)
		case "prejoin":
			s.fill(sel.NameInput, s.opts.DisplayName)
			if s.opts.Email != "" {
				s.fill(sel.EmailInput, s.opts.Email)
			}
			_ = s.click(sel.Next) || s.click(sel.JoinButton)
		case "preview":
			s.clickUnless(sel.MuteMic, []string{"unmute"})
			s.clickUnless(sel.StopVideo, []string{"start video", "turn on"})
			s.click(sel.JoinButton)
		case "landing":
			s.click(sel.JoinFromBrowser)
		default: // loading / unknown: a cookie banner is the usual blocker
			s.click(sel.CookieAccept)
		}
	}
}

// watch detects the end of the meeting.
func (s *Session) watch(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.finish(ctx.Err())
			return
		case <-s.b.Ctx.Done():
			s.finish(i18n.Errorf(ErrEnded, "err.webex.ended"))
			return
		case <-s.done:
			return
		case <-tick.C:
		}
		if err := s.checkHost(); err != nil {
			s.finish(err)
			return
		}
		switch s.pageState() {
		case "ended":
			s.finish(i18n.Errorf(ErrEnded, "err.webex.ended"))
			return
		case "removed":
			s.finish(i18n.Errorf(ErrRemoved, "err.webex.removed"))
			return
		}
	}
}

func (s *Session) finish(err error) {
	s.once.Do(func() {
		s.doneErr = err
		close(s.done)
		notify(s.opts, StateEnded)
	})
}

// Audio streams captured PCM chunks.
func (s *Session) Audio() <-chan AudioChunk { return s.audio }

// RosterUpdates streams participant / speaker snapshots.
func (s *Session) RosterUpdates() <-chan Roster { return s.roster }

// Done is closed when the meeting ends, the bot is removed, or Leave is called.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err tells why the session ended (nil after Leave).
func (s *Session) Err() error { return s.doneErr }

// Leave exits the meeting cleanly and closes the browser.
func (s *Session) Leave() error {
	if s.click(s.opts.Selectors.Leave) {
		time.Sleep(time.Second)
		s.click(s.opts.Selectors.LeaveConfirm)
		time.Sleep(time.Second)
	}
	s.finish(nil)
	return s.b.Close()
}

// Screenshot captures the page (used by --selftest).
func (s *Session) Screenshot() ([]byte, error) { return screenshot(s.b.Ctx) }

func screenshot(ctx context.Context) ([]byte, error) {
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return chromedp.Run(c, chromedp.FullScreenshot(70))
}
