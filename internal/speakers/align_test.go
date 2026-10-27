package speakers

import (
	"testing"
	"time"

	"deo-minutes/internal/stt"
)

var t0 = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func at(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }

func timeline() *Timeline {
	tl := NewTimeline("Asistent Minute (înregistrează)")
	for s := 0.0; s < 4; s += 0.5 {
		tl.Add(at(s), []string{"Ana Pop", "Asistent Minute (înregistrează)"})
	}
	// Indicator gap 4-6 s (Ana still talking), then Ion; overlap at 7 s.
	for s := 6.0; s < 9; s += 0.5 {
		who := []string{"Ion Ionescu"}
		if s == 7 {
			who = append(who, "Ana Pop")
		}
		tl.Add(at(s), who)
	}
	return tl
}

func TestSpeakerAt(t *testing.T) {
	tl := timeline()
	cases := []struct {
		s, e float64
		want string
	}{
		{0.2, 0.6, "Ana Pop"},
		{4.6, 4.9, "Ana Pop"}, // carried over the indicator gap
		{6.2, 7.4, "Ion Ionescu"},
		{20, 21, ""},
	}
	for _, c := range cases {
		if got := tl.SpeakerAt(at(c.s), at(c.e)); got != c.want {
			t.Errorf("[%v,%v] = %q, want %q", c.s, c.e, got, c.want)
		}
	}
}

func TestOutOfOrderSnapshots(t *testing.T) {
	tl := NewTimeline()
	tl.Add(at(2), []string{"B"})
	tl.Add(at(1), []string{"A"})
	if got := tl.SpeakerAt(at(1), at(1.2)); got != "A" {
		t.Fatalf("got %q", got)
	}
}

func TestBuilder(t *testing.T) {
	b := &Builder{TL: timeline()}
	words := []stt.Word{
		{Start: at(0.2), End: at(0.5), Text: "Bună"},
		{Start: at(0.6), End: at(1.0), Text: "ziua,"},
		{Start: at(6.2), End: at(6.6), Text: "Mulțumesc."},
		{Start: at(20), End: at(20.4), Text: "Cine?"},
	}
	var lines []Line
	for _, w := range words {
		if l := b.Add(w); l != nil {
			lines = append(lines, *l)
		}
	}
	if l := b.Flush(); l != nil {
		lines = append(lines, *l)
	}
	want := []string{
		"[00:00:00] Ana Pop: Bună ziua,",
		"[00:00:06] Ion Ionescu: Mulțumesc.",
		"[00:00:20] Participant necunoscut: Cine?",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i, l := range lines {
		if got := l.Format(t0); got != want[i] {
			t.Errorf("line %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestClock(t *testing.T) {
	if Clock(4*time.Hour+5*time.Minute+6*time.Second) != "04:05:06" || Clock(-time.Second) != "00:00:00" {
		t.Fatal("clock")
	}
}

func TestLagShiftsSnapshots(t *testing.T) {
	tl := NewTimeline()
	tl.Lag = time.Second
	tl.Add(at(3), []string{"A"}) // indicator lit at 3 s for speech at 2 s
	if got := tl.SpeakerAt(at(2), at(2.2)); got != "A" {
		t.Fatalf("got %q", got)
	}
}
