package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
)

// browserCandidates lists where the bot looks for a browser, Chrome first
// (installed for everyone, for the user only, or portable Chrome for Testing
// in the app folder), Edge only as the last resort. None needs admin rights.
func browserCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		pf86, pf, local := os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")
		return []string{
			filepath.Join(pf, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(pf86, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(local, `Google\Chrome\Application\chrome.exe`),
			filepath.Join(config.AppDataDir(), `chrome\chrome.exe`), // Chrome for Testing
			filepath.Join(pf86, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(pf, `Microsoft\Edge\Application\msedge.exe`),
			filepath.Join(local, `Microsoft\Edge\Application\msedge.exe`),
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	default:
		return []string{"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/microsoft-edge"}
	}
}

// FindBrowser returns override if set, else the first installed candidate.
func FindBrowser(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", i18n.Errorf(err, "err.browser.notFound")
		}
		return override, nil
	}
	for _, p := range browserCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", i18n.Errorf(nil, "err.browser.notFound")
}

// Browser is one bot browser process with a throwaway profile.
type Browser struct {
	Ctx     context.Context
	cancel  func()
	profile string
}

// transports are tried in order. The pipe is chromedp's default; some
// browser builds on Windows reject its handle switch (--remote-debugging-io-pipes) and
// quit without output, so the classic debugging port (127.0.0.1, random port)
// is the fallback. (var so tests can force one)
var transports = []struct {
	name string
	opts []chromedp.ExecAllocatorOption
}{
	{"pipe", nil},
	{"port", []chromedp.ExecAllocatorOption{remote.WebSocket}},
}

// Launch starts the browser. GPU is always off (constraint C7); the fake
// microphone plays silence; audio output is muted (capture taps WebAudio
// before the speakers). A non-headless window sits off-screen unless
// onScreen (selftest --visible).
func Launch(parent context.Context, path string, headless, onScreen bool) (*Browser, error) {
	slog.Info("browser", "path", path, "elevated", Elevated())
	var err error
	for _, tr := range transports {
		var b *Browser
		if b, err = launch(parent, path, headless, onScreen, tr.opts); err == nil {
			return b, nil
		}
		slog.Warn("browser start failed", "transport", tr.name, "err", err)
		if parent.Err() != nil {
			break
		}
	}
	if Elevated() {
		return nil, i18n.Errorf(err, "err.browser.elevated")
	}
	return nil, i18n.Errorf(err, "err.browser.blocked")
}

func launch(parent context.Context, path string, headless, onScreen bool, extra []chromedp.ExecAllocatorOption) (*Browser, error) {
	profile := filepath.Join(config.BrowserDir(), fmt.Sprintf("s-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return nil, err
	}
	silence := filepath.Join(profile, "silence.wav")
	if err := audio.SilenceWAV(silence, 2); err != nil {
		return nil, err
	}
	opts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(path),
		chromedp.UserDataDir(profile),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-gpu-compositing", true),
		chromedp.Flag("use-fake-ui-for-media-stream", true),
		chromedp.Flag("use-fake-device-for-media-stream", true),
		chromedp.Flag("use-file-for-fake-audio-capture", silence),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-features", "CalculateNativeWinOcclusion,Translate"),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-service-autorun", true),
		chromedp.Flag("password-store", "basic"),
		// "Join from browser" may open the client in a new tab; a script click
		// is no user gesture, so the popup blocker would swallow it.
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.WindowSize(1280, 800),
	}
	if headless {
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else if !onScreen {
		opts = append(opts, chromedp.Flag("window-position", "-32000,-32000"))
	}
	opts = append(opts, extra...)
	actx, acancel := chromedp.NewExecAllocator(parent, opts...)
	ctx, cancel := chromedp.NewContext(actx)
	if err := chromedp.Do(ctx); err != nil { // starts the process
		cancel()
		acancel()
		_ = os.RemoveAll(profile)
		return nil, err
	}
	return &Browser{Ctx: ctx, cancel: func() { cancel(); acancel() }, profile: profile}, nil
}

// Close stops the browser and deletes its profile (cookies, cache, media).
func (b *Browser) Close() error {
	b.cancel()
	var err error
	for i := 0; i < 10; i++ { // the process may hold files for a moment on Windows
		if err = os.RemoveAll(b.profile); err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return err
}
