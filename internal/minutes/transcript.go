package minutes

import (
	"fmt"
	"strings"
	"time"
)

// Line is one speaker turn, At measured from the meeting start.
type Line struct {
	At      time.Duration
	Speaker string
	Text    string
}

// Transcript is the whole meeting, in order.
type Transcript []Line

// Clock formats a duration as HH:MM:SS.
func Clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// ParseClock reads HH:MM:SS (or MM:SS).
func ParseClock(s string) (time.Duration, bool) {
	var h, m, sec int
	if n, _ := fmt.Sscanf(strings.TrimSpace(s), "%d:%d:%d", &h, &m, &sec); n == 3 {
		return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, true
	}
	if n, _ := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &m, &sec); n == 2 {
		return time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, true
	}
	return 0, false
}

func (l Line) String() string { return fmt.Sprintf("[%s] %s: %s", Clock(l.At), l.Speaker, l.Text) }

// Render writes the transcript as "[HH:MM:SS] Nume: text" lines.
func (t Transcript) Render() string {
	var sb strings.Builder
	for _, l := range t {
		sb.WriteString(l.String())
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Duration is the time of the last line.
func (t Transcript) Duration() time.Duration {
	if len(t) == 0 {
		return 0
	}
	return t[len(t)-1].At
}

// estimateTokens is a cheap upper-bound estimate (Romanian ≈ 3 chars/token).
func estimateTokens(s string) int { return len([]rune(s)) / 3 }

// Windows splits the transcript into consecutive windows of about size,
// each overlapping the previous one by overlap.
func (t Transcript) Windows(size, overlap time.Duration) []Transcript {
	if len(t) == 0 {
		return nil
	}
	var out []Transcript
	for start := time.Duration(0); ; start += size - overlap {
		var w Transcript
		for _, l := range t {
			if l.At >= start && l.At < start+size {
				w = append(w, l)
			}
		}
		if len(w) > 0 {
			out = append(out, w)
		}
		if start+size > t.Duration() {
			return out
		}
	}
}
