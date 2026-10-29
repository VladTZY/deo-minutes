package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"deo-minutes/internal/minutes"
)

// RenderSummary writes the meeting summary as a Word document (.docx). The
// file is built by hand from the minimal WordprocessingML parts, so it needs
// no extra dependency and opens in Word, LibreOffice and the Windows preview.
func RenderSummary(m Meeting, s minutes.Summary) ([]byte, error) {
	var d docBody
	title := fmt.Sprintf("Rezumatul ședinței – %s", m.Project)
	if m.No > 0 {
		title += fmt.Sprintf(", nr. %d", m.No)
	}
	d.para("Title", title)
	meta := []string{"Data: " + m.Date.Format("02.01.2006")}
	if m.PreparedBy != "" {
		meta = append(meta, "Întocmit de: "+m.PreparedBy)
	}
	d.para("Subtitle", strings.Join(meta, "   ·   "))
	if len(m.Participants) > 0 {
		names := make([]string, len(m.Participants))
		for i, p := range m.Participants {
			names[i] = p.Name
		}
		d.para("", "Participanți: "+strings.Join(names, ", "))
	}

	d.para("Heading1", "Pe scurt")
	d.para("", orDash(s.Overview))

	if len(s.Topics) > 0 {
		d.para("Heading1", "Subiecte discutate")
		for _, t := range s.Topics {
			d.para("Heading2", t.Title)
			d.para("", t.Summary)
		}
	}
	d.list("Decizii", s.Decisions)
	if len(s.Actions) > 0 {
		items := make([]string, 0, len(s.Actions))
		for _, a := range s.Actions {
			item := a.Task
			var who []string
			if a.Owner != "" {
				who = append(who, "responsabil: "+a.Owner)
			}
			if a.Deadline != "" {
				who = append(who, "termen: "+a.Deadline)
			}
			if len(who) > 0 {
				item += " (" + strings.Join(who, ", ") + ")"
			}
			items = append(items, item)
		}
		d.list("Acțiuni", items)
	}
	d.list("Puncte deschise", s.OpenPoints)
	d.list("Pași următori", s.NextSteps)
	d.para("Note", "Rezumat generat automat din transcrierea ședinței. Verifică-l înainte de a-l trimite mai departe.")

	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", rootRelsXML},
		{"word/_rels/document.xml.rels", docRelsXML},
		{"word/styles.xml", stylesXML},
		{"word/numbering.xml", numberingXML},
		{"word/document.xml", documentHead + d.String() + documentTail},
	}
	for _, p := range parts {
		w, err := z.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "–"
	}
	return s
}

type docBody struct{ strings.Builder }

// para adds one paragraph in style ("" = Normal).
func (d *docBody) para(style, text string) {
	d.WriteString("<w:p>")
	if style != "" {
		d.WriteString(`<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`)
	}
	d.run(text)
	d.WriteString("</w:p>")
}

// list adds a heading and a bulleted list; nothing when items is empty.
func (d *docBody) list(heading string, items []string) {
	if len(items) == 0 {
		return
	}
	d.para("Heading1", heading)
	for _, it := range items {
		d.WriteString(`<w:p><w:pPr><w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr>`)
		d.run(it)
		d.WriteString("</w:p>")
	}
}

// run writes text, keeping line breaks.
func (d *docBody) run(text string) {
	d.WriteString("<w:r>")
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if i > 0 {
			d.WriteString("<w:br/>")
		}
		d.WriteString(`<w:t xml:space="preserve">`)
		_ = xml.EscapeText(d, []byte(cleanXMLText(line)))
		d.WriteString("</w:t>")
	}
	d.WriteString("</w:r>")
}

// cleanXMLText drops characters XML 1.0 cannot carry (model output is
// untrusted text).
func cleanXMLText(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t', r >= 0x20 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD, r >= 0x10000 && r <= 0x10FFFF:
			return r
		}
		return -1
	}, s)
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
</Relationships>`

const documentHead = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`

// A4 portrait, 2.5 cm margins.
const documentTail = `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1418" w:right="1418" w:bottom="1418" w:left="1418" w:header="709" w:footer="709" w:gutter="0"/></w:sectPr></w:body></w:document>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults>
<w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="ro-RO"/></w:rPr></w:rPrDefault>
<w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault>
</w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="60"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/><w:szCs w:val="36"/><w:color w:val="1F3864"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:rPr><w:color w:val="595959"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="320" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/><w:color w:val="1F3864"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="60"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/><w:szCs w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="60"/><w:ind w:left="720"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Note"><w:name w:val="Note"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="360"/></w:pPr><w:rPr><w:i/><w:sz w:val="18"/><w:szCs w:val="18"/><w:color w:val="7F7F7F"/></w:rPr></w:style>
</w:styles>`

const numberingXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="singleLevel"/>
<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>
</w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`
