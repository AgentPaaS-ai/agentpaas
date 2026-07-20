// Package strutil provides small shared string helpers.
package strutil

// SplitLines splits s on '\n', including a trailing fragment without a newline.
func SplitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// SplitCompleteLines splits newline-delimited data and returns only complete
// lines (terminated by '\n'). Trailing fragments without a newline are held
// back so the caller can avoid advancing past incomplete data.
func SplitCompleteLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	return lines
}
