package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"deo-minutes/internal/audio"
	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
	"deo-minutes/internal/minutes"
)

func TestEveryStateHasLabel(t *testing.T) {
	for _, s := range []string{StateIdle, StateStartingBrowser, StateConnecting, StateLobby, StateRecording,
		StateFinishingTranscript, StateRecorded, StateTranscribing, StateTranscribed, StateGenerating, StateSaving, StateDone, StateError} {
		if !i18n.Has("state." + s) {
			t.Errorf("ro.json lacks state.%s", s)
		}
	}
}

func newTestApp(t *testing.T) (*App, chan Status) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)           // macOS/Linux: UserCacheDir under HOME
	t.Setenv("LOCALAPPDATA", home)   // Windows
	t.Setenv("XDG_CACHE_HOME", home) // Linux
	statuses := make(chan Status, 100)
	a := New("test")
	a.ctx = context.Background()
	a.cfg = config.Defaults()
	a.cfg.Output.Dir = filepath.Join(home, "Documents", "DEO Minute")
	a.emit = func(event string, data any) {
		if st, ok := data.(Status); ok && event == EventStatus {
			statuses <- st
		}
	}
	return a, statuses
}

func waitState(t *testing.T, ch chan Status, want ...string) Status {
	t.Helper()
	timeout := time.After(20 * time.Second)
	for {
		select {
		case st := <-ch:
			for _, w := range want {
				if st.State == w {
					return st
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %v", want)
		}
	}
}

func writeSession(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join(config.SessionsDir(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	started, _ := time.ParseInLocation("20060102-150405", id, time.Local)
	if err := writeJSON(filepath.Join(dir, metaFile), sessionMeta{ID: id, Project: "Proiect Test", MeetingNo: 3,
		PreparedBy: "Maria", Started: started, Participants: []string{"Ana Pop", "Ion Ionescu"}}); err != nil {
		t.Fatal(err)
	}
	lines := `{"at_ms":120000,"speaker":"Ion Ionescu","text":"Ion, te rog să trimiți raportul de buget până vineri."}
{"at_ms":180000,"speaker":"Ana Pop","text":"Am decis să mutăm lansarea în decembrie."}
{"at_ms":` // torn last line from a crash must be ignored
	if err := os.WriteFile(filepath.Join(dir, transcriptFile), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateWritesExcelAndHistory(t *testing.T) {
	a, statuses := newTestApp(t)
	id := "20261027-100000"
	writeSession(t, id)
	a.llmFactory = func(context.Context) (llm.Client, error) {
		return llm.Func(func(_ context.Context, req llm.Request) ([]byte, error) {
			if _, ok := req.Schema["properties"].(map[string]any)["actions"]; ok {
				return json.Marshal(minutes.Records{Actions: []minutes.ActionItem{{Key: "a1", Title: "Raport buget",
					Evidence: []minutes.Evidence{{At: "00:02:00", Speaker: "Ion Ionescu", Quote: "trimiți raportul de buget până vineri"}}}}})
			}
			return []byte(`{}`), nil
		}), nil
	}

	h := a.History()
	if len(h) != 1 || !h[0].CanGenerate || h[0].CanTranscribe || h[0].ExcelPath != "" {
		t.Fatalf("history before: %+v", h)
	}
	if err := a.Generate(id); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, statuses, StateDone, StateError)
	if st.State != StateDone || st.Result == nil || st.Result.Counts.Actions != 1 {
		t.Fatalf("status: %+v", st)
	}
	if filepath.Base(st.Result.ExcelPath) != "261027_Proiect Test_Minuta_3.xlsx" {
		t.Errorf("excel name %q", st.Result.ExcelPath)
	}
	for _, p := range []string{st.Result.ExcelPath, st.Result.TranscriptPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	txt, _ := os.ReadFile(st.Result.TranscriptPath)
	if !strings.Contains(string(txt), "[00:02:00] Ion Ionescu: Ion, te rog") || !strings.Contains(string(txt), "\r\n") {
		t.Errorf("transcript txt: %q", txt)
	}
	h = a.History()
	if len(h) != 1 || !h[0].Steps.Generated || h[0].ExcelPath != st.Result.ExcelPath || h[0].Counts.Actions != 1 {
		t.Fatalf("history after: %+v", h)
	}
	// Generating again never overwrites the first workbook.
	if err := a.Generate(id); err != nil {
		t.Fatal(err)
	}
	st2 := waitState(t, statuses, StateDone, StateError)
	if st2.Result == nil || st2.Result.ExcelPath == st.Result.ExcelPath {
		t.Fatalf("second run must use a new file: %+v", st2.Result)
	}
}

func TestGenerateWithoutGatewayShowsRomanianError(t *testing.T) {
	a, statuses := newTestApp(t)
	keyring.MockInit()
	a.cfg.Mode = config.ModeProd
	writeSession(t, "20261027-110000")
	if err := a.Generate("20261027-110000"); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, statuses, StateError, StateDone)
	if st.State != StateError || st.Error != i18n.T("err.gateway.notConfigured") {
		t.Fatalf("status: %+v", st)
	}
	if !a.History()[0].CanGenerate {
		t.Error("a failed generation must stay retryable")
	}
}

func TestStartValidation(t *testing.T) {
	a, _ := newTestApp(t)
	keyring.MockInit()
	cases := []struct {
		req StartRequest
		key string
	}{
		{StartRequest{Link: "https://evil.com/x", Project: "P", MeetingNo: 1}, "err.link.invalid"},
		{StartRequest{Link: "https://a.webex.com/meet/x", Project: " ", MeetingNo: 1}, "err.project.empty"},
		{StartRequest{Link: "https://a.webex.com/meet/x", Project: "P", MeetingNo: 0}, "err.meetingNo.invalid"},
	}
	for _, c := range cases {
		err := a.Start(c.req)
		var ue *i18n.UserError
		if !errors.As(err, &ue) || ue.Key != c.key {
			t.Errorf("%+v: got %v, want %s", c.req, err, c.key)
		}
	}
}

func TestPathSafety(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.OpenPath("/etc/passwd"); err == nil {
		t.Error("must refuse paths outside the app's folders")
	}
	if err := a.Generate("../../etc"); err == nil {
		t.Error("must refuse bad session ids")
	}
	if err := a.DeleteHistory("../x"); err == nil {
		t.Error("must refuse bad session ids")
	}
	if !within(filepath.Join(a.cfg.Output.Dir, "a.xlsx"), a.cfg.Output.Dir) || within(a.cfg.Output.Dir+"-evil/a", a.cfg.Output.Dir) {
		t.Error("within() wrong")
	}
}

func TestOpenPathRefusesPrograms(t *testing.T) {
	a, _ := newTestApp(t)
	_ = os.MkdirAll(a.cfg.Output.Dir, 0o700)
	exe := filepath.Join(a.cfg.Output.Dir, "evil.exe")
	_ = os.WriteFile(exe, []byte("x"), 0o600)
	if err := a.OpenPath(exe); err == nil {
		t.Fatal("must not launch programs, even inside the output folder")
	}
}

func TestStartInProdNeedsAWS(t *testing.T) {
	a, _ := newTestApp(t)
	keyring.MockInit()
	a.cfg.Mode = config.ModeProd
	err := a.Start(StartRequest{Link: "https://a.webex.com/meet/x", Project: "P", MeetingNo: 1})
	var ue *i18n.UserError
	if !errors.As(err, &ue) || ue.Key != "err.transcribe.creds" {
		t.Fatalf("got %v", err)
	}
}

func TestDemoWithoutOpenRouterKey(t *testing.T) {
	a, statuses := newTestApp(t)
	keyring.MockInit()
	a.cfg.Mode = config.ModeDemo
	writeSession(t, "20261027-120000")
	if err := a.Generate("20261027-120000"); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, statuses, StateError, StateDone)
	if st.Error != i18n.T("err.openrouter.notConfigured") {
		t.Fatalf("status: %+v", st)
	}
}

// fakeWhisper writes a shell script that behaves like whisper-cli: it reads
// -of and writes a JSON transcript with one word per segment.
func fakeWhisper(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	p := filepath.Join(t.TempDir(), "whisper-cli")
	script := `#!/bin/sh
while [ $# -gt 0 ]; do case "$1" in -of) out="$2"; shift;; esac; shift; done
cat > "$out.json" <<'J'
{"transcription":[
 {"offsets":{"from":500,"to":900},"text":" [S1] Ion,"},
 {"offsets":{"from":900,"to":1300},"text":" trimite"},
 {"offsets":{"from":1300,"to":1700},"text":" raportul"},
 {"offsets":{"from":1700,"to":2100},"text":" de"},
 {"offsets":{"from":2100,"to":2500},"text":" buget"},
 {"offsets":{"from":2500,"to":2900},"text":" mâine"},
 {"offsets":{"from":2900,"to":3000},"text":"."}
]}
J
`
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDemoStepsImportTranscribeGenerate(t *testing.T) {
	a, statuses := newTestApp(t)
	a.cfg.Mode = config.ModeDemo
	model := filepath.Join(t.TempDir(), "model.bin")
	_ = os.WriteFile(model, []byte("x"), 0o600)
	a.cfg.LocalSTT.WhisperExe, a.cfg.LocalSTT.Model = fakeWhisper(t), model

	wav := filepath.Join(t.TempDir(), "in.wav")
	w, _ := audio.CreateWAV(wav)
	_, _ = w.Write(make([]byte, 4*audio.BytesPerSecond))
	_ = w.Close()

	id, err := a.importWAV(wav, "Proiect Demo", 2)
	if err != nil {
		t.Fatal(err)
	}
	if h := a.History(); len(h) != 1 || !h[0].CanTranscribe || h[0].CanGenerate || h[0].Imported != "in.wav" {
		t.Fatalf("after import: %+v", h)
	}
	if err := a.Transcribe(id); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, statuses, StateTranscribed, StateError)
	if st.State != StateTranscribed || !st.Steps.Transcribed || st.Progress != 1 {
		t.Fatalf("transcribe: %+v", st)
	}
	lines, err := a.Transcript(id)
	if err != nil || len(lines) != 1 || lines[0] != "[00:00:00] Participant necunoscut: Ion, trimite raportul de buget mâine." {
		t.Fatalf("transcript: %q %v", lines, err)
	}

	a.llmFactory = func(context.Context) (llm.Client, error) {
		return llm.Func(func(_ context.Context, req llm.Request) ([]byte, error) {
			if _, ok := req.Schema["properties"].(map[string]any)["actions"]; ok {
				return json.Marshal(minutes.Records{Actions: []minutes.ActionItem{{Key: "a1", Title: "Raport buget",
					Evidence: []minutes.Evidence{{At: "00:00:00", Speaker: "Participant necunoscut", Quote: "trimite raportul de buget mâine"}}}}})
			}
			return []byte(`{}`), nil
		}), nil
	}
	if err := a.Generate(id); err != nil {
		t.Fatal(err)
	}
	st = waitState(t, statuses, StateDone, StateError)
	if st.State != StateDone || st.Result.Counts.Actions != 1 || !st.Steps.Generated {
		t.Fatalf("generate: %+v", st)
	}
}

func TestImportRejectsWrongFormat(t *testing.T) {
	a, _ := newTestApp(t)
	p := filepath.Join(t.TempDir(), "x.wav")
	_ = os.WriteFile(p, []byte("RIFF\x00\x00\x00\x00WAVEnope"), 0o600)
	if _, err := a.importWAV(p, "P", 1); err == nil {
		t.Fatal("must reject a non-16 kHz/garbage WAV")
	}
}

func TestModelDownloadResumesAndVerifies(t *testing.T) {
	a, _ := newTestApp(t)
	content := []byte(strings.Repeat("model-bytes-", 5000))
	sum := sha256.Sum256(content)
	var ranges []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		http.ServeContent(w, r, "m.bin", time.Time{}, bytes.NewReader(content))
	}))
	defer ts.Close()
	oldURL, oldSHA, oldSize := modelURL, modelSHA256, modelSize
	t.Cleanup(func() { modelURL, modelSHA256, modelSize = oldURL, oldSHA, oldSize })
	modelURL, modelSHA256, modelSize = ts.URL, hex.EncodeToString(sum[:]), int64(len(content))

	dir := filepath.Join(config.AppDataDir(), "models")
	_ = os.MkdirAll(dir, 0o700)
	part := filepath.Join(dir, modelFileName+".partial")
	_ = os.WriteFile(part, content[:1000], 0o600) // an interrupted earlier download

	if err := a.downloadModel(context.Background(), modelURL); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, modelFileName))
	if !bytes.Equal(got, content) || len(ranges) != 1 || ranges[0] != "bytes=1000-" {
		t.Fatalf("resume failed: %d bytes, ranges %v", len(got), ranges)
	}
	if !a.LocalStatus().ModelFound {
		t.Error("downloaded model not found by LocalStatus")
	}

	// A corrupt download is rejected and removed.
	_ = os.Remove(filepath.Join(dir, modelFileName))
	modelSHA256 = strings.Repeat("0", 64)
	if err := a.downloadModel(context.Background(), modelURL); err == nil {
		t.Fatal("checksum mismatch must fail")
	}
	if _, err := os.Stat(part); err == nil {
		t.Error("corrupt partial must be deleted")
	}
}
