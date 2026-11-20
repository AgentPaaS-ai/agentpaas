package daemon

import (
	"context"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ListRegistry returns all installed registry entries with joined deployment
// data from the daemon's routedrun store.
func (s *controlServer) ListRegistry(ctx context.Context, req *controlv1.ListRegistryRequest) (*controlv1.ListRegistryResponse, error) {
	entries, err := registry.ListEntries(s.homePaths.State, s.deploymentStore)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list registry: %v", err)
	}

	protoEntries := make([]*controlv1.RegistryEntry, 0, len(entries))
	for _, e := range entries {
		protoEntries = append(protoEntries, registryEntryToProto(e))
	}
	return &controlv1.ListRegistryResponse{Entries: protoEntries}, nil
}

// ShowRegistry returns a single registry entry by ref (name@pub8 or alias),
// with full capability metadata from the lockfile and deployment join from
// the daemon's routedrun store.
func (s *controlServer) ShowRegistry(ctx context.Context, req *controlv1.ShowRegistryRequest) (*controlv1.ShowRegistryResponse, error) {
	entry, err := registry.ShowEntry(s.homePaths.State, req.GetRef(), s.deploymentStore)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "show registry: %v", err)
	}
	return &controlv1.ShowRegistryResponse{Entry: registryEntryToProto(*entry)}, nil
}

// registryEntryToProto converts a registry.RegistryEntry to its protobuf form.
func registryEntryToProto(e registry.RegistryEntry) *controlv1.RegistryEntry {
	out := &controlv1.RegistryEntry{
		Ref:                  e.Ref,
		Name:                 e.Name,
		Pub8:                 e.Pub8,
		Version:              e.Version,
		PublisherName:        e.PublisherName,
		PublisherFingerprint: e.PublisherFingerprint,
		PackageDigest:        e.PackageDigest,
		PolicyDigest:         e.PolicyDigest,
		InstallMode:          e.InstallMode,
		LocalImageDigest:     e.LocalImageDigest,
		InstalledAt:          timestamppb.New(e.InstalledAt),
		CredentialIds:        e.CredentialIDs,
		Alias:                e.Alias,
		DeploymentStatus:     e.DeploymentStatus,
		Generation:           e.Generation,
		BundleDigest:         e.BundleDigest,
		AliasesDeployment:    e.Aliases,
		Promoted:             e.Promoted,
		PromotedBy:           e.PromotedBy,
	}

	if e.DeploymentID != nil {
		out.DeploymentId = e.DeploymentID
	}

	if e.PromotedAt != nil {
		out.PromotedAt = timestamppb.New(*e.PromotedAt)
	}

	if len(e.Capabilities) > 0 {
		caps := make([]*controlv1.RegistryCapability, 0, len(e.Capabilities))
		for _, c := range e.Capabilities {
			caps = append(caps, &controlv1.RegistryCapability{
				Id:          c.ID,
				Description: c.Description,
			})
		}
		out.Capabilities = caps
	}

	return out
}
