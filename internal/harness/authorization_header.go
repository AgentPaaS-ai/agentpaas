package harness

import (
	"encoding/base64"
	"strings"
)

func authorizationHeaderValue(header, value string) string {
	if !strings.EqualFold(header, "Authorization") {
		return value
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	for _, scheme := range []string{"basic ", "bearer ", "token ", "digest "} {
		if strings.HasPrefix(lower, scheme) {
			return value
		}
	}
	if strings.Contains(value, ":") {
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(value))
	}
	return value
}
