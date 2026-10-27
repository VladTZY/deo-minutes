package bot

// Selectors is the only place that knows what the Webex web client looks like.
// Matching is by visible text, aria-label, title or placeholder (lower-case
// "contains"), not by CSS classes, because Webex renames classes often. The bot
// browser runs with --lang=en-US so these English strings are stable.
// Tune this table from --selftest reports (PLAN.md S1).
type Selectors struct {
	CookieAccept    []string `json:"cookieAccept"`
	JoinFromBrowser []string `json:"joinFromBrowser"`
	NameInput       []string `json:"nameInput"`
	EmailInput      []string `json:"emailInput"`
	PasswordInput   []string `json:"passwordInput"`
	Next            []string `json:"next"`
	JoinButton      []string `json:"joinButton"`
	MuteMic         []string `json:"muteMic"`
	StopVideo       []string `json:"stopVideo"`
	Leave           []string `json:"leave"`
	LeaveConfirm    []string `json:"leaveConfirm"`
	Participants    []string `json:"participants"`

	LobbyText         []string `json:"lobbyText"`
	EndedText         []string `json:"endedText"`
	RemovedText       []string `json:"removedText"`
	WrongPasswordText []string `json:"wrongPasswordText"`

	// Roster: the participants panel container is the element whose
	// aria-label contains one of ParticipantsPanel; each role=listitem in it is
	// one participant. A participant (or video tile) is speaking when its
	// aria-label/title contains one of SpeakingMarkers, or its class contains
	// one of ActiveSpeakerClasses.
	ParticipantsPanel    []string `json:"participantsPanel"`
	SpeakingMarkers      []string `json:"speakingMarkers"`
	ActiveSpeakerClasses []string `json:"activeSpeakerClasses"`
}

// DefaultSelectors reflects the Webex web client as far as known before S1.
var DefaultSelectors = Selectors{
	CookieAccept:    []string{"accept all", "accept cookies", "i accept"},
	JoinFromBrowser: []string{"join from your browser", "join from browser", "join from this browser", "use web app", "join from the web"},
	NameInput:       []string{"your name", "display name", "full name", "name"},
	EmailInput:      []string{"email address", "email"},
	PasswordInput:   []string{"meeting password", "password"},
	Next:            []string{"next", "continue"},
	JoinButton:      []string{"join meeting", "join now", "join"},
	MuteMic:         []string{"mute"},
	StopVideo:       []string{"stop video", "turn off video", "turn off camera"},
	Leave:           []string{"leave meeting", "leave"},
	LeaveConfirm:    []string{"leave meeting", "leave"},
	Participants:    []string{"participants", "show participants"},

	LobbyText:         []string{"lobby", "waiting for the host", "let you in", "host will admit you", "wait for the host"},
	EndedText:         []string{"meeting has ended", "host has ended", "meeting ended", "you've left the meeting", "you left the meeting"},
	RemovedText:       []string{"removed you", "you were removed", "you have been removed", "expelled"},
	WrongPasswordText: []string{"incorrect password", "wrong password", "password is incorrect", "invalid password"},

	ParticipantsPanel:    []string{"participant"},
	SpeakingMarkers:      []string{"is speaking", "speaking", "is talking"},
	ActiveSpeakerClasses: []string{"active-speaker", "activespeaker", "speaking"},
}
