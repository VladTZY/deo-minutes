package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"deo-minutes/internal/config"
	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
	"deo-minutes/internal/minutes"
)

func TestEveryStateHasLabel(t *testing.T) {
	for _, s := range []string{StateIdle, StateStartingBrowser, StateConnecting, StateLobby, StateRecording,
		StateFinishingTranscript, StateGenerating, StateSaving, StateDone, StateError} {
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

func TestResumeWritesExcelAndHistory(t *testing.T) {
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
	if len(h) != 1 || !h[0].CanResume || h[0].ExcelPath != "" {
		t.Fatalf("history before: %+v", h)
	}
	if err := a.Resume(id); err != nil {
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
	if len(h) != 1 || h[0].CanResume || h[0].ExcelPath != st.Result.ExcelPath || h[0].Counts.Actions != 1 {
		t.Fatalf("history after: %+v", h)
	}
	// A second resume never overwrites the first workbook.
	if err := a.Resume(id); err != nil {
		t.Fatal(err)
	}
	st2 := waitState(t, statuses, StateDone, StateError)
	if st2.Result == nil || st2.Result.ExcelPath == st.Result.ExcelPath {
		t.Fatalf("second run must use a new file: %+v", st2.Result)
	}
}

func TestResumeWithoutGatewayShowsRomanianError(t *testing.T) {
	a, statuses := newTestApp(t)
	keyring.MockInit()
	writeSession(t, "20261027-110000")
	if err := a.Resume("20261027-110000"); err != nil {
		t.Fatal(err)
	}
	st := waitState(t, statuses, StateError, StateDone)
	if st.State != StateError || st.Error != i18n.T("err.gateway.notConfigured") {
		t.Fatalf("status: %+v", st)
	}
	if !a.History()[0].CanResume {
		t.Error("failed session must stay resumable")
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
		{StartRequest{Link: "https://a.webex.com/meet/x", Project: "P", MeetingNo: 1}, "err.transcribe.creds"},
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
	if err := a.Resume("../../etc"); err == nil {
		t.Error("must refuse bad session ids")
	}
	if err := a.DeleteHistory("../x"); err == nil {
		t.Error("must refuse bad session ids")
	}
	if !within(filepath.Join(a.cfg.Output.Dir, "a.xlsx"), a.cfg.Output.Dir) || within(a.cfg.Output.Dir+"-evil/a", a.cfg.Output.Dir) {
		t.Error("within() wrong")
	}
}
