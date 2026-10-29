package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"deo-minutes/internal/i18n"
)

func fakeOpenAI(t *testing.T, status int, errBody string, chunks []string) (*OpenAI, *map[string]any, *http.Header) {
	t.Helper()
	var body map[string]any
	var hdr http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(errBody))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range chunks {
			q, _ := json.Marshal(c)
			_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":null}]}\n\n", q)
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(ts.Close)
	o, err := NewOpenAI("oa-key", "gpt-6-astra", "")
	if err != nil {
		t.Fatal(err)
	}
	o.URL = ts.URL
	return o, &body, &hdr
}

func TestOpenAIRequestAndStream(t *testing.T) {
	o, body, hdr := fakeOpenAI(t, http.StatusOK, "", []string{`{"actions":`, `[]}`})
	got, err := o.JSON(context.Background(), Request{System: "sys", Context: "transcript", User: "task", Schema: map[string]any{"type": "object"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"actions":[]}` {
		t.Fatalf("got %s", got)
	}
	b := *body
	rf := b["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if b["model"] != "gpt-6-astra" || rf["type"] != "json_schema" || js["strict"] != true || js["schema"] == nil {
		t.Errorf("request: %v", b)
	}
	if b["reasoning_effort"] != "high" || b["stream"] != true || b["store"] != false || b["max_completion_tokens"] == nil {
		t.Errorf("request options: %v", b)
	}
	for _, k := range []string{"temperature", "max_tokens", "provider", "reasoning"} {
		if _, ok := b[k]; ok {
			t.Errorf("%s must not be sent to OpenAI", k)
		}
	}
	msgs := b["messages"].([]any)
	sys, user := msgs[0].(map[string]any), msgs[1].(map[string]any)
	if sys["role"] != "developer" || sys["content"] != "sys" || user["content"] != "transcript\n\ntask" {
		t.Errorf("messages: %v", msgs)
	}
	if hdr.Get("Authorization") != "Bearer oa-key" {
		t.Errorf("auth: %v", *hdr)
	}
}

func TestOpenAIErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		key    string
	}{
		{http.StatusUnauthorized, `{"error":{"code":"invalid_api_key"}}`, "err.openai.auth"},
		{http.StatusTooManyRequests, `{"error":{"code":"insufficient_quota"}}`, "err.openai.credits"},
		{http.StatusTooManyRequests, `{"error":{"code":"rate_limit_exceeded"}}`, "err.gateway.limit"},
		{http.StatusNotFound, `{"error":{"code":"model_not_found"}}`, "err.openai.model"},
		{http.StatusBadRequest, `{"error":{"message":"bad"}}`, "err.gateway.failed"},
	}
	for _, c := range cases {
		o, _, _ := fakeOpenAI(t, c.status, c.body, nil)
		_, err := o.JSON(context.Background(), Request{Schema: map[string]any{}})
		var ue *i18n.UserError
		if !errors.As(err, &ue) || ue.Key != c.key {
			t.Errorf("status %d %s: %v", c.status, c.body, err)
		}
	}
	if _, err := NewOpenAI("", "m", ""); err == nil {
		t.Fatal("missing key must fail")
	}
}
