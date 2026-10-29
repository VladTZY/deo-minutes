package app

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/speakers"
	"deo-minutes/internal/stt"
)

// steps reports which steps a session has completed.
func (a *App) steps(dir string, meta sessionMeta) Steps {
	exists := func(name string) bool {
		st, err := os.Stat(filepath.Join(dir, name))
		return err == nil && st.Size() > 0
	}
	return Steps{
		Recorded:    exists(audioFile),
		Transcribed: exists(transcriptFile),
		Generated:   meta.ExcelPath != "" || meta.SummaryPath != "",
	}
}

// loadSession reads a session folder for a step, refusing bad ids and
// sessions already running.
func (a *App) loadSession(id string) (*session, error) {
	if !sessionIDRe.MatchString(id) {
		return nil, i18n.Errorf(nil, "err.unexpected")
	}
	dir := filepath.Join(config.SessionsDir(), id)
	var meta sessionMeta
	if err := readJSON(filepath.Join(dir, metaFile), &meta); err != nil {
		return nil, i18n.Errorf(err, "err.unexpected")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session != nil {
		return nil, i18n.Errorf(nil, "err.busy")
	}
	s := &session{meta: meta, dir: dir, stopCh: make(chan struct{})}
	a.session = s
	return s, nil
}

// Transcribe runs the local Romanian transcriber on a recorded session
// (step 2). Works for any session that still has its audio.
func (a *App) Transcribe(id string) error {
	local, err := a.newLocal()
	if err != nil {
		return err
	}
	s, err := a.loadSession(id)
	if err != nil {
		return err
	}
	if !a.steps(s.dir, s.meta).Recorded {
		a.endSession(nil)
		return i18n.Errorf(nil, "err.local.noAudio")
	}
	ctx, cancel := context.WithCancel(a.ctx)
	go func() { // Oprește cancels transcription too
		select {
		case <-s.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	a.setStatus(func(st *Status) {
		*st = Status{State: StateTranscribing, SessionID: s.meta.ID, Mode: s.meta.Mode, CanStop: true, Steps: a.steps(s.dir, s.meta)}
	})
	local.OnProgress = func(done, total float64) {
		a.setStatus(func(st *Status) {
			if total > 0 {
				st.Progress = done / total
			}
		})
	}
	go func() {
		defer cancel()
		if err := a.transcribeLocal(ctx, s, local); err != nil {
			if errors.Is(err, context.Canceled) {
				a.endSession(nil)
				a.setStatus(func(st *Status) { st.State, st.CanStop, st.Steps = StateRecorded, false, a.steps(s.dir, s.meta) })
				return
			}
			a.endSession(err)
			return
		}
		a.mu.Lock()
		a.session = nil
		a.mu.Unlock()
		a.setStatus(func(st *Status) {
			st.State, st.CanStop, st.Progress = StateTranscribed, false, 1
			st.Steps = a.steps(s.dir, s.meta)
		})
	}()
	return nil
}

func (a *App) transcribeLocal(ctx context.Context, s *session, local *stt.Local) error {
	marks, err := readJSONL[stt.Mark](filepath.Join(s.dir, marksFile))
	if err != nil || len(marks) == 0 {
		return i18n.Errorf(err, "err.local.noAudio")
	}
	roster, _ := readJSONL[rosterLine](filepath.Join(s.dir, rosterFile))
	tl := speakers.NewTimeline(s.meta.BotName)
	for _, r := range roster {
		tl.Add(time.UnixMilli(r.TSMS), r.Speakers)
	}
	words, err := local.Transcribe(ctx, filepath.Join(s.dir, audioFile), marks, filepath.Join(s.dir, "work"))
	_ = os.RemoveAll(filepath.Join(s.dir, "work"))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, stt.ErrMissingRuntime) {
			return i18n.Errorf(err, "err.local.vcredist")
		}
		return i18n.Errorf(err, "err.local.failed")
	}
	if len(words) == 0 {
		return i18n.Errorf(nil, "warn.transcriptEmpty")
	}
	tmp := filepath.Join(s.dir, transcriptFile+".partial")
	tw, err := newTranscriptWriter(tmp, marks[0].TS, a.emit)
	if err != nil {
		return i18n.Errorf(err, "err.unexpected")
	}
	b := &speakers.Builder{TL: tl}
	for _, w := range words {
		tw.write(b.Add(w))
	}
	tw.write(b.Flush())
	if err := tw.close(); err != nil {
		return i18n.Errorf(err, "err.unexpected")
	}
	return os.Rename(tmp, filepath.Join(s.dir, transcriptFile))
}

// Outputs returns what step 3 generates by default (the last choice).
func (a *App) Outputs() Outputs {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Outputs{Minutes: a.cfg.Output.Minutes, Summary: a.cfg.Output.Summary}
}

// Generate makes the chosen outputs from a session's transcript (step 3)
// with the current mode's AI provider, and remembers the choice (PROD uses
// it after each meeting). Also used to retry after a failure.
func (a *App) Generate(id string, out Outputs) error {
	if !out.Minutes && !out.Summary {
		return i18n.Errorf(nil, "err.outputs.none")
	}
	a.mu.Lock()
	if a.cfg.Output.Minutes != out.Minutes || a.cfg.Output.Summary != out.Summary {
		c := a.cfg
		c.Output.Minutes, c.Output.Summary = out.Minutes, out.Summary
		if err := config.Save(config.Path(), c); err != nil {
			slog.Warn("save output choice", "err", err)
		} else {
			a.cfg = c
		}
	}
	a.mu.Unlock()
	s, err := a.loadSession(id)
	if err != nil {
		return err
	}
	if !a.steps(s.dir, s.meta).Transcribed {
		a.endSession(nil)
		return i18n.Errorf(nil, "warn.transcriptEmpty")
	}
	a.setStatus(func(st *Status) {
		*st = Status{State: StateGenerating, SessionID: s.meta.ID, Mode: s.meta.Mode, Steps: a.steps(s.dir, s.meta)}
	})
	go a.generate(a.ctx, s, out)
	return nil
}

// Transcript returns a session's transcript lines for preview.
func (a *App) Transcript(id string) ([]string, error) {
	if !sessionIDRe.MatchString(id) {
		return nil, i18n.Errorf(nil, "err.unexpected")
	}
	t, err := readTranscript(filepath.Join(config.SessionsDir(), id, transcriptFile))
	if err != nil {
		return nil, i18n.Errorf(err, "warn.transcriptEmpty")
	}
	out := make([]string, len(t))
	for i, l := range t {
		out[i] = l.String()
	}
	return out, nil
}

// ImportAudio creates a session from a WAV file (16 kHz, mono, 16-bit), so
// the Transcribe and Generate steps can be tested without a meeting.
func (a *App) ImportAudio(project string, meetingNo int) (string, error) {
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   i18n.T("import.title"),
		Filters: []wruntime.FileFilter{{DisplayName: "WAV (16 kHz mono)", Pattern: "*.wav"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return a.importWAV(path, project, meetingNo)
}

func (a *App) importWAV(path, project string, meetingNo int) (string, error) {
	if project == "" {
		project = "Test"
	}
	if meetingNo < 1 {
		meetingNo = 1
	}
	f, err := os.Open(path)
	if err != nil {
		return "", i18n.Errorf(err, "err.import.wav")
	}
	defer func() { _ = f.Close() }()
	data, err := wavData(f)
	if err != nil {
		return "", i18n.Errorf(err, "err.import.wav")
	}
	a.mu.Lock()
	mode, bot := a.cfg.Mode, a.cfg.Bot.DisplayName
	a.mu.Unlock()
	s := newSession(sessionMeta{Mode: mode, Project: project, MeetingNo: meetingNo, BotName: bot, Imported: filepath.Base(path)})
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return "", i18n.Errorf(err, "err.unexpected")
	}
	out, err := os.OpenFile(filepath.Join(s.dir, audioFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", i18n.Errorf(err, "err.unexpected")
	}
	if _, err := io.Copy(out, data); err != nil {
		_ = out.Close()
		return "", i18n.Errorf(err, "err.import.wav")
	}
	if err := out.Close(); err != nil {
		return "", i18n.Errorf(err, "err.unexpected")
	}
	if err := writeJSONL(filepath.Join(s.dir, marksFile), []stt.Mark{{Off: 0, TS: s.meta.Started}}); err != nil {
		return "", i18n.Errorf(err, "err.unexpected")
	}
	if err := writeJSON(filepath.Join(s.dir, metaFile), s.meta); err != nil {
		return "", i18n.Errorf(err, "err.unexpected")
	}
	a.setStatus(func(st *Status) {
		*st = Status{State: StateRecorded, SessionID: s.meta.ID, Mode: mode, Steps: a.steps(s.dir, s.meta)}
	})
	return s.meta.ID, nil
}

// wavData checks a RIFF/WAVE header for 16 kHz mono PCM16 and returns a
// reader positioned at the samples.
func wavData(r io.ReadSeeker) (io.Reader, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil || string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	fmtOK := false
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return nil, errors.New("no data chunk")
		}
		size := int64(binary.LittleEndian.Uint32(ch[4:]))
		switch string(ch[0:4]) {
		case "fmt ":
			var f [16]byte
			if size < 16 {
				return nil, errors.New("bad fmt chunk")
			}
			if _, err := io.ReadFull(r, f[:]); err != nil {
				return nil, err
			}
			format, channels := binary.LittleEndian.Uint16(f[0:]), binary.LittleEndian.Uint16(f[2:])
			rate, bits := binary.LittleEndian.Uint32(f[4:]), binary.LittleEndian.Uint16(f[14:])
			if format != 1 || channels != 1 || rate != audio.SampleRate || bits != 16 {
				return nil, errors.New("need 16 kHz mono 16-bit PCM")
			}
			fmtOK = true
			if _, err := r.Seek(size-16+size%2, io.SeekCurrent); err != nil {
				return nil, err
			}
		case "data":
			if !fmtOK {
				return nil, errors.New("data before fmt")
			}
			return io.LimitReader(r, size), nil
		default:
			if _, err := r.Seek(size+size%2, io.SeekCurrent); err != nil {
				return nil, err
			}
		}
	}
}

func writeJSONL[T any](path string, vals []T) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := jsonEncoder(f)
	for _, v := range vals {
		if err := enc.Encode(v); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// History lists past sessions, newest first.
func (a *App) History() []HistoryItem {
	entries, err := os.ReadDir(config.SessionsDir())
	if err != nil {
		return nil
	}
	a.mu.Lock()
	running := ""
	if a.session != nil {
		running = a.session.meta.ID
	}
	a.mu.Unlock()
	var out []HistoryItem
	for _, e := range entries {
		if !e.IsDir() || !sessionIDRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(config.SessionsDir(), e.Name())
		var m sessionMeta
		if readJSON(filepath.Join(dir, metaFile), &m) != nil {
			continue
		}
		st := a.steps(dir, m)
		idle := m.ID != running
		out = append(out, HistoryItem{
			ID: m.ID, Date: m.Started.Format("02.01.2006 15:04"), Project: m.Project, MeetingNo: m.MeetingNo,
			Mode: m.Mode, Imported: m.Imported, ExcelPath: m.ExcelPath, SummaryPath: m.SummaryPath, Counts: m.Counts, Steps: st,
			CanTranscribe: idle && st.Recorded,
			CanGenerate:   idle && st.Transcribed,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// DeleteHistory removes a session's working files (audio, transcript). The
// Excel and transcript in the output folder belong to the user and stay.
func (a *App) DeleteHistory(id string) error {
	if !sessionIDRe.MatchString(id) {
		return i18n.Errorf(nil, "err.unexpected")
	}
	a.mu.Lock()
	running := a.session != nil && a.session.meta.ID == id
	a.mu.Unlock()
	if running {
		return i18n.Errorf(nil, "err.busy")
	}
	return os.RemoveAll(filepath.Join(config.SessionsDir(), id))
}

// cleanupOld deletes session working folders older than the retention period.
func (a *App) cleanupOld() {
	a.mu.Lock()
	days := a.cfg.Output.RetentionDays
	a.mu.Unlock()
	if days < 1 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	entries, _ := os.ReadDir(config.SessionsDir())
	for _, e := range entries {
		t, err := time.ParseInLocation("20060102-150405", e.Name(), time.Local)
		if err == nil && t.Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(config.SessionsDir(), e.Name()))
		}
	}
}
