// Package templates embeds the company workbook template so the exe is self-contained.
package templates

import _ "embed"

// ProjectWorkbook is a copy of the user's "Project workbook_MASTER.xlsx".
//
//go:embed project_workbook_template.xlsx
var ProjectWorkbook []byte
