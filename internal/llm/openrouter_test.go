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

func fakeOpenRouter(t *testing.T, status int, chunks []string, finish string) (*OpenRouter, *map[string]any, *http.Header) {
	t.Helper()
	var body map[string]any
	var hdr http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"nope"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": OPENROUTER PROCESSING\n\n")
		for _, c := range chunks {
			q, _ := json.Marshal(c)
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":null}]}\n\n", q)
		}
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":%q}]}\n\n", finish)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(ts.Close)
	o, err := NewOpenRouter("or-key", "anthropic/claude-opus-5.5", "")
	if err != nil {
		t.Fatal(err)
	}
	o.URL = ts.URL
	return o, &body, &hdr
}

func TestOpenRouterRequestAndStream(t *testing.T) {
	o, body, hdr := fakeOpenRouter(t, http.StatusOK, []string{`{"actions":`, `[]}`}, "stop")
	schema := map[string]any{"type": "object"}
	got, err := o.JSON(context.Background(), Request{System: "sys", Context: "transcript", User: "task", Schema: schema})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"actions":[]}` {
		t.Fatalf("got %s", got)
	}
	b := *body
	rf := b["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if b["model"] != "anthropic/claude-opus-5.5" || rf["type"] != "json_schema" || js["strict"] != true || js["schema"] == nil {
		t.Errorf("request: %v", b)
	}
	if b["reasoning"].(map[string]any)["effort"] != "high" || b["provider"].(map[string]any)["require_parameters"] != true || b["stream"] != true {
		t.Errorf("request options: %v", b)
	}
	msgs := b["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].([]any)
	if user[0].(map[string]any)["cache_control"] == nil || user[1].(map[string]any)["cache_control"] != nil || user[1].(map[string]any)["text"] != "task" {
		t.Errorf("user content: %v", user)
	}
	if _, ok := b["temperature"]; ok {
		t.Error("temperature must not be sent")
	}
	if hdr.Get("Authorization") != "Bearer or-key" {
		t.Errorf("auth: %v", *hdr)
	}
}

func TestOpenRouterErrors(t *testing.T) {
	cases := []struct {
		status int
		finish string
		key    string
	}{
		{http.StatusUnauthorized, "", "err.openrouter.auth"},
		{http.StatusPaymentRequired, "", "err.openrouter.credits"},
		{http.StatusTooManyRequests, "", "err.gateway.limit"},
		{http.StatusOK, "length", "err.gateway.failed"},
	}
	for _, c := range cases {
		o, _, _ := fakeOpenRouter(t, c.status, []string{`{"a":`}, c.finish)
		_, err := o.JSON(context.Background(), Request{Schema: map[string]any{}})
		var ue *i18n.UserError
		if !errors.As(err, &ue) || ue.Key != c.key {
			t.Errorf("status %d finish %q: %v", c.status, c.finish, err)
		}
	}
}

func TestOpenRouterStreamError(t *testing.T) {
	_, err := readOpenRouterStream(strings.NewReader("data: {\"error\":{\"code\":502,\"message\":\"upstream\"}}\n\n"))
	if err == nil {
		t.Fatal("mid-stream error must fail")
	}
	if _, err := NewOpenRouter("", "m", ""); err == nil {
		t.Fatal("missing key must fail")
	}
}
