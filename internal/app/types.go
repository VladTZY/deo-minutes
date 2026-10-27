package app

// Types shared with the frontend through Wails bindings. Field names are the
// contract with frontend/; change them together with the UI.

// Event names emitted to the frontend.
const (
	EventStatus     = "status"     // payload: Status
	EventTranscript = "transcript" // payload: string, one "[HH:MM:SS] Nume: text" line
)

// State values (also keys "state.<value>" in ro.json).
const (
	StateIdle                = "idle"
	StateStartingBrowser     = "starting_browser"
	StateConnecting          = "connecting"
	StateLobby               = "lobby"
	StateRecording           = "recording"
	StateFinishingTranscript = "finishing_transcript"
	StateGenerating          = "generating"
	StateSaving              = "saving"
	StateDone                = "done"
	StateError               = "error"
)

// StartRequest is what the user fills in on the main page.
type StartRequest struct {
	Link       string `json:"link"`
	Password   string `json:"password"` // optional; kept in memory for this session only
	Project    string `json:"project"`
	MeetingNo  int    `json:"meetingNo"`
	PreparedBy string `json:"preparedBy"`
}

// StartDefaults pre-fills the main page.
type StartDefaults struct {
	Project    string `json:"project"`
	MeetingNo  int    `json:"meetingNo"`
	PreparedBy string `json:"preparedBy"`
}

// Counts of records per register.
type Counts struct {
	Actions           int `json:"actions"`
	Decisions         int `json:"decisions"`
	Risks             int `json:"risks"`
	Interdependencies int `json:"interdependencies"`
	Changes           int `json:"changes"`
}

// Result of a finished meeting.
type Result struct {
	ExcelPath      string `json:"excelPath"`
	TranscriptPath string `json:"transcriptPath"`
	Folder         string `json:"folder"`
	Counts         Counts `json:"counts"`
}

// Status is the live state of the current session.
type Status struct {
	State        string   `json:"state"`
	Label        string   `json:"label"`        // Romanian, ready to show
	StartedAt    int64    `json:"startedAt"`    // unix ms when recording started, 0 before
	Participants int      `json:"participants"` // people seen in the meeting (bot excluded)
	Error        string   `json:"error"`        // Romanian message when State == "error"
	Warnings     []string `json:"warnings"`     // Romanian, non-fatal
	Result       *Result  `json:"result"`       // set when State == "done"
	CanStop      bool     `json:"canStop"`
}

// Settings is everything the Setări page edits, without secrets.
type Settings struct {
	Gateway    GatewaySettings    `json:"gateway"`
	Transcribe TranscribeSettings `json:"transcribe"`
	Bot        BotSettings        `json:"bot"`
	Output     OutputSettings     `json:"output"`
	Secrets    SecretStatus       `json:"secrets"` // read-only: which secrets are stored
}

type GatewaySettings struct {
	BaseURL string `json:"baseUrl"` // empty = AWS Bedrock directly
	Region  string `json:"region"`
	ModelID string `json:"modelId"`
	Auth    string `json:"auth"` // "sigv4" | "api_key" | "bearer"
}

type TranscribeSettings struct {
	Region        string `json:"region"`
	MultiLanguage bool   `json:"multiLanguage"` // ro-RO + en-US detection
}

type BotSettings struct {
	DisplayName     string `json:"displayName"`
	Email           string `json:"email"`
	BrowserPath     string `json:"browserPath"`
	LobbyTimeoutMin int    `json:"lobbyTimeoutMin"`
	SilenceEndMin   int    `json:"silenceEndMin"`
	Headless        bool   `json:"headless"`
}

type OutputSettings struct {
	Dir           string `json:"dir"`
	RetentionDays int    `json:"retentionDays"`
	KeepAudio     bool   `json:"keepAudio"`
}

type SecretStatus struct {
	GatewayKey bool `json:"gatewayKey"`
	AWS        bool `json:"aws"`
}

// HistoryItem is one past meeting.
type HistoryItem struct {
	ID        string `json:"id"`
	Date      string `json:"date"` // dd.mm.yyyy HH:MM
	Project   string `json:"project"`
	MeetingNo int    `json:"meetingNo"`
	ExcelPath string `json:"excelPath"` // empty if not generated
	Counts    Counts `json:"counts"`
	CanResume bool   `json:"canResume"` // transcript exists but no Excel
}
