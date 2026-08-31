package cli

import (
	"net/url"
	"regexp"
)

var googleDocID = regexp.MustCompile(`(?i)^https://docs\.google\.com/(document|spreadsheets)/d/([A-Za-z0-9_-]+)`)
var googleDriveFileID = regexp.MustCompile(`(?i)^https://drive\.google\.com/file/d/([A-Za-z0-9_-]+)`)

func rewriteGoogleShareURL(raw string) string {
	s := raw
	if m := googleDocID.FindStringSubmatch(s); len(m) == 3 {
		kind, id := m[1], m[2]
		format := "txt"
		if kind == "spreadsheets" {
			format = "csv"
		}
		return "https://docs.google.com/" + kind + "/d/" + id + "/export?format=" + format
	}
	if m := googleDriveFileID.FindStringSubmatch(s); len(m) == 2 {
		return "https://drive.google.com/uc?export=download&id=" + url.QueryEscape(m[1])
	}
	return raw
}
