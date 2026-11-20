package port

import (
	"context"
	"time"
)

// PackageStore resolves and verifies exact signed package digests.
type PackageStore interface {
	Resolve(context.Context, string, string) (*PackageResolution, error)
	Verify(context.Context, string) error
	List(context.Context, string) ([]PackageMetadata, error)
}

// PackageResolution is the result of package resolution.
type PackageResolution struct {
	ImageDigest    string
	Manifest       []byte
	PackageName    string
	PackageVersion string
	PolicyDigest   string
}

// PackageMetadata describes a tenant package.
type PackageMetadata struct {
	TenantID       string
	PackageName    string
	PackageVersion string
	ImageDigest    string
	CreatedAt      time.Time
}
