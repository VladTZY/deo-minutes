package minutes

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
)

//go:embed prompts/extract.md
var extractPrompt string

//go:embed prompts/merge.md
var mergePrompt string

// Meeting facts the model needs besides the transcript.
type MeetingInfo struct {
	Project      string
	Date         time.Time
	Participants []string
}

// Extractor turns a transcript into validated records.
type Extractor struct {
	LLM llm.Client
	// SinglePassTokens: transcripts up to this size go in one request (Claude
	// has a 1M-token context; a 4 h meeting is roughly 60-80k tokens). Larger
	// ones, or a gateway with a lower limit, use windows + merge.
	SinglePassTokens int
	Window, Overlap  time.Duration
}

// Extract runs the model and validates the result against the transcript.
func (x *Extractor) Extract(ctx context.Context, m MeetingInfo, t Transcript) (Records, Report, error) {
	var rep Report
	if len(t) == 0 {
		return Records{}, rep, i18n.Errorf(nil, "warn.transcriptEmpty")
	}
	if x.SinglePassTokens == 0 {
		x.SinglePassTokens = 300_000
	}
	if x.Window == 0 {
		x.Window, x.Overlap = 45*time.Minute, 3*time.Minute
	}
	var recs Records
	var err error
	if estimateTokens(t.Render()) <= x.SinglePassTokens {
		recs, err = x.call(ctx, extractPrompt, userMessage(m, t.Render()))
	} else {
		recs, err = x.mapReduce(ctx, m, t)
	}
	if err != nil {
		return Records{}, rep, err
	}
	recs = Validate(recs, t, &rep)
	slog.Info("minutes extracted", "actions", len(recs.Actions), "decisions", len(recs.Decisions),
		"risks", len(recs.Risks), "interdeps", len(recs.Interdependencies), "changes", len(recs.Changes),
		"dropped", rep.Dropped, "evidence_fixed", rep.EvidenceFixed)
	return recs, rep, nil
}

func (x *Extractor) mapReduce(ctx context.Context, m MeetingInfo, t Transcript) (Records, error) {
	var parts []Records
	for i, w := range t.Windows(x.Window, x.Overlap) {
		r, err := x.call(ctx, extractPrompt, userMessage(m, w.Render()))
		if err != nil {
			return Records{}, fmt.Errorf("fereastra %d: %w", i+1, err)
		}
		// Validate each window before merging so the merge only sees real quotes.
		var rep Report
		parts = append(parts, Validate(r, w, &rep))
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	in, _ := json.Marshal(parts)
	return x.call(ctx, mergePrompt, "Records from each window, in meeting order:\n"+string(in))
}

func userMessage(m MeetingInfo, transcript string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Project: %s\nMeeting date: %s (%s)\n", m.Project, m.Date.Format("2006-01-02"), m.Date.Weekday())
	if len(m.Participants) > 0 {
		fmt.Fprintf(&sb, "Participants: %s\n", strings.Join(m.Participants, ", "))
	}
	sb.WriteString("\n<transcript>\n")
	sb.WriteString(transcript)
	sb.WriteString("</transcript>\n")
	return sb.String()
}

func (x *Extractor) call(ctx context.Context, system, user string) (Records, error) {
	b, err := x.LLM.JSON(ctx, llm.Request{System: system, User: user, Schema: Schema()})
	if err != nil {
		return Records{}, err
	}
	var r Records
	if err := json.Unmarshal(b, &r); err != nil {
		return Records{}, i18n.Errorf(err, "err.gateway.failed", "JSON invalid")
	}
	return r, nil
}
