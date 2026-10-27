// Package export fills the company project-workbook template with one
// meeting's records. It only writes values into existing table rows (growing
// the tables when needed) and a few header cells; the template's layout,
// styles, comments and formulas stay untouched.
package export

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"deo-minutes/internal/minutes"
	"deo-minutes/templates"
)

// Sheet names in the template.
const (
	SheetMeetings  = "Meetings"
	SheetActions   = "Action Item Register"
	SheetInterdeps = "Interdependencies"
	SheetRisks     = "Risk Register"
	SheetDecisions = "Decision Log"
	SheetChanges   = "Change Log"
)

// Meeting is the header information for the Meetings sheet.
type Meeting struct {
	Project      string
	No           int
	Date         time.Time
	PreparedBy   string
	Participants []Participant
}

type Participant struct {
	Name       string
	Attendance string // "Call" for everyone seen in Webex
}

const commentAuthor = "DEO Minute"

// Render fills the embedded template and returns the .xlsx bytes.
func Render(m Meeting, r minutes.Records) ([]byte, error) {
	return RenderTemplate(templates.ProjectWorkbook, m, r)
}

// RenderTemplate fills the given template bytes.
func RenderTemplate(tpl []byte, m Meeting, r minutes.Records) ([]byte, error) {
	f, err := excelize.OpenReader(bytes.NewReader(tpl))
	if err != nil {
		return nil, fmt.Errorf("deschidere șablon: %w", err)
	}
	defer func() { _ = f.Close() }()
	w := &writer{f: f, date: m.Date, dateStyles: map[int]int{}}

	steps := []func() error{
		func() error { return w.meetings(m) },
		func() error { return w.actions(r.Actions) },
		func() error { return w.interdeps(r.Interdependencies, r.Actions, r.Risks) },
		func() error { return w.risks(r.Risks) },
		func() error { return w.decisions(r.Decisions) },
		func() error { return w.changes(r.Changes) },
	}
	for _, s := range steps {
		if err := s(); err != nil {
			return nil, err
		}
	}
	// Formulas (e.g. "Coordination Meeting no. …" and the =Meetings!B1 titles)
	// carry stale cached results from the template; make Excel recompute them.
	fullCalc := true
	if err := f.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &fullCalc}); err != nil {
		return nil, err
	}
	f.SetActiveSheet(0)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("scriere Excel: %w", err)
	}
	return buf.Bytes(), nil
}

type writer struct {
	f          *excelize.File
	date       time.Time
	dateStyles map[int]int // base style id → same style with a date format
}

