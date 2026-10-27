package llm

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/bedrock"
	"github.com/anthropics/anthropic-sdk-go/lib/betafallback"
	"github.com/anthropics/anthropic-sdk-go/option"

	"deo-minutes/internal/i18n"
	"deo-minutes/internal/secrets"
)

// Auth modes for the gateway.
const (
	AuthAPIKey = "api_key" // Bedrock API key (the SDK's Mantle auth, sent as a bearer token)
	AuthBearer = "bearer"  // company gateway that does its own auth: our key as Authorization: Bearer, SDK auth skipped
	AuthSigV4  = "sigv4"   // AWS access keys (same as Transcribe)
)

// Config selects endpoint, model and credentials.
type Config struct {
	BaseURL       string // empty = Bedrock Mantle in Region; else the company gateway
	Region        string
	Model         string // e.g. anthropic.claude-opus-5-5
	FallbackModel string // used if the main model refuses; empty disables
	Auth          string
	Key           string // for AuthAPIKey / AuthBearer
	AWS           secrets.AWSCredentials
	Effort        anthropic.OutputConfigEffort
	MaxTokens     int64
	Timeout       time.Duration
}

// Anthropic calls Claude through the Bedrock Mantle (Messages API) endpoint.
type Anthropic struct {
	cfg Config
	msg anthropic.MessageService
}

// NewAnthropic builds the client. Credentials stay in memory.
func NewAnthropic(ctx context.Context, cfg Config) (*Anthropic, error) {
	if cfg.Model == "" {
		return nil, i18n.Errorf(nil, "err.gateway.notConfigured")
	}
	if cfg.Effort == "" {
		cfg.Effort = anthropic.OutputConfigEffortHigh
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 64000
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Minute
	}
	mc := bedrock.MantleClientConfig{AWSRegion: cfg.Region, BaseURL: cfg.BaseURL}
	var opts []option.RequestOption
	switch cfg.Auth {
	case AuthAPIKey:
		if cfg.Key == "" {
			return nil, i18n.Errorf(nil, "err.gateway.notConfigured")
		}
		mc.APIKey = cfg.Key
	case AuthBearer:
		if cfg.Key == "" {
			return nil, i18n.Errorf(nil, "err.gateway.notConfigured")
		}
		mc.SkipAuth = true
		opts = append(opts, option.WithHeader("Authorization", "Bearer "+cfg.Key))
	case AuthSigV4:
		if cfg.AWS.AccessKeyID == "" {
			return nil, i18n.Errorf(nil, "err.gateway.notConfigured")
		}
		mc.AWSAccessKey, mc.AWSSecretAccessKey, mc.AWSSessionToken = cfg.AWS.AccessKeyID, cfg.AWS.SecretAccessKey, cfg.AWS.SessionToken
	default:
		return nil, i18n.Errorf(nil, "err.gateway.notConfigured")
	}
	if cfg.FallbackModel != "" {
		opts = append(opts, option.WithMiddleware(betafallback.BetaRefusalFallbackMiddleware(
			[]anthropic.BetaFallbackParam{{Model: anthropic.Model(cfg.FallbackModel)}})))
	}
	opts = append(opts, option.WithMaxRetries(3))
	c, err := bedrock.NewMantleClient(ctx, mc, opts...)
	if err != nil {
		return nil, i18n.Errorf(err, "err.gateway.notConfigured")
	}
	return &Anthropic{cfg: cfg, msg: c.Messages}, nil
}

// JSON streams one request (long transcripts, large outputs) and returns the
// text of the answer, which structured outputs constrain to req.Schema.
func (a *Anthropic) JSON(ctx context.Context, req Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	defer cancel()
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.cfg.Model),
		MaxTokens: a.cfg.MaxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         req.System,
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(req.User))},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: a.cfg.Effort,
			Format: anthropic.JSONOutputFormatParam{Schema: req.Schema},
		},
	}
	stream := a.msg.NewStreaming(ctx, params, betafallback.WithBetaFallbackState(&betafallback.BetaFallbackState{}))
	var msg anthropic.Message
	for stream.Next() {
		if err := msg.Accumulate(stream.Current()); err != nil {
			return nil, i18n.Errorf(err, "err.gateway.failed", "răspuns invalid")
		}
	}
	if err := stream.Err(); err != nil {
		return nil, classify(err)
	}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return nil, i18n.Errorf(nil, "err.gateway.failed", "modelul a refuzat cererea")
	case anthropic.StopReasonMaxTokens:
		return nil, i18n.Errorf(nil, "err.gateway.failed", "răspunsul a fost trunchiat")
	}
	var sb strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	if sb.Len() == 0 {
		return nil, i18n.Errorf(nil, "err.gateway.failed", "răspuns gol")
	}
	return []byte(sb.String()), nil
}

// classify maps API errors to Romanian messages; the cause stays for the log.
func classify(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return i18n.Errorf(err, "err.gateway.auth")
		case http.StatusTooManyRequests, http.StatusRequestEntityTooLarge:
			return i18n.Errorf(err, "err.gateway.limit")
		}
		return i18n.Errorf(err, "err.gateway.failed", apiErr.StatusCode)
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout() {
		return i18n.Errorf(err, "err.gateway.timeout")
	}
	return i18n.Errorf(err, "err.gateway.failed", "conexiune eșuată")
}
