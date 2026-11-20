package export

import (
	"crypto/ecdsa"
	"errors"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
)

func loadPublisherPrivateKey(ks identity.KeyStore) (*ecdsa.PrivateKey, error) {
	if ks == nil {
		return nil, errors.New("publisher keystore is required")
	}
	return identity.LoadPublisherSigningKey(ks)
}