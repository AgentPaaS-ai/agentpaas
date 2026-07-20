// Package urlutil provides small shared URL helpers.
package urlutil

import "net/url"

// StripUserinfo removes userinfo (user:password@) from a URL string.
// If the URL has no userinfo or cannot be parsed, returns the original URL.
// Preserving the original string when there is no userinfo avoids incidental
// re-serialization differences that would affect canonical digests.
func StripUserinfo(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}
	u.User = nil
	return u.String()
}
