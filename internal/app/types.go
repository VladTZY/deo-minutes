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
	StateRecorded            = "recorded"     // DEMO: audio saved, waiting for step 2
	StateTranscribing        = "transcribing" // DEMO: local transcriber running (Progress)
	StateTranscribed         = "transcribed"  // DEMO: transcript ready, waiting for step 3
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

// Outputs chooses what step 3 generates; at least one is set.
type Outputs struct {
	Minutes bool `json:"minutes"` // the Excel minutes (company workbook)
	Summary bool `json:"summary"` // the Word summary
}

// Result of a finished meeting. A path is empty when that output was not
// generated in this run.
type Result struct {
	ExcelPath      string `json:"excelPath"`
	SummaryPath    string `json:"summaryPath"`
	TranscriptPath string `json:"transcriptPath"`
	Folder         string `json:"folder"`
	Counts         Counts `json:"counts"`
}

// Steps a session has completed: 1 record, 2 transcribe, 3 generate the
// minutes and/or the summary.
type Steps struct {
	Recorded    bool `json:"recorded"`
	Transcribed bool `json:"transcribed"`
	Generated   bool `json:"generated"`
}

// Status is the live state of the current session.
type Status struct {
	State        string   `json:"state"`
	SessionID    string   `json:"sessionId"` // the session the steps apply to
	Mode         string   `json:"mode"`      // "demo" | "prod"
	Progress     float64  `json:"progress"`  // 0..1 while transcribing locally
	Steps        Steps    `json:"steps"`
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
	Mode       string             `json:"mode"`   // "demo" | "prod"
	DemoAI     string             `json:"demoAI"` // "openrouter" | "openai"
	OpenRouter OpenRouterSettings `json:"openrouter"`
	OpenAI     OpenAISettings     `json:"openai"`
	Gateway    GatewaySettings    `json:"gateway"`
	Transcribe TranscribeSettings `json:"transcribe"`
	Bot        BotSettings        `json:"bot"`
	Output     OutputSettings     `json:"output"`
	Secrets    SecretStatus       `json:"secrets"` // read-only: which secrets are stored
}

type OpenRouterSettings struct {
	Model string `json:"model"` // e.g. anthropic/claude-opus-5.5
}

type OpenAISettings struct {
	Model string `json:"model"` // e.g. gpt-6-astra
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
	OpenRouterKey bool `json:"openRouterKey"`
	OpenAIKey     bool `json:"openAIKey"`
	GatewayKey    bool `json:"gatewayKey"`
	AWS           bool `json:"aws"`
}

// HistoryItem is one past meeting (or imported recording).
type HistoryItem struct {
	ID            string `json:"id"`
	Date          string `json:"date"` // dd.mm.yyyy HH:MM
	Project       string `json:"project"`
	MeetingNo     int    `json:"meetingNo"`
	Mode          string `json:"mode"`
	Imported      string `json:"imported"`    // file name if imported, else empty
	ExcelPath     string `json:"excelPath"`   // empty if not generated
	SummaryPath   string `json:"summaryPath"` // empty if not generated
	Counts        Counts `json:"counts"`
	Steps         Steps  `json:"steps"`
	CanTranscribe bool   `json:"canTranscribe"` // audio exists (step 2 can run)
	CanGenerate   bool   `json:"canGenerate"`   // transcript exists (step 3 can run)
}

// LocalStatus says whether the local transcriber files are installed.
type LocalStatus struct {
	WhisperFound bool   `json:"whisperFound"`
	ModelFound   bool   `json:"modelFound"`
	VADFound     bool   `json:"vadFound"`
	ModelPath    string `json:"modelPath"`
}
