package export

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"deo-minutes/internal/minutes"
)

var meetingDate = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func ev(i int) []minutes.Evidence {
	return []minutes.Evidence{{At: "00:12:34", Speaker: "Ion Popescu", Quote: fmt.Sprintf("citat %d cu diacritice ăâîșț", i)}}
}

// bigRecords exceeds every table's template capacity (S4).
func bigRecords() (Meeting, minutes.Records) {
	m := Meeting{Project: "Proiect Ștefan/Test", No: 7, Date: meetingDate, PreparedBy: "Maria Ionescu"}
	for i := 1; i <= 50; i++ {
		m.Participants = append(m.Participants, Participant{Name: fmt.Sprintf("Participant %d", i), Attendance: "Call"})
	}
	var r minutes.Records
	for i := 1; i <= 60; i++ {
		r.Actions = append(r.Actions, minutes.ActionItem{Key: fmt.Sprintf("a%d", i), Title: fmt.Sprintf("Acțiune %d", i),
			Description: "Descriere", Deadline: "2026-11-06", Responsible: "Ion Popescu", Status: "Open", Evidence: ev(i)})
	}
	for i := 1; i <= 15; i++ {
		r.Risks = append(r.Risks, minutes.Risk{Key: fmt.Sprintf("r%d", i), TitleDescription: fmt.Sprintf("Risc %d", i), Severity: "High", Probability: "Low", Evidence: ev(i)})
		r.Decisions = append(r.Decisions, minutes.Decision{Key: fmt.Sprintf("d%d", i), Statement: fmt.Sprintf("Decizie %d", i), Status: "Active", Evidence: ev(i)})
		r.Changes = append(r.Changes, minutes.Change{Key: fmt.Sprintf("c%d", i), Title: fmt.Sprintf("Modificare %d", i), ChangeType: "Large", Status: "New", Evidence: ev(i)})
		r.Interdependencies = append(r.Interdependencies, minutes.Interdependency{Key: fmt.Sprintf("i%d", i), Dependency: fmt.Sprintf("Dependență %d", i),
			Status: "Open", ActionKeys: []string{"a2", "a5", "missing"}, RiskKeys: []string{"r3"}, Evidence: ev(i)})
	}
	return m, r
}

