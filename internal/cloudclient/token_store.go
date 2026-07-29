package cloudclient

import (
	"context"
	"errors"
)

// TokenStore is the interface for persisting cloud API tokens.
type TokenStore interface {
	Set(ctx context.Context, token string) error
	Get(ctx context.Context) (string, error)
	Delete(ctx context.Context) error
}

var (
	// ErrTokenNotFound is returned when no token exists in the store.
	ErrTokenNotFound = errors.New("cloud api token not found")
	// ErrTokenStoreUnavailable is returned when the token store is not available
	// (e.g., keychain on non-macOS or when keychain is locked).
	ErrTokenStoreUnavailable = errors.New("token store unavailable")
)
