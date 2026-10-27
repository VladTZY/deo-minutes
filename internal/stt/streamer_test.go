package stt

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"deo-minutes/internal/audio"
)

// fakeStream "recognizes" one word per second of audio it receives: the word
// for second k spans [k+0.2, k+0.7] and is followed by a comma.
type fakeStream struct {
	mu       sync.Mutex
	bytes    int
	emitted  int
	out      chan []RelWord
	failAt   int // fail Send after this many bytes (0 = never)
	failErr  error
	finished bool
}

func newFake(failAt int, err error) *fakeStream {
	return &fakeStream{out: make(chan []RelWord, 1000), failAt: failAt, failErr: err}
}

func (f *fakeStream) Send(_ context.Context, pcm []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAt > 0 && f.bytes+len(pcm) > f.failAt {
		return f.failErr
	}
	f.bytes += len(pcm)
	for (f.emitted+1)*audio.BytesPerSecond <= f.bytes {
		k := float64(f.emitted)
		f.out <- []RelWord{{Start: k + 0.2, End: k + 0.7, Text: "cuvânt"}, {Text: ",", Punct: true}}
		f.emitted++
	}
	return nil
}
func (f *fakeStream) Results() <-chan []RelWord { return f.out }
func (f *fakeStream) Finish() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.finished {
		f.finished = true
		close(f.out)
	}
	return nil
}
func (f *fakeStream) Close() error { return f.Finish() }
func (f *fakeStream) Err() error   { return nil }

func chunks(n int, start time.Time, loud bool) []Chunk {
	out := make([]Chunk, n)
	for i := range out {
		pcm := make([]byte, audio.BytesPerSecond)
		if loud {
			for j := 0; j < len(pcm); j += 2 {
				v := int16(8000)
				binary.LittleEndian.PutUint16(pcm[j:], uint16(v))
			}
		}
		out[i] = Chunk{TS: start.Add(time.Duration(i) * time.Second), PCM: pcm}
	}
	return out
}

func run(t *testing.T, s *Streamer, in []Chunk) []Word {
	t.Helper()
	ch := make(chan Chunk)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, wait := s.Run(ctx, ch)
	go func() {
		for _, c := range in {
			ch <- c
		}
		close(ch)
	}()
	var words []Word
	for w := range out {
		words = append(words, w)
	}
	if err := wait(); err != nil {
		t.Fatalf("run: %v", err)
	}
	return words
}

func checkOnePerSecond(t *testing.T, words []Word, start time.Time, n int) {
	t.Helper()
	if len(words) != n {
		t.Fatalf("got %d words, want %d", len(words), n)
	}
	for i, w := range words {
		want := start.Add(time.Duration(i)*time.Second + 200*time.Millisecond)
		if d := w.Start.Sub(want); d < -time.Millisecond || d > time.Millisecond {
			t.Errorf("word %d starts %v, want %v", i, w.Start, want)
		}
		if w.Text != "cuvânt," {
			t.Errorf("punctuation not attached: %q", w.Text)
		}
	}
}

func TestStreamerHappyPath(t *testing.T) {
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	s := &Streamer{Open: func(context.Context) (Stream, error) { return newFake(0, nil), nil }}
	checkOnePerSecond(t, run(t, s, chunks(10, start, true)), start, 10)
}

func TestStreamerReconnectReplaysWithoutDuplicates(t *testing.T) {
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	var opens atomic.Int32
	var reconnects atomic.Int32
	s := &Streamer{
		Open: func(context.Context) (Stream, error) {
			if opens.Add(1) == 1 {
				// First stream dies mid-way through second 6.
				return newFake(5*audio.BytesPerSecond+eventBytes*2, errors.New("connection reset")), nil
			}
			return newFake(0, nil), nil
		},
		ReplayPause: time.Millisecond,
		OnReconnect: func(error) { reconnects.Add(1) },
	}
	words := run(t, s, chunks(12, start, true))
	checkOnePerSecond(t, words, start, 12)
	if reconnects.Load() != 1 || opens.Load() != 2 {
		t.Errorf("opens=%d reconnects=%d", opens.Load(), reconnects.Load())
	}
}

func TestStreamerRotatesOnQuietChunk(t *testing.T) {
	start := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	var opens atomic.Int32
	s := &Streamer{
		Open:   func(context.Context) (Stream, error) { opens.Add(1); return newFake(0, nil), nil },
		Rotate: time.Nanosecond, // every quiet chunk ends the stream
	}
	in := append(chunks(3, start, true), chunks(3, start.Add(3*time.Second), false)...)
	words := run(t, s, in)
	checkOnePerSecond(t, words, start, 6)
	if opens.Load() < 3 {
		t.Errorf("expected rotation, opens=%d", opens.Load())
	}
}

func TestStreamerFatalStops(t *testing.T) {
	s := &Streamer{Open: func(context.Context) (Stream, error) {
		return nil, fmt.Errorf("%w: AccessDeniedException", ErrFatal)
	}}
	ch := make(chan Chunk)
	out, wait := s.Run(context.Background(), ch)
	for range out {
	}
	if err := wait(); !errors.Is(err, ErrFatal) {
		t.Fatalf("want fatal, got %v", err)
	}
}

func TestTimelineHandlesGaps(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tl := &timeline{}
	tl.add(Chunk{TS: t0, PCM: make([]byte, audio.BytesPerSecond)})
	tl.add(Chunk{TS: t0.Add(10 * time.Second), PCM: make([]byte, audio.BytesPerSecond)}) // 9 s gap in capture
	if got := tl.abs(1.5); !got.Equal(t0.Add(10500 * time.Millisecond)) {
		t.Fatalf("abs(1.5) = %v", got)
	}
}

// TestFourHourMeeting pushes 4 h of audio through the streamer (with one
// network drop) and checks every second is transcribed once and memory stays
// bounded. Skipped with -short.
func TestFourHourMeeting(t *testing.T) {
	if testing.Short() {
		t.Skip("long")
	}
	const n = 4 * 3600
	start := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	var opens atomic.Int32
	s := &Streamer{
		Open: func(context.Context) (Stream, error) {
			if opens.Add(1) == 2 {
				return newFake(3600*audio.BytesPerSecond, errors.New("reset")), nil
			}
			return newFake(0, nil), nil
		},
		Rotate:      time.Nanosecond, // rotate on every quiet chunk: stresses rotation
		ReplayPause: time.Microsecond,
	}
	ch := make(chan Chunk, 64)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, wait := s.Run(ctx, ch)
	go func() {
		loud := chunks(1, start, true)[0].PCM
		quiet := make([]byte, audio.BytesPerSecond)
		for i := 0; i < n; i++ {
			pcm := loud
			if i%600 == 599 { // a quiet second every 10 min
				pcm = quiet
			}
			ch <- Chunk{TS: start.Add(time.Duration(i) * time.Second), PCM: pcm}
		}
		close(ch)
	}()
	var peak uint64
	count := 0
	var last time.Time
	for w := range out {
		if !w.Start.After(last) {
			t.Fatalf("word %d out of order or duplicated at %v", count, w.Start)
		}
		last = w.Start
		count++
		if count%3600 == 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			peak = max(peak, m.HeapAlloc)
		}
	}
	if err := wait(); err != nil {
		t.Fatal(err)
	}
	if count != n {
		t.Fatalf("words = %d, want %d", count, n)
	}
	if peak > 300<<20 {
		t.Errorf("heap peaked at %d MB", peak>>20)
	}
	t.Logf("streams opened: %d, heap peak %d MB", opens.Load(), peak>>20)
}
