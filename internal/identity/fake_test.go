package identity

import (
	"testing"
)

// TestFakeKeyStore runs the full contract suite against the in-memory fake.
func TestFakeKeyStore(t *testing.T) {
	ContractTests(t, func() KeyStore {
		return NewFakeKeyStore()
	})
}