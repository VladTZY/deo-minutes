package export

import (
	"strings"
	"unicode"
)

// maxCellLen is Excel's per-cell character limit.
const maxCellLen = 32767

// clean strips control characters (except newline and tab) and caps length.
// Values are written as string cells, so Excel never evaluates them as formulas.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxCellLen {
		s = string(r[:maxCellLen-1]) + "…"
	}
	return strings.TrimSpace(s)
}
