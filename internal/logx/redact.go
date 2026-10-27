package logx

import (
	"log/slog"
	"net/url"
	"regexp"
	"strings"
)

const redacted = "[ascuns]"

// sensitiveKeys are attribute names whose values are always hidden.
var sensitiveKeys = []string{"token", "secret", "password", "pwd", "authorization", "apikey", "api_key", "key", "credential", "transcript", "quote"}

var (
	awsKeyRe    = regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)
	bearerRe    = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+/]+=*`)
	urlRe       = regexp.MustCompile(`https?://[^\s"']+`)
	longTokenRe = regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`)
)

func isSensitiveKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range sensitiveKeys {
		if strings.HasSuffix(k, s) || strings.HasPrefix(k, s+"_") {
			return true
		}
	}
	return false
}

// String removes secrets and URL query strings (Webex MTID, pwd, tokens) from s.
func String(s string) string {
	s = awsKeyRe.ReplaceAllString(s, redacted)
	s = bearerRe.ReplaceAllString(s, "Bearer "+redacted)
	s = urlRe.ReplaceAllStringFunc(s, URL)
	s = longTokenRe.ReplaceAllString(s, redacted)
	return s
}

// URL keeps scheme, host and path but drops query and fragment.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return redacted
	}
	u.User = nil
	if u.RawQuery != "" {
		u.RawQuery = "…"
	}
	u.Fragment = ""
	return u.String()
}

func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, String(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, String(err.Error()))
		}
	}
	return a
}
