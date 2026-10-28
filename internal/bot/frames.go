package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"deo-minutes/internal/logx"
)

// The Webex web client can live in an iframe from another webex.com
// subdomain, where the main page's scripts cannot reach. page.js, capture.js
// and roster.js are injected into every frame, so the bot evaluates in the
// main world of each child frame on an allowed host and acts in the one that
// shows the most advanced join state. Context 0 is the main frame.

// trackFrames records the main-world execution contexts of child frames on
// allowed hosts. Called from prepare, before the first navigation.
func (s *Session) trackFrames(ctx context.Context) {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Target == nil {
		return
	}
	mainFrame := string(c.Target.TargetID) // a page's main frame id is its target id
	s.frames = map[string]cdpruntime.ExecutionContextID{}
	created := chromedp.Events(ctx, cdpruntime.ExecutionContextCreated)
	destroyed := chromedp.Events(ctx, cdpruntime.ExecutionContextDestroyed)
	cleared := chromedp.Events(ctx, cdpruntime.ExecutionContextsCleared)
	go func() {
		for ev, err := range created {
			if err != nil || ev.Context == nil {
				return
			}
			var aux struct {
				IsDefault bool   `json:"isDefault"`
				FrameID   string `json:"frameId"`
			}
			if json.Unmarshal([]byte(ev.Context.AuxData), &aux) != nil || !aux.IsDefault || aux.FrameID == mainFrame {
				continue
			}
			if _, ok := s.allowedURL(ev.Context.Origin); !ok {
				continue
			}
			s.framesMu.Lock()
			s.frames[ev.Context.UniqueID] = ev.Context.ID
			s.framesMu.Unlock()
		}
	}()
	go func() {
		for ev, err := range destroyed {
			if err != nil {
				return
			}
			s.framesMu.Lock()
			id, ok := s.frames[ev.ExecutionContextUniqueID]
			delete(s.frames, ev.ExecutionContextUniqueID)
			if ok && s.active == id {
				s.active = 0
			}
			s.framesMu.Unlock()
		}
	}()
	go func() {
		for _, err := range cleared {
			if err != nil {
				return
			}
			s.framesMu.Lock()
			clear(s.frames)
			s.active = 0
			s.framesMu.Unlock()
		}
	}()
}

// contexts returns the main frame (0) followed by the tracked child frames.
func (s *Session) contexts() []cdpruntime.ExecutionContextID {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	out := []cdpruntime.ExecutionContextID{0}
	for _, id := range s.frames {
		out = append(out, id)
	}
	slices.Sort(out[1:])
	return out
}

func (s *Session) activeContext() cdpruntime.ExecutionContextID {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	return s.active
}

// evalIn runs js in the main world of context id (0 = main frame).
func evalIn[T any](s *Session, id cdpruntime.ExecutionContextID, js string) (T, error) {
	if id == 0 {
		return eval[T](s, js)
	}
	ctx, cancel := context.WithTimeout(s.b.Ctx, 10*time.Second)
	defer cancel()
	return chromedp.Run(ctx, chromedp.Evaluate[T](js, func(p *cdpruntime.EvaluateParams) { p.ContextID = id }))
}

// stateRank orders join states from most to least decisive.
var stateRank = []string{"removed", "ended", "wrong_password", "in_meeting", "lobby", "password", "prejoin", "landing", "preview", "unknown", "loading"}

func rank(st string) int {
	if i := slices.Index(stateRank, st); i >= 0 {
		return i
	}
	return len(stateRank)
}

// pageState is the most decisive state over all frames; the frame that shows
// it becomes the one the bot acts in.
func (s *Session) pageState() string {
	js := fmt.Sprintf("window.__deoPage ? window.__deoPage.state(%s) : 'loading'", mustJSON(s.opts.Selectors))
	best, bestCtx := "loading", cdpruntime.ExecutionContextID(0)
	for _, id := range s.contexts() {
		st, err := evalIn[string](s, id, js)
		if err != nil {
			continue
		}
		if rank(st) < rank(best) {
			best, bestCtx = st, id
		}
	}
	s.framesMu.Lock()
	if s.active != bestCtx {
		slog.Info("webex frame", "child", bestCtx != 0)
	}
	s.active = bestCtx
	s.framesMu.Unlock()
	return best
}

// inFrames runs js in the active frame, then in the main frame, until one
// returns true.
func (s *Session) inFrames(js string) bool {
	for _, id := range slices.Compact([]cdpruntime.ExecutionContextID{s.activeContext(), 0}) {
		if ok, _ := evalIn[bool](s, id, js); ok {
			return true
		}
	}
	return false
}

// allowedURL reports whether raw (a URL or an origin) is on an allowed host.
func (s *Session) allowedURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
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

// watchNewTabs: when the page opens the web client in a new tab, the bot
// closes that tab and loads its address in its own tab, where the capture
// scripts and bindings already are. Only allowed hosts are followed.
func (s *Session) watchNewTabs(ctx context.Context) <-chan string {
	out := make(chan string, 1)
	c := chromedp.FromContext(s.b.Ctx)
	if c == nil || c.Target == nil {
		close(out)
		return out
	}
	self := c.Target.TargetID
	go func() {
		defer close(out)
		seen := map[target.ID]bool{}
		var lastURL string
		var lastAt time.Time
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			infos, err := chromedp.Targets(s.b.Ctx)
			if err != nil {
				continue
			}
			for _, i := range infos {
				if i.Type != "page" || i.OpenerID != self || seen[i.TargetID] || i.URL == "" || i.URL == "about:blank" {
					continue
				}
				seen[i.TargetID] = true
				if _, err := chromedp.CallBrowser(s.b.Ctx, target.CloseTarget, target.CloseTargetParams{TargetID: i.TargetID}); err != nil {
					slog.Warn("close new tab", "err", err)
				}
				if _, allowed := s.allowedURL(i.URL); !allowed {
					slog.Warn("new tab not followed", "url", logx.URL(i.URL))
					continue
				}
				if i.URL == lastURL && time.Since(lastAt) < 10*time.Second {
					continue // a second click on the same link
				}
				lastURL, lastAt = i.URL, time.Now()
				slog.Info("following new tab", "url", logx.URL(i.URL))
				select {
				case out <- i.URL:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}
