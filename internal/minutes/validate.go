package minutes

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	evidenceWindow = 90 * time.Second // quote must be near the time the model gave
	minTokenMatch  = 0.85             // share of quote words that must appear in order-insensitive match
)

// Report counts what validation removed or fixed.
type Report struct {
	Dropped        int // records with no verifiable evidence
	EvidenceFixed  int // speaker or time corrected from the transcript
	DeadlinesMoved int // non-ISO deadlines moved into notes
}

// normalize lower-cases, strips diacritics and punctuation, collapses spaces.
func normalize(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(t, strings.ToLower(s))
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

type normLine struct {
	Line
	norm  string
	words map[string]bool
}

type checker struct{ lines []normLine }

func newChecker(t Transcript) *checker {
	c := &checker{lines: make([]normLine, len(t))}
	for i, l := range t {
		n := normalize(l.Text)
		ws := map[string]bool{}
		for _, w := range strings.Fields(n) {
			ws[w] = true
		}
		c.lines[i] = normLine{Line: l, norm: n, words: ws}
	}
	return c
}

// verify finds the transcript line backing e and returns e corrected to that
// line's time and speaker, or false when the quote is not in the transcript.
func (c *checker) verify(e Evidence) (Evidence, bool) {
	q := normalize(e.Quote)
	if q == "" {
		return e, false
	}
	at, ok := ParseClock(e.At)
	var best *normLine
	bestScore := 0.0
	for i := range c.lines {
		l := &c.lines[i]
		if ok && (l.At < at-evidenceWindow || l.At > at+evidenceWindow) {
			continue
		}
		score := 0.0
		if strings.Contains(l.norm, q) {
			score = 1
		} else {
			qw := strings.Fields(q)
			hit := 0
			for _, w := range qw {
				if l.words[w] {
					hit++
				}
			}
			score = float64(hit) / float64(len(qw))
		}
		if score > bestScore || score == bestScore && best != nil && strings.EqualFold(l.Speaker, e.Speaker) && !strings.EqualFold(best.Speaker, e.Speaker) {
			best, bestScore = l, score
		}
	}
	if best == nil || bestScore < minTokenMatch {
		return e, false
	}
	return Evidence{At: Clock(best.At), Speaker: best.Speaker, Quote: e.Quote}, true
}

// verifyAll keeps verified evidence (max 3) and reports whether any survived.
func (c *checker) verifyAll(ev []Evidence, rep *Report) ([]Evidence, bool) {
	var out []Evidence
	for _, e := range ev {
		fixed, ok := c.verify(e)
		if !ok {
			continue
		}
		if fixed.At != e.At || fixed.Speaker != e.Speaker {
			rep.EvidenceFixed++
		}
		out = append(out, fixed)
		if len(out) == 3 {
			break
		}
	}
	return out, len(out) > 0
}

func oneOf(v string, allowed []string, def string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return def
}

// Validate drops records the transcript does not support and normalizes
// fields to the template's controlled values.
func Validate(r Records, t Transcript, rep *Report) Records {
	c := newChecker(t)
	var out Records
	for _, a := range r.Actions {
		ev, ok := c.verifyAll(a.Evidence, rep)
		if !ok || strings.TrimSpace(a.Title+a.Description) == "" {
			rep.Dropped++
			continue
		}
		a.Evidence = ev
		a.Status = oneOf(a.Status, ActionStatuses, DefaultActionStatus)
		if a.Deadline != "" {
			if _, err := time.Parse("2006-01-02", a.Deadline); err != nil {
				a.Notes = strings.TrimSpace(fmt.Sprintf("%s\nTermen menționat: %s", a.Notes, a.Deadline))
				a.Deadline = ""
				rep.DeadlinesMoved++
			}
		}
		out.Actions = append(out.Actions, a)
	}
	for _, d := range r.Decisions {
		ev, ok := c.verifyAll(d.Evidence, rep)
		if !ok || strings.TrimSpace(d.Statement) == "" {
			rep.Dropped++
			continue
		}
		d.Evidence = ev
		d.Status = oneOf(d.Status, DecisionStatuses, DefaultDecisionStatus)
		out.Decisions = append(out.Decisions, d)
	}
	for _, k := range r.Risks {
		ev, ok := c.verifyAll(k.Evidence, rep)
		if !ok || strings.TrimSpace(k.TitleDescription) == "" {
			rep.Dropped++
			continue
		}
		k.Evidence = ev
		k.Severity = oneOf(k.Severity, Levels, "")
		k.Probability = oneOf(k.Probability, Levels, "")
		out.Risks = append(out.Risks, k)
	}
	for _, i := range r.Interdependencies {
		ev, ok := c.verifyAll(i.Evidence, rep)
		if !ok || strings.TrimSpace(i.Dependency) == "" {
			rep.Dropped++
			continue
		}
		i.Evidence = ev
		i.Status = oneOf(i.Status, InterdepStatuses, DefaultInterdepStatus)
		out.Interdependencies = append(out.Interdependencies, i)
	}
	for _, ch := range r.Changes {
		ev, ok := c.verifyAll(ch.Evidence, rep)
		if !ok || strings.TrimSpace(ch.Title+ch.Summary) == "" {
			rep.Dropped++
			continue
		}
		ch.Evidence = ev
		ch.Status = oneOf(ch.Status, ChangeStatuses, DefaultChangeStatus)
		for _, p := range []*string{&ch.RequestorPriority, &ch.CostImpact, &ch.ScheduleImpact, &ch.QualityImpact, &ch.OverallImpact, &ch.Priority} {
			*p = oneOf(*p, Levels, "")
		}
		ch.ChangeType = oneOf(ch.ChangeType, ChangeTypes, "")
		out.Changes = append(out.Changes, ch)
	}
	return out
}
