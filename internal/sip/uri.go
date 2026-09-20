package sip

import "strings"

// RequestURIUser returns the user part of a SIP Request-URI (sip:user@host).
// Empty when the URI has no user (sip:host or tel:).
func RequestURIUser(uri string) string {
	uri = strings.TrimSpace(uri)
	uri = strings.Trim(uri, "<>")
	if i := strings.IndexAny(uri, " \t"); i >= 0 {
		uri = uri[:i]
	}
	lower := strings.ToLower(uri)
	switch {
	case strings.HasPrefix(lower, "sips:"):
		uri = uri[5:]
	case strings.HasPrefix(lower, "sip:"):
		uri = uri[4:]
	}
	at := strings.IndexByte(uri, '@')
	if at <= 0 {
		return ""
	}
	user := uri[:at]
	if i := strings.IndexByte(user, ':'); i >= 0 {
		user = user[:i]
	}
	return user
}
