package bot

import (
	"net/url"
	"strings"

	"deo-minutes/internal/i18n"
)

// MeetingLink is a validated Webex meeting URL.
type MeetingLink struct {
	URL      string
	Password string // from a pwd= query parameter, if the link carries one
}

// ParseLink accepts only https links on *.webex.com.
func ParseLink(raw string) (MeetingLink, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return MeetingLink{}, i18n.Errorf(nil, "err.link.empty")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return MeetingLink{}, i18n.Errorf(err, "err.link.invalid")
	}
	host := strings.ToLower(u.Hostname())
	if host != "webex.com" && !strings.HasSuffix(host, ".webex.com") {
		return MeetingLink{}, i18n.Errorf(nil, "err.link.invalid")
	}
	if u.Port() != "" && u.Port() != "443" {
		return MeetingLink{}, i18n.Errorf(nil, "err.link.invalid")
	}
	return MeetingLink{URL: u.String(), Password: u.Query().Get("pwd")}, nil
}
