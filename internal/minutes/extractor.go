package minutes

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"deo-minutes/internal/i18n"
	"deo-minutes/internal/llm"
)

//go:embed prompts/common.md
var commonPrompt string

//go:embed prompts/merge.md
var mergePrompt string

// Per-register task for the specialist passes.
var registerTask = map[string]string{
	"actions":   "Fill only the actions register: every task someone agreed, was assigned or committed to, including small follow-ups (send, check, prepare, call).",
	"decisions": "Fill only the decisions register: every point the meeting decided, approved or rejected. Skip proposals that were only discussed.",
	"risks":     "Fill only the risks register: every risk, threat, blocker or concern about the project that was raised, with cause, severity, probability and response when they were discussed.",
	"changes":   "Fill only the changes register: every requested change to scope, schedule, budget or deliverables, with who asked for it and the impact discussed.",
}

const interdepTask = "Fill only the interdependencies register: every place where this project depends on another project or team, or another depends on it. " +
	"These actions and risks were already extracted from the same meeting; link to them through action_keys and risk_keys when they are related, using exactly these keys:\n"

// MeetingInfo holds the facts the model needs besides the transcript.
type MeetingInfo struct {
	Project      string
	Date         time.Time
	Participants []string
}

// Extractor turns a transcript into validated records.
type Extractor struct {
	LLM llm.Client
	// SinglePassTokens: transcripts up to this size are read whole by every
	// specialist pass (Claude has a 1M-token context; a 4 h meeting is about
	// 60-80k tokens). Larger ones are cut into windows that are merged after.
	SinglePassTokens int
	Window, Overlap  time.Duration
	// Parallel caps concurrent passes (gateway rate limits).
	Parallel int
}

// Extract runs one focused pass per register and validates the result
// against the transcript.
func (x *Extractor) Extract(ctx context.Context, m MeetingInfo, t Transcript) (Records, Report, error) {
	var rep Report
	if len(t) == 0 {
		return Records{}, rep, i18n.Errorf(nil, "warn.transcriptEmpty")
	}
	if x.SinglePassTokens == 0 {
		x.SinglePassTokens = 300_000
	}
	if x.Window == 0 {
		x.Window, x.Overlap = 60*time.Minute, 3*time.Minute
	}
	if x.Parallel == 0 {
		x.Parallel = 3
	}
	var recs Records
	var err error
	if estimateTokens(t.Render()) <= x.SinglePassTokens {
		recs, err = x.specialists(ctx, m, t, &rep)
	} else {
		recs, err = x.windowed(ctx, m, t, &rep)
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

// specialists runs the register passes over one transcript. The transcript
// is a cached prefix shared by all passes: the actions pass runs first and
// writes the cache, the next three run in parallel and read it, and the
// interdependencies pass runs last with the actions and risks to link to.
func (x *Extractor) specialists(ctx context.Context, m MeetingInfo, t Transcript, rep *Report) (Records, error) {
	shared := meetingContext(m, t.Render())
	var out Records
	if err := x.pass(ctx, shared, "actions", registerTask["actions"], &out); err != nil {
		return Records{}, err
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(x.Parallel)
	var dec, risk, chg Records
	for reg, dst := range map[string]*Records{"decisions": &dec, "risks": &risk, "changes": &chg} {
		g.Go(func() error { return x.pass(gctx, shared, reg, registerTask[reg], dst) })
	}
	if err := g.Wait(); err != nil {
		return Records{}, err
	}
	out.Decisions, out.Risks, out.Changes = dec.Decisions, risk.Risks, chg.Changes

	// Validate before linking so the interdependency pass only sees real items.
	checked := Validate(out, t, rep)
	out.Actions, out.Risks = checked.Actions, checked.Risks
	if err := x.pass(ctx, shared, "interdependencies", interdepTask+linkTargets(out), &out); err != nil {
		return Records{}, err
	}
	return out, nil
}

// linkTargets lists action and risk keys with titles for the interdependency pass.
func linkTargets(r Records) string {
	var sb strings.Builder
	for _, a := range r.Actions {
		fmt.Fprintf(&sb, "- %s: %s\n", a.Key, a.Title)
	}
	for _, k := range r.Risks {
		fmt.Fprintf(&sb, "- %s: %s\n", k.Key, k.TitleDescription)
	}
	if sb.Len() == 0 {
		return "(none)\n"
	}
	return sb.String()
}

// windowed handles transcripts too long to read whole: specialist passes per
// window, each validated, then one merge call.
func (x *Extractor) windowed(ctx context.Context, m MeetingInfo, t Transcript, rep *Report) (Records, error) {
	var parts []Records
	for i, w := range t.Windows(x.Window, x.Overlap) {
		r, err := x.specialists(ctx, m, w, rep)
		if err != nil {
			return Records{}, fmt.Errorf("fereastra %d: %w", i+1, err)
		}
		parts = append(parts, Validate(r, w, rep))
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	in, _ := json.Marshal(parts)
	var out Records
	err := x.call(ctx, llm.Request{System: mergePrompt, User: "Records from each window, in meeting order:\n" + string(in), Schema: Schema()}, &out)
	return out, err
}

func meetingContext(m MeetingInfo, transcript string) string {
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

// pass runs one register and copies that register into dst.
func (x *Extractor) pass(ctx context.Context, shared, register, task string, dst *Records) error {
	var r Records
	err := x.call(ctx, llm.Request{System: commonPrompt, Context: shared, User: task, Schema: SchemaFor(register)}, &r)
	if err != nil {
		return err
	}
	switch register {
	case "actions":
		dst.Actions = r.Actions
	case "decisions":
		dst.Decisions = r.Decisions
	case "risks":
		dst.Risks = r.Risks
	case "changes":
		dst.Changes = r.Changes
	case "interdependencies":
		dst.Interdependencies = r.Interdependencies
	}
	return nil
}

func (x *Extractor) call(ctx context.Context, req llm.Request, out *Records) error {
	b, err := x.LLM.JSON(ctx, req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return i18n.Errorf(err, "err.gateway.failed", "JSON invalid")
	}
	return nil
}