func render(t *testing.T, m Meeting, r minutes.Records) *excelize.File {
	t.Helper()
	b, err := Render(m, r)
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("DEO_KEEP_XLSX"); dir != "" { // manual S4 check in Excel/LibreOffice
		_ = os.WriteFile(filepath.Join(dir, t.Name()+".xlsx"), b, 0o600)
	}
	f, err := excelize.OpenReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func get(t *testing.T, f *excelize.File, sheet, cell string) string {
	t.Helper()
	v, err := f.GetCellValue(sheet, cell)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func tableRange(t *testing.T, f *excelize.File, sheet string) string {
	t.Helper()
	ts, err := f.GetTables(sheet)
	if err != nil || len(ts) != 1 {
		t.Fatalf("%s tables: %v %v", sheet, ts, err)
	}
	return ts[0].Range
}

func TestRenderGrowsTablesAndFillsValues(t *testing.T) {
	m, r := bigRecords()
	f := render(t, m, r)

	if got := f.GetSheetList(); strings.Join(got, "|") != "Meetings|Action Item Register|Interdependencies|Risk Register|Decision Log|Change Log" {
		t.Fatalf("sheets changed: %v", got)
	}
	wantRanges := map[string]string{
		SheetMeetings:  "D8:E58",
		SheetActions:   "A5:I65",
		SheetInterdeps: "B5:H20",
		SheetRisks:     "B6:K21",
		SheetDecisions: "B5:I20",
		SheetChanges:   "B5:O20",
	}
	for s, want := range wantRanges {
		if got := tableRange(t, f, s); got != want {
			t.Errorf("%s range = %s, want %s", s, got, want)
		}
	}

	checks := []struct{ sheet, cell, want string }{
		{SheetMeetings, "B1", "Proiect Ștefan/Test"},
		{SheetMeetings, "P6", "7"},
		{SheetMeetings, "C10", "Maria Ionescu"},
		{SheetMeetings, "B9", "Meeting date"}, // label beside the table untouched
		{SheetMeetings, "B12", "Distributed to:"},
		{SheetMeetings, "D9", "Participant 1"},
		{SheetMeetings, "D58", "Participant 50"},
		{SheetMeetings, "E58", "Call"},
		{SheetActions, "A6", "AI-1"},
		{SheetActions, "A65", "AI-60"},
		{SheetActions, "B65", "Acțiune 60"},
		{SheetActions, "F6", ""}, // Team left empty (Q14)
		{SheetActions, "I6", "[00:12:34] Ion Popescu: „citat 1 cu diacritice ăâîșț”"},
		{SheetInterdeps, "B20", "ID-15"},
		{SheetInterdeps, "F6", "AI-2, AI-5"},
		{SheetInterdeps, "G6", "R-3"},
		{SheetRisks, "B21", "R-15"},
		{SheetRisks, "E7", "High"},
		{SheetDecisions, "B20", "D-15"},
		{SheetDecisions, "H6", "Active"},
		{SheetChanges, "B20", "CH-15"},
		{SheetChanges, "H6", "Large"},
	}
	for _, c := range checks {
		if got := get(t, f, c.sheet, c.cell); got != c.want {
			t.Errorf("%s!%s = %q, want %q", c.sheet, c.cell, got, c.want)
		}
	}

	if cp, err := f.GetCalcProps(); err != nil || cp.FullCalcOnLoad == nil || !*cp.FullCalcOnLoad {
		t.Errorf("fullCalcOnLoad not set: %+v %v", cp, err)
	}

	// Formulas survive.
	if fx, _ := f.GetCellFormula(SheetMeetings, "B6"); !strings.Contains(fx, "P6") {
		t.Errorf("Meetings!B6 formula lost: %q", fx)
	}
	if fx, _ := f.GetCellFormula(SheetActions, "A2"); fx != "Meetings!B1" {
		t.Errorf("Action!A2 formula = %q", fx)
	}

	// Dates are real dates formatted dd.mm.yyyy.
	if got := get(t, f, SheetActions, "D6"); got != "27.10.2026" {
		t.Errorf("Date issued = %q", got)
	}
	if got := get(t, f, SheetActions, "E65"); got != "06.11.2026" {
		t.Errorf("Deadline = %q", got)
	}
	if got := get(t, f, SheetMeetings, "C9"); got != "27.10.2026" {
		t.Errorf("Meeting date = %q", got)
	}

	// Template header comments on Change Log survive; evidence comments added.
	comments, err := f.GetComments(SheetChanges)
	if err != nil {
		t.Fatal(err)
	}
	cells := map[string]bool{}
	for _, c := range comments {
		cells[c.Cell] = true
	}
	for _, c := range []string{"G5", "H5", "M5", "N5"} {
		if !cells[c] {
			t.Errorf("Change Log header comment %s lost", c)
		}
	}
	rc, _ := f.GetComments(SheetRisks)
	if len(rc) != 15 {
		t.Errorf("risk evidence comments = %d, want 15", len(rc))
	}

	// Grown rows copy the style of the template's last data row (now row 65).
	s1, _ := f.GetCellStyle(SheetActions, "C65")
	s2, _ := f.GetCellStyle(SheetActions, "C45")
	if s1 != s2 {
		t.Errorf("grown row style %d != %d", s2, s1)
	}
}

func TestRenderSmallClearsPlaceholders(t *testing.T) {
	m := Meeting{Project: "P", No: 1, Date: meetingDate, Participants: []Participant{{Name: "Ana", Attendance: "Call"}}}
	r := minutes.Records{Actions: []minutes.ActionItem{{Key: "a1", Title: "T", Status: "Open", Deadline: "vineri"}}}
	f := render(t, m, r)
	if got := tableRange(t, f, SheetActions); got != "A5:I44" {
		t.Errorf("table must not shrink: %s", got)
	}
	for _, c := range []struct{ sheet, cell string }{
		{SheetActions, "A7"},   // template prefilled AI-2
		{SheetMeetings, "E10"}, // template sample "Call"
		{SheetDecisions, "H6"}, // template sample "Active"
		{SheetDecisions, "B6"}, // prefilled D-1
		{SheetActions, "E6"},   // unparseable deadline stays empty
	} {
		if got := get(t, f, c.sheet, c.cell); got != "" {
			t.Errorf("%s!%s = %q, want empty", c.sheet, c.cell, got)
		}
	}
}

func TestBaseNameAndUnique(t *testing.T) {
	got := BaseName(meetingDate, ` Proiect: A/B? `, 3)
	if got != "261027_Proiect- A-B-_Minuta_3" {
		t.Fatalf("got %q", got)
	}
	dir := t.TempDir()
	b1, _ := UniqueBase(dir, "x")
	if err := WriteFileAtomic(filepath.Join(dir, b1+".xlsx"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	if b2, _ := UniqueBase(dir, "x"); b2 != "x_v2" {
		t.Fatalf("got %q", b2)
	}
}

func TestClean(t *testing.T) {
	if got := clean(" a\x00b\nc\x07 "); got != "ab\nc" {
		t.Fatalf("got %q", got)
	}
}
