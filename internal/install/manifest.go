package install

// InstallManifest records receiver-side install metadata (extended by B23-T04).
type InstallManifest struct {
	PublisherFingerprint string `json:"publisher_fingerprint"`
	PublisherName        string `json:"publisher_name"`
	AgentName            string `json:"agent_name"`
	AgentVersion         string `json:"agent_version"`
	AcceptedPolicyDigest string `json:"accepted_policy_digest"`
	// CredentialMap maps declared policy credential IDs to local secret store names
	// (renames only; scope remains governed by the signed policy).
	CredentialMap map[string]string `json:"credential_map,omitempty"`
}