func (w *writer) meetings(m Meeting) error {
	f := w.f
	if err := f.SetCellStr(SheetMeetings, "B1", clean(m.Project)); err != nil {
		return err
	}
	if err := f.SetCellInt(SheetMeetings, "P6", int64(m.No)); err != nil {
		return err
	}
	if err := w.setDate(SheetMeetings, "C9", m.Date); err != nil {
		return err
	}
	if err := f.SetCellStr(SheetMeetings, "C10", clean(m.PreparedBy)); err != nil {
		return err
	}
	t, err := openTable(f, SheetMeetings)
	if err != nil {
		return err
	}
	if err := t.ensureRows(len(m.Participants)); err != nil {
		return err
	}
	if err := t.clear(); err != nil {
		return err
	}
	for i, p := range m.Participants {
		row := t.dataRow(i)
		if err := t.set(row, "Participants", clean(p.Name)); err != nil {
			return err
		}
		if err := t.set(row, "Local/Call", p.Attendance); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) actions(items []minutes.ActionItem) error {
	t, err := w.prepare(SheetActions, len(items))
	if err != nil {
		return err
	}
	for i, a := range items {
		row := t.dataRow(i)
		vals := map[string]string{
			"#":           fmt.Sprintf("AI-%d", i+1),
			"Title":       a.Title,
			"Description": a.Description,
			"Responsible": a.Responsible,
			"Status":      a.Status,
			"Notes":       joinNonEmpty(a.Notes, formatEvidence(a.Evidence)),
		}
		if err := t.setAll(row, vals); err != nil {
			return err
		}
		if err := w.setDateCol(t, row, "Date issued", w.date); err != nil {
			return err
		}
		if d, ok := parseDate(a.Deadline); ok {
			if err := w.setDateCol(t, row, "Deadline", d); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *writer) interdeps(items []minutes.Interdependency, acts []minutes.ActionItem, risks []minutes.Risk) error {
	actID := map[string]string{}
	for i, a := range acts {
		actID[a.Key] = fmt.Sprintf("AI-%d", i+1)
	}
	riskID := map[string]string{}
	for i, r := range risks {
		riskID[r.Key] = fmt.Sprintf("R-%d", i+1)
	}
	t, err := w.prepare(SheetInterdeps, len(items))
	if err != nil {
		return err
	}
	for i, d := range items {
		vals := map[string]string{
			"#":                           fmt.Sprintf("ID-%d", i+1),
			"Dependent on (Project name)": d.DependentOnProject,
			"Dependency":                  d.Dependency,
			"Status":                      d.Status,
			"Action Item(s)":              resolveKeys(d.ActionKeys, actID),
			"Risk(s)":                     resolveKeys(d.RiskKeys, riskID),
			"Notes":                       joinNonEmpty(d.Notes, formatEvidence(d.Evidence)),
		}
		if err := t.setAll(t.dataRow(i), vals); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) risks(items []minutes.Risk) error {
	t, err := w.prepare(SheetRisks, len(items))
	if err != nil {
		return err
	}
	for i, r := range items {
		row := t.dataRow(i)
		vals := map[string]string{
			"#":                                fmt.Sprintf("R-%d", i+1),
			"Risk title and brief description": r.TitleDescription,
			"Cause":                            r.Cause,
			"Severity":                         r.Severity,
			"Probability":                      r.Probability,
			"Decision":                         r.Decision,
			"Risk response actions":            r.ResponseActions,
			"Owner for implementation":         r.Owner,
			"Status update":                    r.StatusUpdate,
		}
		if err := t.setAll(row, vals); err != nil {
			return err
		}
		if err := w.setDateCol(t, row, "Date of update", w.date); err != nil {
			return err
		}
		if err := t.comment(row, "#", formatEvidence(r.Evidence)); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) decisions(items []minutes.Decision) error {
	t, err := w.prepare(SheetDecisions, len(items))
	if err != nil {
		return err
	}
	for i, d := range items {
		row := t.dataRow(i)
		vals := map[string]string{
			"#":                                 fmt.Sprintf("D-%d", i+1),
			"Statement":                         d.Statement,
			"Rationale":                         d.Rationale,
			"Made by":                           d.MadeBy,
			"Agreed with (with the consent of)": d.AgreedWith,
			"Status":                            d.Status,
			"Outcomes":                          d.Outcomes,
		}
		if err := t.setAll(row, vals); err != nil {
			return err
		}
		if err := w.setDateCol(t, row, "Date", w.date); err != nil {
			return err
		}
		if err := t.comment(row, "#", formatEvidence(d.Evidence)); err != nil {
			return err
		}
	}
	return nil
}

func (w *writer) changes(items []minutes.Change) error {
	t, err := w.prepare(SheetChanges, len(items))
	if err != nil {
		return err
	}
	for i, c := range items {
		row := t.dataRow(i)
		vals := map[string]string{
			"#":                    fmt.Sprintf("CH-%d", i+1),
			"Title":                c.Title,
			"Summary description":  c.Summary,
			"Requested by":         c.RequestedBy,
			"Requestor's priority": c.RequestorPriority,
			"Change type":          c.ChangeType,
			"Cost impact":          c.CostImpact,
			"Schedule impact":      c.ScheduleImpact,
			"Quality impact":       c.QualityImpact,
			"Overall impact":       c.OverallImpact,
			"Priority":             c.Priority,
			"Status":               c.Status,
			"Notes":                joinNonEmpty(c.Notes, formatEvidence(c.Evidence)),
		}
		if err := t.setAll(row, vals); err != nil {
			return err
		}
		if err := w.setDateCol(t, row, "Date submitted", w.date); err != nil {
			return err
		}
	}
	return nil
}

// prepare opens the sheet's table, grows it to n rows and clears the
// template's placeholder values (prefilled IDs, sample statuses).
func (w *writer) prepare(sheet string, n int) (*table, error) {
	t, err := openTable(w.f, sheet)
	if err != nil {
		return nil, err
	}
	if err := t.ensureRows(n); err != nil {
		return nil, err
	}
	return t, t.clear()
}

func (w *writer) setDateCol(t *table, row int, header string, d time.Time) error {
	cell, err := t.cell(row, header)
	if err != nil {
		return err
	}
	return w.setDate(t.sheet, cell, d)
}

// setDate writes a real Excel date and gives the cell a dd.mm.yyyy format
// while keeping the rest of its existing style (borders, font, fill).
func (w *writer) setDate(sheet, cell string, d time.Time) error {
	if d.IsZero() {
		return w.f.SetCellStr(sheet, cell, "")
	}
	base, err := w.f.GetCellStyle(sheet, cell)
	if err != nil {
		return err
	}
	id, ok := w.dateStyles[base]
	if !ok {
		st, err := w.f.GetStyle(base)
		if err != nil {
			return err
		}
		format := "dd.mm.yyyy"
		st.NumFmt = 0
		st.CustomNumFmt = &format
		if id, err = w.f.NewStyle(st); err != nil {
			return err
		}
		w.dateStyles[base] = id
	}
	if err := w.f.SetCellValue(sheet, cell, time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)); err != nil {
		return err
	}
	return w.f.SetCellStyle(sheet, cell, cell, id)
}

func formatEvidence(ev []minutes.Evidence) string {
	lines := make([]string, 0, len(ev))
	for _, e := range ev {
		if e.Quote == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: „%s”", e.At, e.Speaker, e.Quote))
	}
	return strings.Join(lines, "\n")
}

func joinNonEmpty(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n")
}

func resolveKeys(keys []string, ids map[string]string) string {
	var out []string
	for _, k := range keys {
		if id, ok := ids[k]; ok {
			out = append(out, id)
		}
	}
	return strings.Join(out, ", ")
}

func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	return t, err == nil
}
