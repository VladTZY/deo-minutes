package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/bot"
	"deo-minutes/internal/config"
	"deo-minutes/internal/export"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/minutes"
	"deo-minutes/internal/speakers"
	"deo-minutes/internal/stt"
)

// Files inside a session folder (%LOCALAPPDATA%\DEOMinutes\sessions\<id>).
const (
	metaFile       = "session.json"
	transcriptFile = "transcript.jsonl"
	audioFile      = "audio.pcm"
	marksFile      = "audio_marks.jsonl" // byte offset → capture time, per chunk
	rosterFile     = "roster.jsonl"      // who was speaking, every ~500 ms
	quietRMS       = 0.005
	maxMeeting     = 5 * time.Hour // hard stop, also caps disk use (~576 MB of audio)
	maxNames       = 200
)

// sessionMeta is persisted so every step can run (again) later.
type sessionMeta struct {
	ID           string    `json:"id"`
	Mode         string    `json:"mode"`
	Project      string    `json:"project"`
	MeetingNo    int       `json:"meeting_no"`
	PreparedBy   string    `json:"prepared_by"`
	Started      time.Time `json:"started"`
	Participants []string  `json:"participants"`
	BotName      string    `json:"bot_name"`
	Imported     string    `json:"imported,omitempty"` // file name when created from an imported recording
	ExcelPath    string    `json:"excel_path,omitempty"`
	Counts       Counts    `json:"counts"`
}

