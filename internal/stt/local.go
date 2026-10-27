package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"deo-minutes/internal/audio"
)

// Mark ties a byte offset in the recorded PCM file to the capture time of
// the chunk that starts there, so gaps in capture keep correct timestamps.
type Mark struct {
	Off int64     `json:"off"`
	TS  time.Time `json:"ts"`
}

// AbsTime converts a byte offset in the PCM file to wall-clock time.
func AbsTime(marks []Mark, off int64) time.Time {
	if len(marks) == 0 {
		return time.Time{}
	}
	i := sort.Search(len(marks), func(i int) bool { return marks[i].Off > off }) - 1
	if i < 0 {
		i = 0
	}
	delta := float64(off-marks[i].Off) / audio.BytesPerSecond
	return marks[i].TS.Add(time.Duration(delta * float64(time.Second)))
}

// Local transcribes a recorded meeting after the fact with whisper.cpp
// (whisper-cli) and a Romanian model, on the CPU, in pieces so progress is
// visible and a 4-hour file never needs one huge run.
type Local struct {
	Exe      string // whisper-cli(.exe)
	Model    string // ggml model file
	VADModel string // optional silero VAD ggml model; skips silence
	Language string // "ro"
	Threads  int
	PieceSec int // default 600 (10 min)
	// OnProgress reports processed and total seconds of audio.
	OnProgress func(done, total float64)
}

const pieceOverlap = 2 * audio.BytesPerSecond // re-read 2 s so words cut at a boundary survive

var speakerTag = regexp.MustCompile(`\[S\d+\]`)

// ErrMissingRuntime: Windows could not load whisper-cli's DLLs, usually the
// Microsoft Visual C++ 2015-2022 runtime.
var ErrMissingRuntime = errors.New("whisper-cli: missing DLL (Visual C++ runtime)")

// missingDLL recognizes STATUS_DLL_NOT_FOUND (0xC0000135).
func missingDLL(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	code := uint32(ee.ExitCode())
	return code == 0xC0000135
}

