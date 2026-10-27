package export

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// table is the single Excel table on a template sheet, addressed by header text.
type table struct {
	f              *excelize.File
	sheet          string
	x1, y1, x2, y2 int            // y1 = header row
	cols           map[string]int // normalized header → column number
}

func openTable(f *excelize.File, sheet string) (*table, error) {
	tables, err := f.GetTables(sheet)
	if err != nil {
		return nil, err
	}
	if len(tables) != 1 {
		return nil, fmt.Errorf("foaia %q: aștept un singur tabel, am găsit %d", sheet, len(tables))
	}
	t := &table{f: f, sheet: sheet}
	if err := t.load(tables[0].Range); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *table) load(ref string) error {
	parts := strings.Split(ref, ":")
	if len(parts) != 2 {
		return fmt.Errorf("foaia %q: interval tabel invalid %q", t.sheet, ref)
	}
	var err error
	if t.x1, t.y1, err = excelize.CellNameToCoordinates(parts[0]); err != nil {
		return err
	}
	if t.x2, t.y2, err = excelize.CellNameToCoordinates(parts[1]); err != nil {
		return err
	}
	t.cols = map[string]int{}
	for c := t.x1; c <= t.x2; c++ {
		name, _ := excelize.CoordinatesToCellName(c, t.y1)
		v, err := t.f.GetCellValue(t.sheet, name)
		if err != nil {
			return err
		}
		t.cols[norm(v)] = c
	}
	return nil
}

func norm(h string) string { return strings.ToLower(strings.TrimSpace(h)) }

func (t *table) capacity() int     { return t.y2 - t.y1 }
func (t *table) dataRow(i int) int { return t.y1 + 1 + i }

// ensureRows grows the table so it has at least n data rows. Blank rows are
// inserted inside the table range (so the table ref grows with them) and get
// the last data row's cell styles, for the table's columns only: cells beside
// the table, like the Meetings labels, are left alone.
func (t *table) ensureRows(n int) error {
	k := n - t.capacity()
	if k <= 0 {
		return nil
	}
	at := t.y2
	if err := t.f.InsertRows(t.sheet, at, k); err != nil {
		return fmt.Errorf("foaia %q: extindere tabel: %w", t.sheet, err)
	}
	src := at + k // the old last data row, shifted down
	height, err := t.f.GetRowHeight(t.sheet, src)
	if err != nil {
		return err
	}
	for c := t.x1; c <= t.x2; c++ {
		from, _ := excelize.CoordinatesToCellName(c, src)
		style, err := t.f.GetCellStyle(t.sheet, from)
		if err != nil {
			return err
		}
		top, _ := excelize.CoordinatesToCellName(c, at)
		bottom, _ := excelize.CoordinatesToCellName(c, src-1)
		if err := t.f.SetCellStyle(t.sheet, top, bottom, style); err != nil {
			return err
		}
	}
	for r := at; r < src; r++ {
		if err := t.f.SetRowHeight(t.sheet, r, height); err != nil {
			return err
		}
	}
	tables, err := t.f.GetTables(t.sheet)
	if err != nil {
		return err
	}
	return t.load(tables[0].Range)
}

// clear blanks every data cell, keeping styles.
func (t *table) clear() error {
	for r := t.y1 + 1; r <= t.y2; r++ {
		for c := t.x1; c <= t.x2; c++ {
			name, _ := excelize.CoordinatesToCellName(c, r)
			if err := t.f.SetCellStr(t.sheet, name, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *table) cell(row int, header string) (string, error) {
	c, ok := t.cols[norm(header)]
	if !ok {
		return "", fmt.Errorf("foaia %q: coloana %q lipsește din șablon", t.sheet, header)
	}
	return excelize.CoordinatesToCellName(c, row)
}

func (t *table) set(row int, header, value string) error {
	cell, err := t.cell(row, header)
	if err != nil {
		return err
	}
	return t.f.SetCellStr(t.sheet, cell, clean(value))
}

func (t *table) setAll(row int, vals map[string]string) error {
	for h, v := range vals {
		if err := t.set(row, h, v); err != nil {
			return err
		}
	}
	return nil
}

func (t *table) comment(row int, header, text string) error {
	if text == "" {
		return nil
	}
	cell, err := t.cell(row, header)
	if err != nil {
		return err
	}
	return t.f.AddComment(t.sheet, excelize.Comment{
		Cell:      cell,
		Author:    commentAuthor,
		Paragraph: []excelize.RichTextRun{{Text: clean(text)}},
	})
}
