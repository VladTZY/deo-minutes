package minutes

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
)

//go:embed prompts/summary.md
var summaryPrompt string

// Summary is the narrative meeting summary, in Romanian.
type Summary struct {
	Overview   string          `json:"overview"`
	Topics     []SummaryTopic  `json:"topics"`
	Decisions  []string        `json:"decisions"`
	Actions    []SummaryAction `json:"actions"`
	OpenPoints []string        `json:"open_points"`
	NextSteps  []string        `json:"next_steps"`
}

type SummaryTopic struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

type SummaryAction struct {
	Task     string `json:"task"`
	Owner    string `json:"owner"`
	Deadline string `json:"deadline"`
}

// SummarySchema is the strict structured-output schema of Summary.
func SummarySchema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	strs := func(desc string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
	}
	list := func(item map[string]any, desc string) map[string]any {
		return map[string]any{"type": "array", "items": item, "description": desc}
	}
	return closed(map[string]any{
		"overview": str("3-6 sentences: purpose, main outcomes, anything urgent; Romanian"),
		"topics": list(closed(map[string]any{
			"title":   str("short topic title, Romanian"),
			"summary": str("2-5 sentences on what was said and where it ended, Romanian"),
		}), "subjects discussed, in meeting order"),
		"decisions": strs("one sentence per decision actually taken, Romanian"),
		"actions": list(closed(map[string]any{
			"task":     str("what must be done, Romanian"),
			"owner":    str("person responsible as named in the meeting, else empty"),
			"deadline": str("deadline as stated or YYYY-MM-DD, else empty"),
		}), "agreed tasks"),
		"open_points": strs("questions or issues left open, Romanian"),
		"next_steps":  strs("what happens next, only if stated, Romanian"),
	})
}

// Summarize writes the meeting summary. Transcripts longer than
// SinglePassTokens are summarised per window and the parts merged.
func (x *Extractor) Summarize(ctx context.Context, m MeetingInfo, t Transcript) (Summary, error) {
	if len(t) == 0 {
		return Summary{}, i18n.Errorf(nil, "warn.transcriptEmpty")
	}
	if x.SinglePassTokens == 0 {
		x.SinglePassTokens = 300_000
	}
	if x.Window == 0 {
		x.Window, x.Overlap = 60*time.Minute, 3*time.Minute
	}
	if estimateTokens(t.Render()) <= x.SinglePassTokens {
		return x.summarizeOnce(ctx, meetingContext(m, t.Render()), "Write the meeting summary.")
	}
	var parts []Summary
	for i, w := range t.Windows(x.Window, x.Overlap) {
		task := fmt.Sprintf("This is part %d of a long meeting, in order. Write the summary of this part only.", i+1)
		s, err := x.summarizeOnce(ctx, meetingContext(m, w.Render()), task)
		if err != nil {
			return Summary{}, fmt.Errorf("fereastra %d: %w", i+1, err)
		}
		parts = append(parts, s)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	in, _ := json.Marshal(parts)
	return x.summarizeOnce(ctx, "", "These are the summaries of consecutive parts of one meeting, in order. "+
		"Merge them into one summary of the whole meeting: one overview, topics in meeting order without repeats "+
		"(the parts overlap by a few minutes), and every decision, action, open point and next step once.\n"+string(in))
}

func (x *Extractor) summarizeOnce(ctx context.Context, shared, task string) (Summary, error) {
	b, err := x.LLM.JSON(ctx, llm.Request{System: summaryPrompt, Context: shared, User: task, Schema: SummarySchema()})
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	if err := json.Unmarshal(b, &s); err != nil {
		return Summary{}, i18n.Errorf(err, "err.gateway.failed", "JSON invalid")
	}
	return s, nil
}
