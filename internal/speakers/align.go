// Package speakers attributes transcribed words to the people Webex showed as
// speaking at that moment, and groups them into transcript lines.
package speakers

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"deo-minutes/internal/i18n"
	"deo-minutes/internal/stt"
)

// Timeline holds active-speaker snapshots from the bot (every ~500 ms).
type Timeline struct {
	// Lag is how far Webex's speaking indicator trails the voice; snapshots
	// are shifted back by it. Calibrate from --selftest data (PLAN.md S1).
	Lag time.Duration

	mu      sync.Mutex
	ts      []time.Time
	who     [][]string
	exclude map[string]bool
}

// NewTimeline ignores the given names (the bot itself).
func NewTimeline(exclude ...string) *Timeline {
	t := &Timeline{exclude: map[string]bool{}}
	for _, n := range exclude {
		t.exclude[norm(n)] = true
	}
	return t
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Add records who was speaking at ts. Snapshots may arrive slightly out of order.
func (t *Timeline) Add(ts time.Time, speaking []string) {
	var keep []string
	for _, n := range speaking {
		if n != "" && !t.exclude[norm(n)] {
			keep = append(keep, n)
		}
	}
	ts = ts.Add(-t.Lag)
	t.mu.Lock()
	defer t.mu.Unlock()
	i := sort.Search(len(t.ts), func(i int) bool { return t.ts[i].After(ts) })
	t.ts = append(t.ts, time.Time{})
	t.who = append(t.who, nil)
	copy(t.ts[i+1:], t.ts[i:])
	copy(t.who[i+1:], t.who[i:])
	t.ts[i], t.who[i] = ts, keep
}

const (
	window   = 500 * time.Millisecond // snapshot cadence slack around a word
	carry    = 3 * time.Second        // keep the last speaker through short silences of the indicator
	newLine  = 2 * time.Second        // a pause this long starts a new line
	maxChars = 600                    // keep lines readable
)

// SpeakerAt returns who spoke most during [start, end], or "".
func (t *Timeline) SpeakerAt(start, end time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	from := sort.Search(len(t.ts), func(i int) bool { return !t.ts[i].Before(start.Add(-window)) })
	votes := map[string]int{}
	order := []string{}
	for i := from; i < len(t.ts) && !t.ts[i].After(end.Add(window)); i++ {
		for _, n := range t.who[i] {
			if votes[n] == 0 {
				order = append(order, n)
			}
			votes[n]++
		}
	}
	best := ""
	for _, n := range order {
		if votes[n] > votes[best] {
			best = n
		}
	}
	if best != "" {
		return best
	}
	for i := from - 1; i >= 0 && start.Sub(t.ts[i]) <= carry; i-- {
		if len(t.who[i]) > 0 {
			return t.who[i][0]
		}
	}
	return ""
}

// Line is one speaker turn.
type Line struct {
	Start   time.Time
	End     time.Time
	Speaker string
	Text    string
}

// Format renders "[HH:MM:SS] Nume: text" relative to the meeting start.
func (l Line) Format(meetingStart time.Time) string {
	return fmt.Sprintf("[%s] %s: %s", Clock(l.Start.Sub(meetingStart)), l.Speaker, l.Text)
}

// Clock formats a duration as HH:MM:SS.
func Clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// Builder turns a stream of words into lines.
type Builder struct {
	TL  *Timeline
	cur *Line
}

// Add appends a word and returns a finished line when the speaker changes
// or after a pause.
func (b *Builder) Add(w stt.Word) *Line {
	who := b.TL.SpeakerAt(w.Start, w.End)
	if who == "" {
		who = i18n.T("speaker.unknown")
	}
	var done *Line
	if b.cur != nil && (b.cur.Speaker != who || w.Start.Sub(b.cur.End) > newLine || len(b.cur.Text) > maxChars) {
		done = b.cur
		b.cur = nil
	}
	if b.cur == nil {
		b.cur = &Line{Start: w.Start, Speaker: who, Text: w.Text, End: w.End}
	} else {
		b.cur.Text += " " + w.Text
		b.cur.End = w.End
	}
	return done
}

// Flush returns the line in progress, if any.
func (b *Builder) Flush() *Line {
	l := b.cur
	b.cur = nil
	return l
}
