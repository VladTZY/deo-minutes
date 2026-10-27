package stt

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"deo-minutes/internal/audio"
)

func TestAbsTimeAcrossGap(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	marks := []Mark{{Off: 0, TS: t0}, {Off: audio.BytesPerSecond, TS: t0.Add(30 * time.Second)}}
	if got := AbsTime(marks, audio.BytesPerSecond/2); !got.Equal(t0.Add(500 * time.Millisecond)) {
		t.Errorf("first chunk: %v", got)
	}
	if got := AbsTime(marks, audio.BytesPerSecond*3/2); !got.Equal(t0.Add(30500 * time.Millisecond)) {
		t.Errorf("after gap: %v", got)
	}
}

// TestLocalWhisper runs the real whisper-cli on Romanian speech. Set
// DEO_WHISPER_EXE, DEO_WHISPER_MODEL, DEO_TEST_PCM (16 kHz mono s16le) and
// optionally DEO_WHISPER_VAD and DEO_EXPECT (comma-separated words).
func TestLocalWhisper(t *testing.T) {
	exe, model, pcm := os.Getenv("DEO_WHISPER_EXE"), os.Getenv("DEO_WHISPER_MODEL"), os.Getenv("DEO_TEST_PCM")
	if exe == "" || model == "" || pcm == "" {
		t.Skip("set DEO_WHISPER_EXE, DEO_WHISPER_MODEL and DEO_TEST_PCM")
	}
	t0 := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	var progress []float64
	piece, _ := strconv.Atoi(os.Getenv("DEO_PIECE_SEC"))
	l := &Local{Exe: exe, Model: model, VADModel: os.Getenv("DEO_WHISPER_VAD"), PieceSec: max(piece, 0),
		OnProgress: func(done, total float64) { progress = append(progress, done/total) }}
	start := time.Now()
	words, err := l.Transcribe(context.Background(), pcm, []Mark{{Off: 0, TS: t0}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, w := range words {
		sb.WriteString(w.Text + " ")
		if w.End.Before(w.Start) || w.Start.Before(t0) {
			t.Errorf("bad times for %q: %v-%v", w.Text, w.Start, w.End)
		}
	}
	text := sb.String()
	t.Logf("%d words in %v: %s", len(words), time.Since(start).Round(time.Millisecond), text)
	for i, w := range words {
		t.Logf("  %6.2f-%6.2f %s", w.Start.Sub(t0).Seconds(), w.End.Sub(t0).Seconds(), w.Text)
		if i > 0 && w.Start.Before(words[i-1].Start) {
			t.Errorf("word %q out of order", w.Text)
		}
	}
	for _, want := range strings.Split(os.Getenv("DEO_EXPECT"), ",") {
		if want != "" && !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("missing %q", want)
		}
	}
	if len(progress) == 0 || progress[len(progress)-1] != 1 {
		t.Errorf("progress = %v", progress)
	}
}

func TestFixVADTiming(t *testing.T) {
	// 0-2 s speech, 2-10 s silence, 10-12 s speech.
	pcm := make([]byte, 12*audio.BytesPerSecond)
	for i := 0; i < len(pcm); i += 2 {
		sec := i / audio.BytesPerSecond
		if sec < 2 || sec >= 10 {
			pcm[i+1] = 0x20 // loud
		}
	}
	seg := func(from, to int64, text string) whisperSeg {
		var s whisperSeg
		s.Offsets.From, s.Offsets.To, s.Text = from, to, text
		return s
	}
	segs := []whisperSeg{
		seg(1000, 1800, " vineri."),
		seg(2100, 2200, " Am"),     // stamped inside the silence
		seg(2200, 10300, " decis"), // stretched across the silence
		seg(10300, 10600, " să"),
	}
	fixVADTiming(segs, pcm)
	if segs[0].Offsets.From != 1000 || segs[3].Offsets.From != 10300 {
		t.Errorf("spoken words moved: %+v %+v", segs[0].Offsets, segs[3].Offsets)
	}
	for _, i := range []int{1, 2} {
		if o := segs[i].Offsets; o.From < 9980 || o.From > 10100 || o.To-o.From > 1000 {
			t.Errorf("%q not moved to where speech resumes: %+v", segs[i].Text, o)
		}
	}
}
