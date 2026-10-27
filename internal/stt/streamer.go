// Package stt turns the bot's live PCM into timed words with Amazon
// Transcribe Streaming. It keeps going through network drops (reconnect and
// replay) and rotates streams so a 4-hour meeting never hits a stream limit.
package stt

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"deo-minutes/internal/audio"
)

// Chunk is ~1 s of 16 kHz mono s16le PCM starting at TS.
type Chunk struct {
	TS  time.Time
	PCM []byte
}

// Word is one recognized word (punctuation attached) at absolute times.
type Word struct {
	Start, End time.Time
	Text       string
	Lang       string
}

// RelWord is a word as a stream reports it: seconds from the stream's start.
type RelWord struct {
	Start, End float64
	Text       string
	Punct      bool
	Lang       string
}

// Stream is one open transcription session.
type Stream interface {
	Send(ctx context.Context, pcm []byte) error
	// Results yields finished (non-partial) words; it closes when the stream ends.
	Results() <-chan []RelWord
	// Finish signals end of audio; Results then drains and closes.
	Finish() error
	Close() error
	Err() error
}

// Opener starts a new Stream.
type Opener func(ctx context.Context) (Stream, error)

// ErrFatal wraps errors that retrying cannot fix (bad credentials, denied access).
var ErrFatal = errors.New("transcriere oprită")

// Streamer runs streams back to back over one meeting.
type Streamer struct {
	Open        Opener
	Rotate      time.Duration // start a fresh stream after this long (at a quiet moment)
	MaxBackoff  time.Duration
	ReplayPause time.Duration // pause between replayed chunks after a reconnect
	OnReconnect func(err error)

	mu        sync.Mutex
	lastFinal time.Time // end of the last emitted word
}

const (
	eventBytes   = audio.BytesPerSecond / 5 // 200 ms per AudioEvent (AWS recommends 50-200 ms)
	quietRMS     = 0.005
	rotateGrace  = 2 * time.Minute
	maxRetained  = 30 * 60 // chunks kept for replay (30 min)
	dupTolerance = 50 * time.Millisecond
)

// Run consumes in until it closes, emitting words on the returned channel,
// which closes when all audio is transcribed. A fatal error is reported via
// the returned func after the channel closes.
func (s *Streamer) Run(ctx context.Context, in <-chan Chunk) (<-chan Word, func() error) {
	out := make(chan Word, 1024)
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(out)
		runErr = s.run(ctx, in, out)
	}()
	return out, func() error { <-done; return runErr }
}

// timeline maps a stream's relative seconds to the absolute time of the audio.
type timeline struct {
	mu     sync.Mutex
	starts []float64 // relative start (s) of each chunk sent
	ts     []time.Time
	sent   float64
}

func (t *timeline) add(c Chunk) {
	t.mu.Lock()
	t.starts = append(t.starts, t.sent)
	t.ts = append(t.ts, c.TS)
	t.sent += float64(len(c.PCM)) / audio.BytesPerSecond
	t.mu.Unlock()
}

func (t *timeline) abs(rel float64) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.starts) == 0 {
		return time.Time{}
	}
	i := len(t.starts) - 1
	for i > 0 && t.starts[i] > rel {
		i--
	}
	return t.ts[i].Add(time.Duration((rel - t.starts[i]) * float64(time.Second)))
}

func (s *Streamer) run(ctx context.Context, in <-chan Chunk, out chan<- Word) error {
	if s.MaxBackoff == 0 {
		s.MaxBackoff = 30 * time.Second
	}
	if s.ReplayPause == 0 {
		s.ReplayPause = 250 * time.Millisecond
	}
	var retained []Chunk // audio of the current stream, for replay
	backoff := time.Second
	inOpen := true

	for {
		st, err := s.Open(ctx)
		if err != nil {
			if errors.Is(err, ErrFatal) || ctx.Err() != nil {
				return err
			}
			s.reconnecting(err)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = min(backoff*2, s.MaxBackoff)
			continue
		}
		tl := &timeline{}
		recvDone := make(chan struct{})
		go func() {
			defer close(recvDone)
			s.receive(st, tl, out)
		}()

		started := time.Now()
		streamErr := func() error {
			// Replay what the previous stream did not finish.
			retained = s.unfinished(retained)
			for _, c := range retained {
				if err := s.send(ctx, st, tl, c); err != nil {
					return err
				}
				if !sleep(ctx, s.ReplayPause) {
					return ctx.Err()
				}
			}
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case c, ok := <-in:
					if !ok {
						inOpen = false
						return nil
					}
					retained = append(retained, c)
					if len(retained) > maxRetained {
						retained = retained[len(retained)-maxRetained:]
					}
					if err := s.send(ctx, st, tl, c); err != nil {
						return err
					}
					age := time.Since(started)
					if s.Rotate > 0 && (age >= s.Rotate && audio.RMS(c.PCM) < quietRMS || age >= s.Rotate+rotateGrace) {
						return nil
					}
				}
			}
		}()

		if streamErr == nil {
			_ = st.Finish()
			<-recvDone
			streamErr = st.Err()
		} else {
			_ = st.Close()
			<-recvDone
		}
		_ = st.Close()

		if streamErr != nil {
			if errors.Is(streamErr, ErrFatal) || ctx.Err() != nil {
				return streamErr
			}
			s.reconnecting(streamErr)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = min(backoff*2, s.MaxBackoff)
			continue
		}
		// A clean finish transcribed everything sent: nothing to replay.
		backoff = time.Second
		retained = nil
		if !inOpen {
			return nil
		}
	}
}

func (s *Streamer) reconnecting(err error) {
	slog.Warn("transcribe reconnect", "err", err)
	if s.OnReconnect != nil {
		s.OnReconnect(err)
	}
}

// unfinished drops chunks that end before the last emitted word.
func (s *Streamer) unfinished(chunks []Chunk) []Chunk {
	s.mu.Lock()
	last := s.lastFinal
	s.mu.Unlock()
	i := 0
	for i < len(chunks) {
		c := chunks[i]
		end := c.TS.Add(time.Duration(float64(len(c.PCM)) / audio.BytesPerSecond * float64(time.Second)))
		if end.After(last) {
			break
		}
		i++
	}
	return chunks[i:]
}

func (s *Streamer) send(ctx context.Context, st Stream, tl *timeline, c Chunk) error {
	tl.add(c)
	for off := 0; off < len(c.PCM); off += eventBytes {
		end := min(off+eventBytes, len(c.PCM))
		if err := st.Send(ctx, c.PCM[off:end]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Streamer) receive(st Stream, tl *timeline, out chan<- Word) {
	for words := range st.Results() {
		var pending *Word
		flush := func() {
			if pending == nil {
				return
			}
			s.mu.Lock()
			dup := pending.Start.Before(s.lastFinal.Add(-dupTolerance))
			if !dup && pending.End.After(s.lastFinal) {
				s.lastFinal = pending.End
			}
			s.mu.Unlock()
			if !dup {
				out <- *pending
			}
			pending = nil
		}
		for _, w := range words {
			if w.Punct {
				if pending != nil {
					pending.Text += w.Text
				}
				continue
			}
			flush()
			pending = &Word{Start: tl.abs(w.Start), End: tl.abs(w.End), Text: w.Text, Lang: w.Lang}
		}
		flush()
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
