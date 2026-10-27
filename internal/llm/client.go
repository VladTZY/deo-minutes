// Package llm sends one prompt to Claude through AWS Bedrock (directly or via
// the company gateway) and returns JSON that matches a given schema.
package llm

import (
	"context"
)

// Request is one structured-output call.
type Request struct {
	System string         // stable instructions (cached)
	User   string         // the transcript and meeting facts
	Schema map[string]any // JSON schema the answer must match
}

// Client returns the model's JSON answer for a request.
type Client interface {
	JSON(ctx context.Context, req Request) ([]byte, error)
}

// Func adapts a function to Client (used by tests).
type Func func(ctx context.Context, req Request) ([]byte, error)

func (f Func) JSON(ctx context.Context, req Request) ([]byte, error) { return f(ctx, req) }
