package oauth

import (
	"errors"
	"fmt"
	"time"
)

// EnsureAccess returns a non-expired access token or AuthorizationRequiredError.
// It reads from the local OAuth Store only; it never falls through to a
// static SecretStore. If the stored token is expired, it is deleted locally
// and AuthorizationRequiredError is returned.
func EnsureAccess(store Store, key TokenKey, refreshSkew time.Duration) (string, error) {
	if store == nil {
		return "", fmt.Errorf("oauth store is nil")
	}
	rec, err := store.Get(key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", &AuthorizationRequiredError{CredentialID: key.CredentialID}
		}
		return "", err
	}
	if refreshSkew <= 0 {
		refreshSkew = time.Minute
	}
	if time.Until(rec.AccessExpiresAt) > refreshSkew {
		return rec.AccessToken, nil
	}
	// Token expired or about to expire: delete locally, require re-authorization.
	_ = store.Delete(key)
	return "", &AuthorizationRequiredError{CredentialID: key.CredentialID}
}
