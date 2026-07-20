// Package httpjson provides shared JSON HTTP response helpers.
package httpjson

import (
	"encoding/json"
	"net/http"
)

// Write sets Content-Type to application/json, writes the status code, and
// encodes value as JSON. Encode errors are ignored (best-effort to client).
func Write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value) // best-effort encode to client
}
