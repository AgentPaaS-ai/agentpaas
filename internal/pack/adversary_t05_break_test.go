package pack

import (
	"errors"
	"testing"
)

func TestAdversaryBreak_500SubstringInUnauthorized(t *testing.T) {
	err := errors.New("sign image: unauthorized: invalid key")
	if isRetryableSignError(err) {
		t.Fatal("auth error should not retry")
	}
	// Break vector: any error message containing "500" as substring
	err5001 := errors.New("sign image: unauthorized: registry returned code 5001")
	if isRetryableSignError(err5001) {
		t.Fatal("BREAK: '500' pattern matches 5001 in auth error — false retry")
	}
	errPolicy := errors.New("sign image: policy rejected HTTP 500 from sidecar")
	if isRetryableSignError(errPolicy) {
		t.Log("MEDIUM: non-transient policy error classified retryable via '500' substring")
	}
}