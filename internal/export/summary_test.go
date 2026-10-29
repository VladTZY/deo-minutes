package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"deo-minutes/internal/minutes"
)

func TestRenderSummaryDocx(t *testing.T) {
	m := Meeting{Project: "Alfa & Beta", No: 3, Date: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC), PreparedBy: "Ana",
		Participants: []Participant{{Name: "Ion Popescu"}, {Name: "Maria Ionescu"}}}
	s := minutes.Summary{
		Overview:   "Bugetul pe T4 a fost aprobat; livrarea se mută cu o săptămână.\nUrgent: contractul <nou>.",
		Topics:     []minutes.SummaryTopic{{Title: "Buget", Summary: "S-a discutat bugetul."}},
		Decisions:  []string{"Se aprobă bugetul."},
		Actions:    []minutes.SummaryAction{{Task: "Trimite raportul", Owner: "Ion", Deadline: "2026-10-31"}, {Task: "Verifică oferta"}},
		OpenPoints: []string{"Cine plătește licența?\x01"},
	}
	b, err := RenderSummary(m, s)
	if err != nil {
		t.Fatal(err)
	}
	if p := os.Getenv("DEO_DOCX_OUT"); p != "" {
		_ = os.WriteFile(p, b, 0o600)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(data)
		// every part must be well-formed XML
		dec := xml.NewDecoder(bytes.NewReader(data))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Fatalf("%s: %v", f.Name, err)
			}
		}
	}
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		if _, ok := files[want]; !ok {
			t.Errorf("missing part %s", want)
		}
	}
	doc := files["word/document.xml"]
	for _, want := range []string{
		"Rezumatul ședinței – Alfa &amp; Beta, nr. 3", "Data: 29.10.2026", "Întocmit de: Ana", "Ion Popescu, Maria Ionescu",
		"contractul &lt;nou&gt;", "<w:br/>", "Trimite raportul (responsabil: Ion, termen: 2026-10-31)", "Verifică oferta</w:t>",
		"Puncte deschise", "Cine plătește licența?</w:t>",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document.xml lacks %q", want)
		}
	}
	if strings.Contains(doc, "Pași următori") {
		t.Error("empty sections must be left out")
	}
}
