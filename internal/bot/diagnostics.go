package bot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"deo-minutes/internal/config"
	"deo-minutes/internal/logx"
)

// keepDiagnostics is how many join-failure folders stay in the logs folder.
const keepDiagnostics = 5

// saveJoinDiagnostics writes what the bot saw when it could not join into
// logs\join-<time>: a screenshot, the visible controls and the frame tree
// (URLs without query, so no password). It stays on this computer.
func (s *Session) saveJoinDiagnostics(joinErr error) string {
	dir := filepath.Join(config.LogsDir(), "join-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	if img, err := s.Screenshot(); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "pagina.jpg"), img, 0o600)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "eroare: %v\r\nstare: %s\r\n\r\nframe-uri:\r\n", joinErr, s.pageState())
	ctx, cancel := context.WithTimeout(s.b.Ctx, 10*time.Second)
	defer cancel()
	if tree, err := chromedp.Call(ctx, page.GetFrameTree, cdp.Empty{}); err == nil {
		writeFrames(&b, tree.FrameTree, 0)
	}
	b.WriteString("\r\ncontroale vizibile:\r\n")
	if labels, err := eval[[]string](s, controlsJS); err == nil {
		for _, l := range labels {
			b.WriteString(l + "\r\n")
		}
	}
	_ = os.WriteFile(filepath.Join(dir, "raport.txt"), []byte(b.String()), 0o600)
	pruneDiagnostics()
	return dir
}

func writeFrames(b *strings.Builder, t *page.FrameTree, depth int) {
	if t == nil || t.Frame == nil {
		return
	}
	fmt.Fprintf(b, "%s%s\r\n", strings.Repeat("  ", depth), logx.URL(t.Frame.URL))
	for _, c := range t.ChildFrames {
		writeFrames(b, c, depth+1)
	}
}

func pruneDiagnostics() {
	dirs, _ := filepath.Glob(filepath.Join(config.LogsDir(), "join-*"))
	sort.Strings(dirs)
	for len(dirs) > keepDiagnostics {
		_ = os.RemoveAll(dirs[0])
		dirs = dirs[1:]
	}
}
