package bundle

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	reAbsPath     = regexp.MustCompile(`(?m)^(\s*File:\s+)\S+`)
	reSizeBytes   = regexp.MustCompile(`(?m)^(\s*Size:\s+)\d+ bytes`)
	reBundleDigest = regexp.MustCompile(`(?m)^(\s*Bundle digest:\s+)[0-9a-f]{64}`)
	reFingerprint = regexp.MustCompile(`(?m)^(\s*Fingerprint:\s+)[0-9a-f ]+$`)
	reProvShortFP = regexp.MustCompile(`\([0-9a-f]{8}\)`)
)

// NormalizeInspectText replaces volatile paths, digests, and fingerprints for golden tests.
func NormalizeInspectText(s string) string {
	s = reAbsPath.ReplaceAllString(s, `${1}<bundle>`)
	s = reSizeBytes.ReplaceAllString(s, `${1}<size> bytes`)
	s = reBundleDigest.ReplaceAllString(s, `${1}<digest>`)
	s = reFingerprint.ReplaceAllString(s, `${1}<fingerprint>`)
	s = reProvShortFP.ReplaceAllString(s, "(<fp8>)")
	return s
}

// NormalizeInspectReport returns a copy with volatile fields cleared for golden JSON tests.
func NormalizeInspectReport(r *InspectReport) *InspectReport {
	if r == nil {
		return nil
	}
	out := *r
	out.Header.File = "<bundle>"
	out.Header.SizeBytes = 0
	out.Header.BundleDigest = ""
	if out.Publisher != nil {
		p := *out.Publisher
		p.Fingerprint = ""
		p.FingerprintDisplay = ""
		out.Publisher = &p
	}
	if out.Provenance != nil {
		p := *out.Provenance
		for i := range p.Entries {
			p.Entries[i].PublisherFingerprint = ""
		}
		out.Provenance = &p
	}
	if out.ProvenanceText != "" {
		out.ProvenanceText = NormalizeInspectText(out.ProvenanceText)
	}
	return &out
}

// MustMarshalInspectGoldenJSON marshals a normalized inspect report for golden comparison.
func MustMarshalInspectGoldenJSON(r *InspectReport) []byte {
	n := NormalizeInspectReport(r)
	data, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		panic(err)
	}
	return data
}

func goldenTextEqual(want, got string) bool {
	return strings.TrimSpace(NormalizeInspectText(want)) == strings.TrimSpace(NormalizeInspectText(got))
}

func goldenJSONEqual(want, got []byte) bool {
	var a, b InspectReport
	if err := json.Unmarshal(want, &a); err != nil {
		return false
	}
	if err := json.Unmarshal(got, &b); err != nil {
		return false
	}
	na := MustMarshalInspectGoldenJSON(&a)
	nb := MustMarshalInspectGoldenJSON(&b)
	return string(na) == string(nb)
}