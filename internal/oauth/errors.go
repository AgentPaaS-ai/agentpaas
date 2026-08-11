package oauth

import (
	"errors"
	"fmt"
)

// AuthorizationRequiredError is returned when no delegated token is stored.
// Callers must surface ConsentURL to the operator (browser) and not retry inject.
type AuthorizationRequiredError struct {
	ConsentURL   string
	GrantID      string
	CredentialID string
}

func (e *AuthorizationRequiredError) Error() string {
	return fmt.Sprintf("authorization_required credential=%s grant=%s", e.CredentialID, e.GrantID)
}

// IsAuthorizationRequired reports whether err is or wraps *AuthorizationRequiredError.
// Returns the unwrapped error and true if matched.
func IsAuthorizationRequired(err error) (*AuthorizationRequiredError, bool) {
	var target *AuthorizationRequiredError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
