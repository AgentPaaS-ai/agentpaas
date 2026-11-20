package port

import "context"

// SecretBroker applies credentials outside untrusted workload code.
type SecretBroker interface {
	Apply(context.Context, ApplyCredentialRequest) error
	Revoke(context.Context, string, string) error
	List(context.Context, string) ([]string, error)
}

// ApplyCredentialRequest identifies a credential to apply without its value.
type ApplyCredentialRequest struct {
	TenantID     string
	WorkloadID   string
	CredentialID string
	MountPath    string
	Header       string
}
