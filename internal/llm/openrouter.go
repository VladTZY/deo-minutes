package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"deo-minutes/internal/i18n"
)

// OpenRouterURL is the chat completions endpoint (overridable in tests).
const OpenRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// OpenRouter calls a model through OpenRouter's OpenAI-compatible API with
// strict JSON-schema output (DEMO mode).
type OpenRouter struct {
	URL       string
	Key       string
	Model     string // e.g. anthropic/claude-opus-5.5
	Effort    string // reasoning effort: low | medium | high | xhigh | max
	MaxTokens int
	HTTP      *http.Client
}

// NewOpenRouter validates the settings.
func NewOpenRouter(key, model, effort string) (*OpenRouter, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(model) == "" {
		return nil, i18n.Errorf(nil, "err.openrouter.notConfigured")
	}
	if effort == "" {
		effort = "high"
	}
	return &OpenRouter{URL: OpenRouterURL, Key: key, Model: model, Effort: effort, MaxTokens: 64000,
		HTTP: &http.Client{Timeout: 20 * time.Minute}}, nil
}

type orText struct {
	Type         string         `json:"type"`
	Text         string         `json:"text"`
	CacheControl map[string]any `json:"cache_control,omitempty"`
}

type orMessage struct {
	Role    string   `json:"role"`
	Content []orText `json:"content"`
}

var ephemeral = map[string]any{"type": "ephemeral"}

// JSON streams one structured-output request and returns the JSON answer.
func (o *OpenRouter) JSON(ctx context.Context, req Request) ([]byte, error) {
	user := []orText{}
	if req.Context != "" {
		user = append(user, orText{Type: "text", Text: req.Context, CacheControl: ephemeral})
	}
	user = append(user, orText{Type: "text", Text: req.User})
	body := map[string]any{
		"model": o.Model,
		"messages": []orMessage{
			{Role: "system", Content: []orText{{Type: "text", Text: req.System, CacheControl: ephemeral}}},
			{Role: "user", Content: user},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "minute", "strict": true, "schema": req.Schema},
		},
		"reasoning":  map[string]any{"effort": o.Effort, "exclude": true},
		"max_tokens": o.MaxTokens,
		"stream":     true,
		"provider":   map[string]any{"require_parameters": true},
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
	hreq.Header.Set("X-Title", "DEO Minute")
	resp, err := o.HTTP.Do(hreq)
	if err != nil {
		return nil, orNetErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, orStatusErr(resp.StatusCode, msg)
	}
	return readOpenRouterStream(resp.Body)
}

func readOpenRouterStream(r io.Reader) ([]byte, error) { return readChatStream(r, "OpenRouter") }

// readChatStream accumulates the content deltas of an OpenAI-style chat
// completions SSE stream; provider names the service in errors.
func readChatStream(r io.Reader, provider string) ([]byte, error) {
	var out strings.Builder
	finish := ""
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") { // comments (": OPENROUTER PROCESSING"), event: lines and blanks
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev struct {
			Error *struct {
				Code    any    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Error != nil {
			return nil, i18n.Errorf(fmt.Errorf("%s: %v %s", provider, ev.Error.Code, ev.Error.Message), "err.gateway.failed", "eroare "+provider)
		}
		for _, c := range ev.Choices {
			out.WriteString(c.Delta.Content)
			if c.FinishReason != nil {
				finish = *c.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, orNetErr(err)
	}
	switch finish {
	case "length":
		return nil, i18n.Errorf(nil, "err.gateway.failed", "răspunsul a fost trunchiat")
	case "content_filter":
		return nil, i18n.Errorf(nil, "err.gateway.failed", "modelul a refuzat cererea")
	}
	if strings.TrimSpace(out.String()) == "" {
		return nil, i18n.Errorf(nil, "err.gateway.failed", "răspuns gol")
	}
	return []byte(out.String()), nil
}

func orStatusErr(status int, body []byte) error {
	cause := fmt.Errorf("openrouter http %d: %s", status, strings.TrimSpace(string(body)))
	slog.Warn("openrouter error", "status", status)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return i18n.Errorf(cause, "err.openrouter.auth")
	case http.StatusPaymentRequired:
		return i18n.Errorf(cause, "err.openrouter.credits")
	case http.StatusTooManyRequests, http.StatusRequestEntityTooLarge:
		return i18n.Errorf(cause, "err.gateway.limit")
	}
	return i18n.Errorf(cause, "err.gateway.failed", status)
}

func orNetErr(err error) error {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return i18n.Errorf(err, "err.gateway.timeout")
	}
	return i18n.Errorf(err, "err.gateway.failed", "conexiune eșuată")
}
