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

	"github.com/anthropics/anthropic-sdk-go"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/bot"
	"deo-minutes/internal/config"
	"deo-minutes/internal/export"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
	"deo-minutes/internal/minutes"
	"deo-minutes/internal/secrets"
	"deo-minutes/internal/speakers"
	"deo-minutes/internal/stt"
)

// Files inside a session folder (%LOCALAPPDATA%\DEOMinutes\sessions\<id>).
const (
	metaFile       = "session.json"
	transcriptFile = "transcript.jsonl"
	audioFile      = "audio.pcm"
	quietRMS       = 0.005
)

// sessionMeta is persisted so a crashed or failed session can be resumed.
type sessionMeta struct {
	ID           string    `json:"id"`
	Project      string    `json:"project"`
	MeetingNo    int       `json:"meeting_no"`
	PreparedBy   string    `json:"prepared_by"`
	Started      time.Time `json:"started"`
	Participants []string  `json:"participants"`
	ExcelPath    string    `json:"excel_path,omitempty"`
	Counts       Counts    `json:"counts"`
}

type transcriptLine struct {
	AtMS    int64  `json:"at_ms"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type session struct {
	meta   sessionMeta
	dir    string
	cancel context.CancelFunc
	stopCh chan struct{}
	once   sync.Once
}

// stop asks the running session to leave the meeting and finish.
func (s *session) stop() { s.once.Do(func() { close(s.stopCh) }) }

var sessionIDRe = regexp.MustCompile(`^\d{8}-\d{6}$`)

// Start validates the form and starts a meeting session in the background.
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
	openSTT, err := a.newTranscriber()
	if err != nil {
		return err
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
	now := time.Now()
	s := &session{
		meta:   sessionMeta{ID: now.Format("20060102-150405"), Project: req.Project, MeetingNo: req.MeetingNo, PreparedBy: req.PreparedBy, Started: now},
		stopCh: make(chan struct{}),
	}
	s.dir = filepath.Join(config.SessionsDir(), s.meta.ID)
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
	s.cancel = cancel
	a.setStatus(func(st *Status) { *st = Status{State: StateStartingBrowser, CanStop: true} })
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
			LobbyTimeout: time.Duration(cfg.Bot.LobbyTimeoutMin) * time.Minute,
			Selectors:    bot.DefaultSelectors,
		}, openSTT)
	}()
	return nil
}

// Stop leaves the meeting; the minutes are still generated from what was captured.
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

// endSession clears the running session and sets the final state.
func (a *App) endSession(err error) {
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	if err != nil {
		slog.Error("session failed", "err", err)
		a.setStatus(func(st *Status) { st.State, st.Error, st.CanStop = StateError, userMessage(err), false })
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
	go func() { // Stop pressed while still joining
		select {
		case <-s.stopCh:
			joinCancel()
		case <-joinCtx.Done():
		}
	}()
	bs, err := bot.Join(joinCtx, opts)
	if err != nil {
		joinCancel()
		if errors.Is(err, context.Canceled) {
			a.endSession(nil)
			a.setStatus(func(st *Status) { *st = Status{State: StateIdle} })
			return
		}
		a.endSession(err)
		return
	}
	// The join context only guarded the join; the session lives on ctx.
	defer joinCancel()

	participants, err := a.record(ctx, s, cfg, bs, openSTT)
	if err != nil {
		a.endSession(err)
		return
	}
	s.meta.Participants = participants
	_ = writeJSON(filepath.Join(s.dir, metaFile), s.meta)
	if !cfg.Output.KeepAudio {
		_ = os.Remove(filepath.Join(s.dir, audioFile))
	}
	a.finish(ctx, s)
}

// record captures audio, transcribes it live and tracks speakers until the
// meeting ends, Stop is pressed, or the meeting is silent for too long.
func (a *App) record(ctx context.Context, s *session, cfg config.Config, bs *bot.Session, openSTT stt.Opener) ([]string, error) {
	recStart := time.Now()
	a.setStatus(func(st *Status) { st.State, st.StartedAt = StateRecording, recStart.UnixMilli() })

	pcm, err := os.OpenFile(filepath.Join(s.dir, audioFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = bs.Leave()
		return nil, i18n.Errorf(err, "err.unexpected")
	}
	defer func() { _ = pcm.Close() }()
	tf, err := os.OpenFile(filepath.Join(s.dir, transcriptFile), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = bs.Leave()
		return nil, i18n.Errorf(err, "err.unexpected")
	}
	defer func() { _ = tf.Close() }()

	tl := speakers.NewTimeline(cfg.Bot.DisplayName)
	builder := &speakers.Builder{TL: tl}
	streamer := &stt.Streamer{
		Open:        openSTT,
		Rotate:      time.Duration(cfg.Transcribe.RotateMin) * time.Minute,
		OnReconnect: func(error) { a.warn(i18n.T("err.transcribe.network")) },
	}
	sttCtx, sttCancel := context.WithCancel(ctx)
	defer sttCancel()
	chunks := make(chan stt.Chunk, 256)
	words, wait := streamer.Run(sttCtx, chunks)

	var t0 time.Time // meeting time zero: first audio chunk
	var t0Mu sync.Mutex
	getT0 := func() time.Time { t0Mu.Lock(); defer t0Mu.Unlock(); return t0 }
	writeLine := func(l *speakers.Line) {
		if l == nil {
			return
		}
		tlx := transcriptLine{AtMS: l.Start.Sub(getT0()).Milliseconds(), Speaker: l.Speaker, Text: l.Text}
		b, _ := json.Marshal(tlx)
		_, _ = tf.Write(append(b, '\n'))
		if a.emit != nil {
			a.emit(EventTranscript, l.Format(getT0()))
		}
	}
	wordsDone := make(chan struct{})
	go func() {
		defer close(wordsDone)
		for w := range words {
			writeLine(builder.Add(w))
		}
		writeLine(builder.Flush())
	}()

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
			t0Mu.Lock()
			if t0.IsZero() {
				t0 = c.TS
			}
			t0Mu.Unlock()
			_, _ = pcm.Write(c.PCM)
			if audio.RMS(c.PCM) >= quietRMS {
				lastSound = time.Now()
			}
			select {
			case chunks <- stt.Chunk{TS: c.TS, PCM: c.PCM}:
			case <-ctx.Done():
				break loop
			}
		case r := <-bs.RosterUpdates():
			tl.Add(r.TS, r.Speakers)
			changed := false
			for _, n := range r.Participants {
				if n != "" && strings.ToLower(n) != botName && !names[n] {
					names[n], changed = true, true
				}
			}
			if changed {
				a.setStatus(func(st *Status) { st.Participants = len(names) })
			}
		case <-tick.C:
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
	a.setStatus(func(st *Status) { st.State, st.CanStop = StateFinishingTranscript, false })
	close(chunks)
	<-wordsDone
	if err := wait(); err != nil && !errors.Is(err, context.Canceled) {
		if errors.Is(err, stt.ErrFatal) {
			return nil, i18n.Errorf(err, "err.transcribe.auth")
		}
		return nil, i18n.Errorf(err, "err.transcribe.failed", "conexiune eșuată")
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	return list, nil
}

// finish turns the saved transcript into the Excel workbook.
func (a *App) finish(ctx context.Context, s *session) {
	a.setStatus(func(st *Status) { st.State, st.CanStop = StateGenerating, false })
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
	})
}

// Resume generates the Excel for a past session whose transcript exists
// (e.g. the gateway failed or the app was closed during generation).
func (a *App) Resume(id string) error {
	if !sessionIDRe.MatchString(id) {
		return i18n.Errorf(nil, "err.unexpected")
	}
	dir := filepath.Join(config.SessionsDir(), id)
	var meta sessionMeta
	if err := readJSON(filepath.Join(dir, metaFile), &meta); err != nil {
		return i18n.Errorf(err, "err.unexpected")
	}
	a.mu.Lock()
	if a.session != nil {
		a.mu.Unlock()
		return i18n.Errorf(nil, "err.busy")
	}
	s := &session{meta: meta, dir: dir, stopCh: make(chan struct{})}
	a.session = s
	a.mu.Unlock()
	a.setStatus(func(st *Status) { *st = Status{State: StateGenerating} })
	go a.finish(a.ctx, s)
	return nil
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
		var m sessionMeta
		if readJSON(filepath.Join(config.SessionsDir(), e.Name(), metaFile), &m) != nil {
			continue
		}
		_, terr := os.Stat(filepath.Join(config.SessionsDir(), e.Name(), transcriptFile))
		out = append(out, HistoryItem{
			ID: m.ID, Date: m.Started.Format("02.01.2006 15:04"), Project: m.Project, MeetingNo: m.MeetingNo,
			ExcelPath: m.ExcelPath, Counts: m.Counts,
			CanResume: m.ExcelPath == "" && terr == nil && m.ID != running,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// DeleteHistory removes a session's working files (transcript, audio). The
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

func (a *App) newLLM(ctx context.Context) (llm.Client, error) {
	a.mu.Lock()
	g := a.cfg.Gateway
	a.mu.Unlock()
	cfg := llm.Config{BaseURL: g.BaseURL, Region: g.Region, Model: g.ModelID, FallbackModel: g.FallbackModel,
		Auth: g.Auth, Effort: anthropic.OutputConfigEffort(g.Effort)}
	switch g.Auth {
	case llm.AuthSigV4:
		creds, err := secrets.GetAWS()
		if err != nil {
			return nil, i18n.Errorf(err, "err.gateway.notConfigured")
		}
		cfg.AWS = creds
	default:
		key, err := secrets.Get(secrets.Gateway)
		if err != nil {
			return nil, i18n.Errorf(err, "err.gateway.notConfigured")
		}
		cfg.Key = key
	}
	return llm.NewAnthropic(ctx, cfg)
}

func (a *App) newTranscriber() (stt.Opener, error) {
	creds, err := secrets.GetAWS()
	if err != nil {
		return nil, i18n.Errorf(err, "err.transcribe.creds")
	}
	a.mu.Lock()
	t := a.cfg.Transcribe
	a.mu.Unlock()
	return stt.NewTranscribeOpener(stt.TranscribeConfig{Region: t.Region, Language: t.Language, MultiLanguage: t.MultiLanguage}, creds), nil
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
