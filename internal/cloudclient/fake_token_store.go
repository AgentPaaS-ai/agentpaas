package cloudclient

import (
	"context"
	"fmt"
	"sync"
)

// FakeTokenStore is an in-memory TokenStore for testing.
type FakeTokenStore struct {
	mu    sync.Mutex
	token string
}

// NewFakeTokenStore creates a new FakeTokenStore.
func NewFakeTokenStore() *FakeTokenStore {
	return &FakeTokenStore{}
}

// Set stores a token in the fake store.
func (f *FakeTokenStore) Set(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token = token
	return nil
}

// Get retrieves a token from the fake store.
func (f *FakeTokenStore) Get(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token == "" {
		return "", fmt.Errorf("%w: no token stored", ErrTokenNotFound)
	}
	return f.token, nil
}

// Delete removes the token from the fake store.
func (f *FakeTokenStore) Delete(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.token = ""
	return nil
}

var _ TokenStore = (*FakeTokenStore)(nil)
