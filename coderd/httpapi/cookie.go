package httpapi

import (
	"net/textproto"
	"strings"

	"github.com/coder/coder/codersdk"
)

// StripCoderCookies removes the session token from the cookie header provided.
func StripCoderCookies(header string) string {
	header = textproto.TrimString(header)
	cookies := []string{}

	var part string
	for len(header) > 0 { // continue since we have rest
		part, header, _ = strings.Cut(header, ";")
		part = textproto.TrimString(part)
		if part == "" {
			continue
		}
		name, _, _ := strings.Cut(part, "=")
		if name == codersdk.SessionTokenKey ||
			name == codersdk.OAuth2StateKey ||
			name == codersdk.OAuth2RedirectKey {
			continue
		}
		cookies = append(cookies, part)
	}
	return strings.Join(cookies, "; ")
}
