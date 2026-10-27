package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"deo-minutes/internal/i18n"
)

// fakeMessages serves /v1/messages as an SSE stream returning text, and
// records the last request.
func fakeMessages(t *testing.T, status int, text, stop string) (*httptest.Server, *map[string]any, *http.Header) {
	t.Helper()
	var body map[string]any
	var hdr http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/messages") {
			http.NotFound(w, r)
			return
		}
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"x","message":"nope"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		q, _ := json.Marshal(text)
		events := []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"anthropic.claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta` + "\n" + fmt.Sprintf(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, q),
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
			`event: message_delta` + "\n" + fmt.Sprintf(`data: {"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":5}}`, stop),
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		}
		for _, e := range events {
			_, _ = fmt.Fprint(w, e+"\n\n")
		}
	}))
	t.Cleanup(ts.Close)
	return ts, &body, &hdr
}

func TestAnthropicStructuredOutputRequest(t *testing.T) {
	ts, body, hdr := fakeMessages(t, http.StatusOK, `{"actions":[]}`, "end_turn")
	c, err := NewAnthropic(context.Background(), Config{BaseURL: ts.URL, Region: "eu-central-1",
		Model: "anthropic.claude-opus-5-5", Auth: AuthBearer, Key: "gw-key"})
	if err != nil {
		t.Fatal(err)
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}, "additionalProperties": false}
	got, err := c.JSON(context.Background(), Request{System: "sys", User: "transcript", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"actions":[]}` {
		t.Fatalf("got %s", got)
	}
	b := *body
	oc := b["output_config"].(map[string]any)
	if b["model"] != "anthropic.claude-opus-5-5" || b["stream"] != true || oc["effort"] != "high" {
		t.Errorf("request: %v", b)
	}
	if f := oc["format"].(map[string]any); f["type"] != "json_schema" || f["schema"] == nil {
		t.Errorf("format: %v", f)
	}
	for _, banned := range []string{"temperature", "tool_choice", "thinking"} {
		if _, ok := b[banned]; ok {
			t.Errorf("%s must not be sent", banned)
		}
	}
	sys := b["system"].([]any)[0].(map[string]any)
	if sys["cache_control"] == nil {
		t.Error("system prompt not cached")
	}
	if hdr.Get("Authorization") != "Bearer gw-key" || hdr.Get("X-Api-Key") != "" {
		t.Errorf("auth headers: %v", *hdr)
	}
}

func TestAnthropicAPIKeyAuth(t *testing.T) {
	ts, _, hdr := fakeMessages(t, http.StatusOK, `{}`, "end_turn")
	c, err := NewAnthropic(context.Background(), Config{BaseURL: ts.URL, Region: "eu-central-1", Model: "m", Auth: AuthAPIKey, Key: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.JSON(context.Background(), Request{Schema: map[string]any{"type": "object"}}); err != nil {
		t.Fatal(err)
	}
	if hdr.Get("Authorization") != "Bearer k1" { // Bedrock API keys are bearer tokens
		t.Errorf("auth: %v", *hdr)
	}
}

func TestAnthropicErrors(t *testing.T) {
	cases := []struct {
		status int
		stop   string
		key    string
	}{
		{http.StatusUnauthorized, "", "err.gateway.auth"},
		{http.StatusTooManyRequests, "", "err.gateway.limit"},
		{http.StatusOK, "max_tokens", "err.gateway.failed"},
	}
	for _, c := range cases {
		ts, _, _ := fakeMessages(t, c.status, `{"a":`, c.stop)
		cl, err := NewAnthropic(context.Background(), Config{BaseURL: ts.URL, Region: "eu-central-1", Model: "m", Auth: AuthAPIKey, Key: "k"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = cl.JSON(context.Background(), Request{Schema: map[string]any{"type": "object"}})
		var ue *i18n.UserError
		if !errors.As(err, &ue) || ue.Key != c.key {
			t.Errorf("status %d stop %q: got %v", c.status, c.stop, err)
		}
	}
}

func TestAnthropicMissingConfig(t *testing.T) {
	if _, err := NewAnthropic(context.Background(), Config{Model: "m", Auth: AuthBearer}); err == nil {
		t.Fatal("bearer without key must fail")
	}
	if _, err := NewAnthropic(context.Background(), Config{Auth: AuthAPIKey, Key: "k"}); err == nil {
		t.Fatal("missing model must fail")
	}
}
