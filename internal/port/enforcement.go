package port

import "context"

// EgressEnforcer applies outbound communication policy.
type EgressEnforcer interface {
	Apply(context.Context, string, CommSnapshot) error
	Check(context.Context, string, string) Decision
	Remove(context.Context, string) error
}

// IngressEnforcer applies inbound communication policy.
type IngressEnforcer interface {
	Apply(context.Context, string, CommSnapshot) error
	Check(context.Context, string, string) Decision
	Remove(context.Context, string) error
}

// CommSnapshot is an immutable communication policy snapshot.
type CommSnapshot struct {
	Digest  string
	Rules   []CommRule
	Default CommAction
}

// CommRule matches a host and port.
type CommRule struct {
	Host         string
	Port         int
	Action       CommAction
	CredentialID string
}

// CommAction is an allow or deny communication action.
type CommAction string

const (
	CommAllow CommAction = "allow"
	CommDeny  CommAction = "deny"
)

// Decision records an auditable communication decision.
type Decision struct {
	Action    CommAction
	Reason    string
	RuleIndex int
}
