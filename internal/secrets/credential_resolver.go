package secrets

// CredentialResolver maps declared policy credential IDs to local secret store names.
// When Resolve returns ok=false, the credential is deferred/unmapped at install time.
type CredentialResolver interface {
	Resolve(declaredID string) (localName string, ok bool)
}

// MapCredentialResolver resolves using an install manifest credential_map.
type MapCredentialResolver struct {
	Map map[string]string
}

// Resolve implements CredentialResolver.
func (m MapCredentialResolver) Resolve(declaredID string) (string, bool) {
	if m.Map == nil {
		return "", false
	}
	local, ok := m.Map[declaredID]
	if !ok || local == "" {
		return "", false
	}
	return local, true
}