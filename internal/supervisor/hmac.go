package supervisor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// verifyProgressHMAC recomputes the HMAC over the canonical progress event and
// compares it to the supplied HMAC. The canonical form is the JSON of the
// ProgressEvent with the HMAC field cleared.
func verifyProgressHMAC(p ProgressEvent, key []byte) bool {
	if key == nil || len(key) == 0 {
		return false
	}
	want := p.HMAC
	if want == "" {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(canonicalProgressBytes(p))
	got := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(got))
}

func canonicalProgressBytes(p ProgressEvent) []byte {
	cp := p
	cp.HMAC = ""
	b, _ := json.Marshal(cp)
	return b
}

// verifyResultHMAC recomputes the HMAC over the canonical result event and
// compares it to the supplied HMAC.
func verifyResultHMAC(r ResultEvent, key []byte) bool {
	if key == nil || len(key) == 0 {
		return false
	}
	want := r.HMAC
	if want == "" {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(canonicalResultBytes(r))
	got := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(got))
}

func canonicalResultBytes(r ResultEvent) []byte {
	cr := r
	cr.HMAC = ""
	b, _ := json.Marshal(cr)
	return b
}
