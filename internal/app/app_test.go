package app

import (
	"testing"

	"deo-minutes/internal/i18n"
)

func TestEveryStateHasLabel(t *testing.T) {
	for _, s := range []string{StateIdle, StateStartingBrowser, StateConnecting, StateLobby, StateRecording,
		StateFinishingTranscript, StateGenerating, StateSaving, StateDone, StateError} {
		if !i18n.Has("state." + s) {
			t.Errorf("ro.json lacks state.%s", s)
		}
	}
}
