package install

import (
	"strings"
	"unicode"
)

// compareAgentVersions returns -1 if a<b, 0 if equal, 1 if a>b (semver-ish major.minor.patch).
func compareAgentVersions(a, b string) int {
	pa := parseVersionParts(a)
	pb := parseVersionParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseVersionParts(v string) [3]int {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i] = atoiDigits(parts[i])
	}
	return out
}

func atoiDigits(s string) int {
	s = strings.TrimSpace(s)
	var n int
	for _, r := range s {
		if !unicode.IsDigit(r) {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// isVersionDecrease reports whether newVer is strictly less than priorVer.
func isVersionDecrease(priorVer, newVer string) bool {
	return compareAgentVersions(newVer, priorVer) < 0
}