// Transcribe reads pcmPath (16 kHz mono s16le), using workDir for temporary
// files, and returns words at absolute times.
func (l *Local) Transcribe(ctx context.Context, pcmPath string, marks []Mark, workDir string) ([]Word, error) {
	if l.PieceSec == 0 {
		l.PieceSec = 600
	}
	if l.Threads <= 0 {
		l.Threads = max(1, runtime.NumCPU()-1)
	}
	if l.Language == "" {
		l.Language = "ro"
	}
	f, err := os.Open(pcmPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size() &^ 1
	piece := int64(l.PieceSec) * audio.BytesPerSecond
	total := float64(size) / audio.BytesPerSecond
	var words []Word
	var lastEnd time.Time
	for start := int64(0); start < size; start += piece {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := min(start+piece+pieceOverlap, size)
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		segs, err := l.runPiece(ctx, buf, workDir)
		if err != nil {
			return nil, err
		}
		fixVADTiming(segs, buf)
		last := end == size
		seamEnd := lastEnd // words of the previous piece end here
		for _, s := range segs {
			text := strings.TrimSpace(speakerTag.ReplaceAllString(s.Text, ""))
			if text == "" {
				continue
			}
			// The 2 s past this piece's end are re-read by the next piece:
			// leave words starting there to it, unless this is the last piece.
			if !last && msToBytes(s.Offsets.From) >= piece {
				continue
			}
			w := Word{
				Start: AbsTime(marks, start+msToBytes(s.Offsets.From)),
				End:   AbsTime(marks, start+msToBytes(s.Offsets.To)),
				Text:  text,
				Lang:  l.Language,
			}
			// At the seam, skip words the previous piece already emitted.
			if msToBytes(s.Offsets.From) < pieceOverlap && w.Start.Before(seamEnd.Add(-dupTolerance)) {
				continue
			}
			if isPunct(text) && len(words) > 0 { // keep punctuation on the previous word
				words[len(words)-1].Text += text
				continue
			}
			words = append(words, w)
			lastEnd = w.End
		}
		if l.OnProgress != nil {
			l.OnProgress(float64(min(start+piece, size))/audio.BytesPerSecond, total)
		}
	}
	return words, nil
}

func msToBytes(ms int64) int64 { return ms * audio.BytesPerSecond / 1000 &^ 1 }

func isPunct(s string) bool {
	return strings.Trim(s, ".,;:!?…-–\"'„”«»()") == ""
}

// fixVADTiming repairs a whisper.cpp VAD artefact: words right after a long
// pause can be stamped at (or stretched from) the start of the pause. A word
// whose first 200 ms are clearly silent gets its start moved to where sound
// resumes (within 60 s), keeping at most its original length.
func fixVADTiming(segs []whisperSeg, pcm []byte) {
	const frame = 20 * audio.BytesPerSecond / 1000 // 20 ms
	loud := func(a, b int64) bool {
		a, b = max(a, 0), min(b, int64(len(pcm)))
		for off := a; off+frame <= b; off += frame {
			if audio.RMS(pcm[off:off+frame]) >= 0.003 {
				return true
			}
		}
		return false
	}
	firstLoud := func(a, limit int64) int64 {
		for off := max(a, 0); off+frame <= min(limit, int64(len(pcm))); off += frame {
			if audio.RMS(pcm[off:off+frame]) >= 0.003 {
				return off
			}
		}
		return -1
	}
	for i := range segs {
		s := &segs[i]
		if strings.TrimSpace(s.Text) == "" || isPunct(strings.TrimSpace(s.Text)) {
			continue
		}
		from := msToBytes(s.Offsets.From)
		if loud(from, from+msToBytes(200)) {
			continue
		}
		at := firstLoud(from, from+msToBytes(60_000))
		if at < 0 {
			continue
		}
		newFrom := at * 1000 / audio.BytesPerSecond
		dur := min(s.Offsets.To-s.Offsets.From, 1000)
		s.Offsets.From = newFrom
		s.Offsets.To = max(s.Offsets.To, newFrom+dur)
		if s.Offsets.To-newFrom > 1500 { // was stretched across the pause
			s.Offsets.To = newFrom + 600
		}
	}
}

type whisperSeg struct {
	Offsets struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	} `json:"offsets"`
	Text string `json:"text"`
}

// runPiece writes one WAV, runs whisper-cli on it and parses its JSON.
func (l *Local) runPiece(ctx context.Context, pcm []byte, workDir string) ([]whisperSeg, error) {
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	wavPath := filepath.Join(workDir, "piece.wav")
	outBase := filepath.Join(workDir, "piece")
	defer func() { _ = os.Remove(wavPath); _ = os.Remove(outBase + ".json") }()
	w, err := audio.CreateWAV(wavPath)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(pcm); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	args := []string{
		"-m", l.Model, "-f", wavPath, "-l", l.Language, "-t", strconv.Itoa(l.Threads),
		"-mc", "0", // no previous-text prompting: fine-tunes loop on long audio otherwise
		"-ml", "1", "-sow", // one segment per word, for speaker alignment
		"-oj", "-of", outBase, "-np",
	}
	if l.VADModel != "" {
		if _, err := os.Stat(l.VADModel); err == nil {
			args = append(args, "--vad", "-vm", l.VADModel)
		}
	}
	cmd := exec.CommandContext(ctx, l.Exe, args...)
	hideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if missingDLL(err) {
			return nil, ErrMissingRuntime
		}
		tail := stderr.String()
		if len(tail) > 500 {
			tail = tail[len(tail)-500:]
		}
		return nil, fmt.Errorf("whisper-cli: %w: %s", err, strings.TrimSpace(tail))
	}
	b, err := os.ReadFile(outBase + ".json")
	if err != nil {
		return nil, fmt.Errorf("whisper-cli output: %w", err)
	}
	var out struct {
		Transcription []whisperSeg `json:"transcription"`
	}
	if err := json.Unmarshal(bytes.ToValidUTF8(b, nil), &out); err != nil {
		return nil, fmt.Errorf("whisper-cli json: %w", err)
	}
	return out.Transcription, nil
}
