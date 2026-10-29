package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"deo-minutes/internal/i18n"
)

// OpenAIURL is the chat completions endpoint (overridable in tests).
const OpenAIURL = "https://api.openai.com/v1/chat/completions"

// OpenAI calls an OpenAI model directly with strict JSON-schema output
// (DEMO mode, the alternative to OpenRouter). OpenAI caches long prompt
// prefixes by itself, so the shared context goes first and unmarked.
type OpenAI struct {
	URL       string
	Key       string
	Model     string // e.g. gpt-6-astra
	Effort    string // reasoning_effort: low | medium | high | xhigh | max
	MaxTokens int
	HTTP      *http.Client
}

// NewOpenAI validates the settings.
func NewOpenAI(key, model, effort string) (*OpenAI, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(model) == "" {
		return nil, i18n.Errorf(nil, "err.openai.notConfigured")
	}
	if effort == "" {
		effort = "high"
	}
	return &OpenAI{URL: OpenAIURL, Key: key, Model: model, Effort: effort, MaxTokens: 64000,
		HTTP: &http.Client{Timeout: 20 * time.Minute}}, nil
}

type oaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// JSON streams one structured-output request and returns the JSON answer.
func (o *OpenAI) JSON(ctx context.Context, req Request) ([]byte, error) {
	user := req.User
	if req.Context != "" {
		user = req.Context + "\n\n" + req.User
	}
	body := map[string]any{
		"model": o.Model,
		"messages": []oaMessage{
			{Role: "developer", Content: req.System},
			{Role: "user", Content: user},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "minute", "strict": true, "schema": req.Schema},
		},
		"reasoning_effort":      o.Effort,
		"max_completion_tokens": o.MaxTokens,
		"stream":                true,
		"store":                 false,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.URL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Authorization", "Bearer "+o.Key)
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := o.HTTP.Do(hreq)
	if err != nil {
		return nil, orNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, oaStatusErr(resp.StatusCode, msg)
	}
	return readChatStream(resp.Body, "OpenAI")
}

func oaStatusErr(status int, body []byte) error {
	cause := fmt.Errorf("openai http %d: %s", status, strings.TrimSpace(string(body)))
	slog.Warn("openai error", "status", status)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return i18n.Errorf(cause, "err.openai.auth")
	case status == http.StatusTooManyRequests && bytes.Contains(body, []byte("insufficient_quota")):
		return i18n.Errorf(cause, "err.openai.credits")
	case status == http.StatusNotFound && bytes.Contains(body, []byte("model")):
		return i18n.Errorf(cause, "err.openai.model")
	case status == http.StatusTooManyRequests || status == http.StatusRequestEntityTooLarge:
		return i18n.Errorf(cause, "err.gateway.limit")
	}
	return i18n.Errorf(cause, "err.gateway.failed", status)
}
