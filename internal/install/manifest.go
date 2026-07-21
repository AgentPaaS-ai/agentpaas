package install

import "time"

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

	// B23-T04 materialization fields (install-manifest.json under state/agents/<name>@<pub8>/).
	InstallMode         string            `json:"install_mode,omitempty"`
	LocalImageDigest    string            `json:"local_image_digest,omitempty"`
	DepsUnlockedRebuild bool              `json:"deps_unlocked_rebuild,omitempty"`
	ParentBundleRef     *ParentBundleRef  `json:"parent_bundle_ref,omitempty"`
	InstalledAt         time.Time         `json:"installed_at,omitempty"`
	Alias               string            `json:"alias,omitempty"`

	// B31-T01 promotion fields.
	// Promoted defaults to false; existing manifests without this field
	// unmarshal with zero values (migration is automatic).
	Promoted   bool       `json:"promoted,omitempty"`
	PromotedAt *time.Time `json:"promoted_at,omitempty"`
	PromotedBy string     `json:"promoted_by,omitempty"`
}

// ParentBundleRef records the originating bundle for fork lineage (B24).
type ParentBundleRef struct {
	Digest string `json:"digest"`
	Path   string `json:"path"`
}