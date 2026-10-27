// Package i18n holds every user-facing string. ro.json is the single source;
// the frontend loads the same map through the Translations binding.
package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed ro.json
var roJSON []byte

var ro map[string]string

func init() {
	if err := json.Unmarshal(roJSON, &ro); err != nil {
		panic(fmt.Sprintf("i18n: ro.json invalid: %v", err))
	}
}

// T returns the Romanian string for key, replacing {0}, {1}… with args.
// Unknown keys return the key itself so a missing string is visible, not blank.
func T(key string, args ...any) string {
	s, ok := ro[key]
	if !ok {
		return key
	}
	for i, a := range args {
		s = strings.ReplaceAll(s, fmt.Sprintf("{%d}", i), fmt.Sprint(a))
	}
	return s
}

// All returns a copy of the translation map for the frontend.
func All() map[string]string {
	out := make(map[string]string, len(ro))
	for k, v := range ro {
		out[k] = v
	}
	return out
}

// Has reports whether key exists.
func Has(key string) bool { _, ok := ro[key]; return ok }

// UserError is an error whose message is already a Romanian, user-facing string.
type UserError struct {
	Key  string
	Args []any
	Err  error // underlying cause, for logs only
}

func (e *UserError) Error() string { return T(e.Key, e.Args...) }
func (e *UserError) Unwrap() error { return e.Err }

// Errorf builds a UserError.
func Errorf(cause error, key string, args ...any) *UserError {
	return &UserError{Key: key, Args: args, Err: cause}
}
