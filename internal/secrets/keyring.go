// Package secrets stores credentials in the OS credential store
// (Windows Credential Manager, per-user, no admin rights needed).
// Values read here must never be logged or sent to the frontend.
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "DEOMinutes"

// Known credential names.
const (
	Gateway    = "gateway"    // bearer key for the Bedrock gateway (PROD)
	OpenRouter = "openrouter" // OpenRouter API key (DEMO)
	AWS        = "aws"        // JSON-encoded AWSCredentials for Transcribe
)

var ErrNotFound = errors.New("secret neconfigurat")

// AWSCredentials is stored as JSON under the AWS name.
type AWSCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token,omitempty"`
}

func Set(name, value string) error {
	if value == "" {
		return errors.New("valoare goală")
	}
	if err := keyring.Set(service, name, value); err != nil {
		return fmt.Errorf("salvare secret %s: %w", name, err)
	}
	return nil
}

func Get(name string) (string, error) {
	v, err := keyring.Get(service, name)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("citire secret %s: %w", name, err)
	}
	return v, nil
}

func Delete(name string) error {
	err := keyring.Delete(service, name)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// IsSet reports whether a secret exists, without exposing it.
func IsSet(name string) bool {
	_, err := Get(name)
	return err == nil
}

func SetAWS(c AWSCredentials) error {
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return errors.New("cheile AWS sunt incomplete")
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return Set(AWS, string(b))
}

func GetAWS() (AWSCredentials, error) {
	v, err := Get(AWS)
	if err != nil {
		return AWSCredentials{}, err
	}
	var c AWSCredentials
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return AWSCredentials{}, errors.New("cheile AWS salvate sunt corupte")
	}
	return c, nil
}