type transcriptLine struct {
	AtMS    int64  `json:"at_ms"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type rosterLine struct {
	TSMS     int64    `json:"ts_ms"`
	Speakers []string `json:"speakers"`
}

type session struct {
	meta   sessionMeta
	dir    string
	stopCh chan struct{}
	once   sync.Once
}

// stop asks the running session to leave the meeting and finish.
func (s *session) stop() { s.once.Do(func() { close(s.stopCh) }) }

var sessionIDRe = regexp.MustCompile(`^\d{8}-\d{6}$`)

// Start validates the form and starts recording a meeting in the background.
// PROD continues automatically to the Excel; DEMO stops after recording and
// waits for the Transcribe and Generate buttons.
func (a *App) Start(req StartRequest) error {
	link, err := bot.ParseLink(req.Link)
	if err != nil {
		return err
	}
	req.Project, req.PreparedBy = strings.TrimSpace(req.Project), strings.TrimSpace(req.PreparedBy)
	if req.Project == "" {
		return i18n.Errorf(nil, "err.project.empty")
	}
	if req.MeetingNo < 1 {
		return i18n.Errorf(nil, "err.meetingNo.invalid")
	}
	a.mu.Lock()
	mode := a.cfg.Mode
	a.mu.Unlock()
	var openSTT stt.Opener
	if mode == config.ModeProd {
		if openSTT, err = a.newTranscriber(); err != nil {
			return err
		}
	}

	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return i18n.Errorf(nil, "err.busy")
	}
	a.cfg.Last.Project, a.cfg.Last.PreparedBy = req.Project, req.PreparedBy
	if req.MeetingNo > a.cfg.Last.MeetingNoByProject[req.Project] {
		a.cfg.Last.MeetingNoByProject[req.Project] = req.MeetingNo
	}
	cfg := a.cfg
	s := newSession(sessionMeta{Mode: mode, Project: req.Project, MeetingNo: req.MeetingNo, PreparedBy: req.PreparedBy, BotName: cfg.Bot.DisplayName})
	a.session = s
	a.mu.Unlock()

	if err := config.Save(config.Path(), cfg); err != nil {
		slog.Warn("config save", "err", err)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		a.endSession(i18n.Errorf(err, "err.unexpected"))
		return nil
	}
	_ = writeJSON(filepath.Join(s.dir, metaFile), s.meta)

	ctx, cancel := context.WithCancel(a.ctx)
	a.setStatus(func(st *Status) {
		*st = Status{State: StateStartingBrowser, CanStop: true, SessionID: s.meta.ID, Mode: mode}
	})
	go func() {
		defer cancel()
		pwd := req.Password
		if pwd == "" {
			pwd = link.Password
		}
		a.run(ctx, s, cfg, bot.Options{
			Link:         link,
			Password:     pwd,
			DisplayName:  cfg.Bot.DisplayName,
			Email:        cfg.Bot.Email,
			BrowserPath:  cfg.Bot.BrowserPath,
			Headless:     cfg.Bot.Headless,
			ShowWindow:   !cfg.Bot.Headless, // unticking "Browser ascuns" shows the bot window
			LobbyTimeout: time.Duration(cfg.Bot.LobbyTimeoutMin) * time.Minute,
			Selectors:    bot.DefaultSelectors,
		}, openSTT)
	}()
	return nil
}

func newSession(meta sessionMeta) *session {
	now := time.Now()
	meta.ID, meta.Started = now.Format("20060102-150405"), now
	return &session{meta: meta, dir: filepath.Join(config.SessionsDir(), meta.ID), stopCh: make(chan struct{})}
}

// Stop leaves the meeting; what was recorded so far is kept.
func (a *App) Stop() error {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil {
		s.stop()
	}
	return nil
}

// Status returns the current session status.
func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *App) setStatus(f func(*Status)) {
	a.mu.Lock()
	f(&a.status)
	a.status.Label = i18n.T("state."+a.status.State, a.cfg.Bot.DisplayName)
	st := a.status
	a.mu.Unlock()
	if a.emit != nil {
		a.emit(EventStatus, st)
	}
}

func (a *App) warn(msg string) {
	a.setStatus(func(st *Status) {
		for _, w := range st.Warnings {
			if w == msg {
				return
			}
		}
		st.Warnings = append(st.Warnings, msg)
	})
}

// endSession clears the running session and, on error, shows it.
func (a *App) endSession(err error) {
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	if err != nil {
		slog.Error("session failed", "err", err)
		a.setStatus(func(st *Status) { st.State, st.Error, st.CanStop, st.Progress = StateError, userMessage(err), false, 0 })
	}
}

func (a *App) run(ctx context.Context, s *session, cfg config.Config, opts bot.Options, openSTT stt.Opener) {
	opts.OnState = func(b bot.State) {
		switch b {
		case bot.StateConnecting:
			a.setStatus(func(st *Status) { st.State = StateConnecting })
		case bot.StateLobby:
			a.setStatus(func(st *Status) { st.State = StateLobby })
		}
	}
	joinCtx, joinCancel := context.WithCancel(ctx)
	defer joinCancel()
	go func() { // Stop pressed while still joining
		select {
		case <-s.stopCh:
			joinCancel()
		case <-joinCtx.Done():
		}
	}()
	bs, err := bot.Join(joinCtx, opts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.endSession(nil)
			a.setStatus(func(st *Status) { *st = Status{State: StateIdle} })
			return
		}
		a.endSession(err)
		return
	}

	participants, err := a.record(ctx, s, cfg, bs, openSTT)
	if err != nil {
		a.endSession(err)
		return
	}
	s.meta.Participants = participants
	_ = writeJSON(filepath.Join(s.dir, metaFile), s.meta)

	if s.meta.Mode == config.ModeProd {
		a.setStatus(func(st *Status) { st.Steps = a.steps(s.dir, s.meta) }) // recorded + transcribed live
		if !cfg.Output.KeepAudio {
			_ = os.Remove(filepath.Join(s.dir, audioFile))
		}
		a.generate(ctx, s)
		return
	}
	// DEMO: stop here; the user runs Transcribe, then Generate.
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	a.setStatus(func(st *Status) {
		st.State, st.CanStop = StateRecorded, false
		st.Steps = a.steps(s.dir, s.meta)
	})
}

// record captures audio and speaker snapshots to disk until the meeting
// ends, Stop is pressed, the meeting is silent too long or hits maxMeeting.
// In PROD it also transcribes live with Amazon Transcribe.
func (a *App) record(ctx context.Context, s *session, cfg config.Config, bs *bot.Session, openSTT stt.Opener) ([]string, error) {
	recStart := time.Now()
	a.setStatus(func(st *Status) { st.State, st.StartedAt = StateRecording, recStart.UnixMilli() })

	files := map[string]*os.File{}
	for _, name := range []string{audioFile, marksFile, rosterFile} {
		f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			_ = bs.Leave()
			return nil, i18n.Errorf(err, "err.unexpected")
		}
		defer func() { _ = f.Close() }()
		files[name] = f
	}
	pcm, marks, rosterOut := files[audioFile], json.NewEncoder(files[marksFile]), json.NewEncoder(files[rosterFile])
	var written int64

	live := openSTT != nil
	var chunks chan stt.Chunk
	var liveDone <-chan error
	tl := speakers.NewTimeline(cfg.Bot.DisplayName)
	if live {
		chunks = make(chan stt.Chunk, 256)
		liveDone = a.liveTranscribe(ctx, s, cfg, tl, openSTT, chunks, recStart)
	}

	names := map[string]bool{}
	botName := strings.ToLower(strings.TrimSpace(cfg.Bot.DisplayName))
	lastSound := time.Now()
	silence := time.Duration(cfg.Bot.SilenceEndMin) * time.Minute
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	leave := false

loop:
	for {
		select {
		case c := <-bs.Audio():
			_ = marks.Encode(stt.Mark{Off: written, TS: c.TS})
			n, _ := pcm.Write(c.PCM)
			written += int64(n)
			if audio.RMS(c.PCM) >= quietRMS {
				lastSound = time.Now()
			}
			if live {
				select {
				case chunks <- stt.Chunk{TS: c.TS, PCM: c.PCM}:
				case <-ctx.Done():
					break loop
				}
			}
		case r := <-bs.RosterUpdates():
			tl.Add(r.TS, r.Speakers)
			_ = rosterOut.Encode(rosterLine{TSMS: r.TS.UnixMilli(), Speakers: r.Speakers})
			changed := false
			for _, n := range r.Participants {
				if n != "" && strings.ToLower(n) != botName && !names[n] && len(names) < maxNames {
					names[n], changed = true, true
				}
			}
			if changed {
				a.setStatus(func(st *Status) { st.Participants = len(names) })
			}
		case <-tick.C:
			if time.Since(recStart) > maxMeeting {
				slog.Warn("maximum meeting length reached")
				a.warn(i18n.T("warn.maxLength"))
				leave = true
				break loop
			}
			if silence > 0 && time.Since(lastSound) > silence {
				slog.Info("ending after silence")
				leave = true
				break loop
			}
		case <-bs.Done():
			break loop
		case <-s.stopCh:
			leave = true
			break loop
		case <-ctx.Done():
			leave = true
			break loop
		}
	}
	if leave {
		_ = bs.Leave()
	} else {
		_ = bs.Close()
	}
	if live {
		a.setStatus(func(st *Status) { st.State, st.CanStop = StateFinishingTranscript, false })
		close(chunks)
		if err := <-liveDone; err != nil {
			return nil, err
		}
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	return list, nil
}

// liveTranscribe streams chunks to Amazon Transcribe (PROD) and writes the
// transcript incrementally. The returned channel yields the final error.
func (a *App) liveTranscribe(ctx context.Context, s *session, cfg config.Config, tl *speakers.Timeline, openSTT stt.Opener, chunks <-chan stt.Chunk, t0 time.Time) <-chan error {
	done := make(chan error, 1)
	streamer := &stt.Streamer{
		Open:        openSTT,
		Rotate:      time.Duration(cfg.Transcribe.RotateMin) * time.Minute,
		OnReconnect: func(error) { a.warn(i18n.T("err.transcribe.network")) },
	}
	words, wait := streamer.Run(ctx, chunks)
	go func() {
		tw, err := newTranscriptWriter(filepath.Join(s.dir, transcriptFile), t0, a.emit)
		if err != nil {
			for range words {
			}
			done <- i18n.Errorf(err, "err.unexpected")
			return
		}
		b := &speakers.Builder{TL: tl}
		for w := range words {
			tw.write(b.Add(w))
		}
		tw.write(b.Flush())
		_ = tw.close()
		err = wait()
		switch {
		case err == nil || errors.Is(err, context.Canceled):
			done <- nil
		case errors.Is(err, stt.ErrFatal):
			done <- i18n.Errorf(err, "err.transcribe.auth")
		default:
			done <- i18n.Errorf(err, "err.transcribe.failed", "conexiune eșuată")
		}
	}()
	return done
}

// transcriptWriter writes lines with times relative to the recording start.
type transcriptWriter struct {
	f    *os.File
	t0   time.Time
	emit func(string, any)
}

func newTranscriptWriter(path string, t0 time.Time, emit func(string, any)) (*transcriptWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &transcriptWriter{f: f, t0: t0, emit: emit}, nil
}

func (w *transcriptWriter) write(l *speakers.Line) {
	if l == nil {
		return
	}
	b, _ := json.Marshal(transcriptLine{AtMS: l.Start.Sub(w.t0).Milliseconds(), Speaker: l.Speaker, Text: l.Text})
	_, _ = w.f.Write(append(b, '\n'))
	if w.emit != nil {
		w.emit(EventTranscript, l.Format(w.t0))
	}
}

func (w *transcriptWriter) close() error { return w.f.Close() }

// generate turns the saved transcript into the Excel workbook.
func (a *App) generate(ctx context.Context, s *session) {
	a.setStatus(func(st *Status) { st.State, st.CanStop, st.Progress = StateGenerating, false, 0 })
	t, err := readTranscript(filepath.Join(s.dir, transcriptFile))
	if err != nil || len(t) == 0 {
		a.endSession(i18n.Errorf(err, "warn.transcriptEmpty"))
		return
	}
	factory := a.llmFactory
	if factory == nil {
		factory = a.newLLM
	}
	client, err := factory(ctx)
	if err != nil {
		a.endSession(err)
		return
	}
	x := &minutes.Extractor{LLM: client}
	date := s.meta.Started
	recs, rep, err := x.Extract(ctx, minutes.MeetingInfo{Project: s.meta.Project, Date: date, Participants: s.meta.Participants}, t)
	if err != nil {
		a.endSession(err)
		return
	}
	if rep.Dropped > 0 {
		a.warn(i18n.T("warn.recordsDropped", rep.Dropped))
	}

	a.setStatus(func(st *Status) { st.State = StateSaving })
	m := export.Meeting{Project: s.meta.Project, No: s.meta.MeetingNo, Date: date, PreparedBy: s.meta.PreparedBy}
	for _, p := range s.meta.Participants {
		m.Participants = append(m.Participants, export.Participant{Name: p, Attendance: "Call"})
	}
	xlsx, err := export.Render(m, recs)
	if err != nil {
		a.endSession(i18n.Errorf(err, "err.excel.write", err.Error()))
		return
	}
	a.mu.Lock()
	outDir := a.cfg.Output.Dir
	a.mu.Unlock()
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		a.endSession(i18n.Errorf(err, "err.excel.write", err.Error()))
		return
	}
	base, err := export.UniqueBase(outDir, export.BaseName(date, s.meta.Project, s.meta.MeetingNo))
	if err != nil {
		a.endSession(i18n.Errorf(err, "err.excel.write", err.Error()))
		return
	}
	excelPath := filepath.Join(outDir, base+".xlsx")
	txtPath := filepath.Join(outDir, base+"_transcriere.txt")
	if err := export.WriteFileAtomic(excelPath, xlsx); err != nil {
		a.endSession(i18n.Errorf(err, "err.excel.write", err.Error()))
		return
	}
	if err := export.WriteFileAtomic(txtPath, []byte(strings.ReplaceAll(t.Render(), "\n", "\r\n"))); err != nil {
		slog.Warn("transcript write", "err", err)
	}
	counts := Counts{Actions: len(recs.Actions), Decisions: len(recs.Decisions), Risks: len(recs.Risks),
		Interdependencies: len(recs.Interdependencies), Changes: len(recs.Changes)}
	s.meta.ExcelPath, s.meta.Counts = excelPath, counts
	_ = writeJSON(filepath.Join(s.dir, metaFile), s.meta)

	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	a.setStatus(func(st *Status) {
		st.State = StateDone
		st.Result = &Result{ExcelPath: excelPath, TranscriptPath: txtPath, Folder: outDir, Counts: counts}
		st.Steps = a.steps(s.dir, s.meta)
	})
}

func readTranscript(path string) (minutes.Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var t minutes.Transcript
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var l transcriptLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // a torn last line after a crash
		}
		t = append(t, minutes.Line{At: time.Duration(l.AtMS) * time.Millisecond, Speaker: l.Speaker, Text: l.Text})
	}
	return t, sc.Err()
}

// readJSONL decodes one value per line, skipping torn lines.
func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var v T
		if json.Unmarshal(sc.Bytes(), &v) == nil {
			out = append(out, v)
		}
	}
	return out, sc.Err()
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return export.WriteFileAtomic(path, b)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}
