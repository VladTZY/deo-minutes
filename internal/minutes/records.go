// Package minutes turns a speaker-labelled transcript into the records that
// fill the project workbook (one record type per register sheet).
package minutes

// Evidence ties a record to what was actually said.
type Evidence struct {
	At      string `json:"at"`      // HH:MM:SS from meeting start
	Speaker string `json:"speaker"` // name as in the Webex roster
	Quote   string `json:"quote"`   // verbatim from the transcript
}

// ActionItem → "Action Item Register". Team is intentionally absent (Q14).
type ActionItem struct {
	Key         string     `json:"key"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Deadline    string     `json:"deadline"` // YYYY-MM-DD or ""
	Responsible string     `json:"responsible"`
	Status      string     `json:"status"`
	Notes       string     `json:"notes"`
	Evidence    []Evidence `json:"evidence"`
}

// Decision → "Decision Log".
type Decision struct {
	Key        string     `json:"key"`
	Statement  string     `json:"statement"`
	Rationale  string     `json:"rationale"`
	MadeBy     string     `json:"made_by"`
	AgreedWith string     `json:"agreed_with"`
	Status     string     `json:"status"`
	Outcomes   string     `json:"outcomes"`
	Evidence   []Evidence `json:"evidence"`
}

// Risk → "Risk Register".
type Risk struct {
	Key              string     `json:"key"`
	TitleDescription string     `json:"title_description"`
	Cause            string     `json:"cause"`
	Severity         string     `json:"severity"`
	Probability      string     `json:"probability"`
	Decision         string     `json:"decision"`
	ResponseActions  string     `json:"response_actions"`
	Owner            string     `json:"owner"`
	StatusUpdate     string     `json:"status_update"`
	Evidence         []Evidence `json:"evidence"`
}

// Interdependency → "Interdependencies".
type Interdependency struct {
	Key                string     `json:"key"`
	DependentOnProject string     `json:"dependent_on_project"`
	Dependency         string     `json:"dependency"`
	Status             string     `json:"status"`
	ActionKeys         []string   `json:"action_keys"`
	RiskKeys           []string   `json:"risk_keys"`
	Notes              string     `json:"notes"`
	Evidence           []Evidence `json:"evidence"`
}

// Change → "Change Log".
type Change struct {
	Key               string     `json:"key"`
	Title             string     `json:"title"`
	Summary           string     `json:"summary"`
	RequestedBy       string     `json:"requested_by"`
	RequestorPriority string     `json:"requestor_priority"`
	ChangeType        string     `json:"change_type"`
	CostImpact        string     `json:"cost_impact"`
	ScheduleImpact    string     `json:"schedule_impact"`
	QualityImpact     string     `json:"quality_impact"`
	OverallImpact     string     `json:"overall_impact"`
	Priority          string     `json:"priority"`
	Status            string     `json:"status"`
	Notes             string     `json:"notes"`
	Evidence          []Evidence `json:"evidence"`
}

// Records is everything extracted from one meeting.
type Records struct {
	Actions           []ActionItem      `json:"actions"`
	Decisions         []Decision        `json:"decisions"`
	Risks             []Risk            `json:"risks"`
	Interdependencies []Interdependency `json:"interdependencies"`
	Changes           []Change          `json:"changes"`
}

// Controlled values. English, matching the template (Q12, Q13).
var (
	ActionStatuses   = []string{"Open", "In progress", "Closed"}
	Levels           = []string{"Low", "Medium", "High"}
	ChangeTypes      = []string{"Small", "Large"}
	ChangeStatuses   = []string{"New", "Approved", "Escalated", "Rejected", "Analyzed", "Implemented"}
	DecisionStatuses = []string{"Active", "Superseded", "Revoked"}
	InterdepStatuses = []string{"Open", "Closed"}
)

// Default statuses for newly raised items.
const (
	DefaultActionStatus   = "Open"
	DefaultDecisionStatus = "Active"
	DefaultInterdepStatus = "Open"
	DefaultChangeStatus   = "New"
)
