package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/spf13/cobra"
)

// errOlderCloud is the one line printed when the cloud answers that it does
// not accept bundles. It is not wrapped, so the process prints it once.
var errOlderCloud = errors.New("the cloud is older than the CLI")

func lockMentionsBundleKeys(lock *pack.AgentLock) bool {
	if lock == nil {
		return false
	}
	return lock.BundleDigest != "" || lock.BundleSize != 0 || lock.RuntimeID != ""
}

func lockCarriesBundleKeys(lock *pack.AgentLock) bool {
	return lock != nil && lock.BundleDigest != "" && lock.BundleSize > 0 && lock.RuntimeID != ""
}

func cloudDoesNotAcceptBundles(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not accept bundles") || strings.Contains(msg, "bundles_not_accepted")
}

func imageKind(img cloudclient.ImageRecord) string {
	if img.Kind != "" {
		return img.Kind
	}
	if img.BundleDigest != "" {
		return "bundle"
	}
	return "image"
}

func pushCloudBundle(cmd *cobra.Command, lockPath string, lock *pack.AgentLock, platformFlag string, skipRegistry bool, registryRef string) error {
	if !lockCarriesBundleKeys(lock) {
		return fmt.Errorf("cloud push: run bundle keys must be set together")
	}
	if !validBundleDigest(lock.BundleDigest) {
		return fmt.Errorf("cloud push: bundle_digest must be 64 lowercase hex characters")
	}
	if skipRegistry || registryRef != "" {
		return fmt.Errorf("cloud push: a bundle lock is uploaded as a bundle; image registry flags do not apply")
	}

	resolvedPlatform := platformFlag
	if resolvedPlatform == "" {
		if lock.Platform != "" {
			resolvedPlatform = lock.Platform
		} else {
			resolvedPlatform = "linux/amd64"
		}
	}
	if resolvedPlatform != "linux/amd64" {
		return fmt.Errorf("cloud push: platform must be linux/amd64 for cloud, got %q", resolvedPlatform)
	}

	bundlePath := filepath.Join(filepath.Dir(lockPath), pack.RunBundleName)
	if err := verifyRunBundleFile(bundlePath, lock); err != nil {
		return err
	}

	token, err := resolveToken(cmd)
	if err != nil {
		if strings.Contains(err.Error(), "not logged in") {
			return printNotLoggedIn(cmd)
		}
		return fmt.Errorf("cloud push: %w", err)
	}

	client := cloudclient.NewCloudClient(resolveAPIURL())
	images, listErr := client.ListImages(cmd.Context(), token)
	if listErr != nil {
		if strings.Contains(listErr.Error(), "not authenticated") {
			return printNotLoggedIn(cmd)
		}
		if cloudDoesNotAcceptBundles(listErr) {
			return errOlderCloud
		}
	} else if existing := admittedBundle(images, lock.BundleDigest); existing != nil {
		return printAdmittedBundle(cmd, &cloudclient.AdmitImageResponse{
			ID:           existing.ID,
			TenantID:     existing.TenantID,
			ImageDigest:  existing.ImageDigest,
			Platform:     existing.Platform,
			RegistryRef:  existing.RegistryRef,
			AgentName:    existing.AgentName,
			AgentVersion: existing.AgentVersion,
			Status:       existing.Status,
			CreatedAt:    existing.CreatedAt,
			Kind:         imageKind(*existing),
			BundleDigest: existing.BundleDigest,
		})
	}

	lockJSON, err := json.Marshal(lock)
	if err != nil {
		return fmt.Errorf("cloud push: marshal lock JSON: %w", err)
	}
	var lockMap interface{}
	if err := json.Unmarshal(lockJSON, &lockMap); err != nil {
		return fmt.Errorf("cloud push: parse lock JSON: %w", err)
	}
	imageDigest := lock.ImageDigest
	if imageDigest == "" {
		imageDigest = "sha256:" + lock.BundleDigest
	}
	progress := cloudProgress{w: cmd.ErrOrStderr(), json: jsonOutput(cmd)}
	resp, err := uploadBundleFile(cmd.Context(), client, token, bundlePath, cloudclient.UploadImageStartRequest{
		ImageDigest: imageDigest,
		Platform:    resolvedPlatform,
		AgentLock:   lockMap,
		Kind:        "bundle",
	}, lock.BundleSize, progress)
	if err != nil {
		if errors.Is(err, errOlderCloud) {
			return errOlderCloud
		}
		if strings.Contains(err.Error(), "not authenticated") {
			return printNotLoggedIn(cmd)
		}
		return fmt.Errorf("cloud push: %w", err)
	}
	if resp.Kind == "" {
		resp.Kind = "bundle"
	}
	if resp.BundleDigest == "" {
		resp.BundleDigest = lock.BundleDigest
	}
	return printAdmittedBundle(cmd, resp)
}

func admittedBundle(images []cloudclient.ImageRecord, digest string) *cloudclient.ImageRecord {
	wantImage := "sha256:" + digest
	for i := range images {
		img := &images[i]
		if img.Status != "admitted" {
			continue
		}
		if img.BundleDigest == digest || (img.Kind == "bundle" && (img.ImageDigest == wantImage || img.ImageDigest == digest)) {
			return img
		}
	}
	return nil
}

func printAdmittedBundle(cmd *cobra.Command, resp *cloudclient.AdmitImageResponse) error {
	if jsonOutput(cmd) {
		return printTextOrJSON(true, resp, nil)
	}
	fmt.Printf("Image admitted: %s\n", resp.ID)
	fmt.Printf("  Digest: %s\n", resp.ImageDigest)
	fmt.Printf("  Status: %s\n", resp.Status)
	if resp.Kind != "" {
		fmt.Printf("  Kind: %s\n", resp.Kind)
	}
	if resp.RegistryRef != "" {
		fmt.Printf("  Registry: %s\n", resp.RegistryRef)
	}
	return nil
}

func validBundleDigest(digest string) bool {
	if len(digest) != sha256.Size*2 {
		return false
	}
	for _, c := range digest {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func verifyRunBundleFile(path string, lock *pack.AgentLock) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("cloud push: read bundle: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cloud push: run bundle is not a regular file")
	}
	if info.Size() != lock.BundleSize {
		return fmt.Errorf("cloud push: run bundle size mismatch: got %d want %d", info.Size(), lock.BundleSize)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cloud push: read bundle: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return fmt.Errorf("cloud push: read bundle: %w", err)
	}
	if n != lock.BundleSize {
		return fmt.Errorf("cloud push: run bundle size mismatch: got %d want %d", n, lock.BundleSize)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != lock.BundleDigest {
		return fmt.Errorf("cloud push: run bundle digest mismatch: got %s want %s", got, lock.BundleDigest)
	}
	return nil
}

func uploadBundleFile(ctx context.Context, client *cloudclient.CloudClient, token, path string, startReq cloudclient.UploadImageStartRequest, size int64, progress cloudProgress) (*cloudclient.AdmitImageResponse, error) {
	startResp, err := client.UploadImageStart(ctx, token, startReq)
	if err != nil {
		if cloudDoesNotAcceptBundles(err) {
			return nil, errOlderCloud
		}
		return nil, err
	}
	if startResp.UploadID == "" {
		return nil, fmt.Errorf("upload-start returned an empty upload_id")
	}
	if startResp.ChunkSizeBytes <= 0 {
		return nil, fmt.Errorf("upload-start returned an invalid chunk_size_bytes: %d", startResp.ChunkSizeBytes)
	}

	if err := uploadBundleChunks(ctx, client, token, path, startResp.UploadID, startResp.ChunkSizeBytes, size, progress); err != nil {
		if errors.Is(err, errOlderCloud) || cloudDoesNotAcceptBundles(err) {
			return nil, errOlderCloud
		}
		return nil, err
	}
	progress.line("Admitting bundle…")
	resp, err := client.UploadImageComplete(ctx, token, startResp.UploadID)
	if err != nil {
		if cloudDoesNotAcceptBundles(err) {
			return nil, errOlderCloud
		}
		return nil, err
	}
	return resp, nil
}

func uploadBundleChunks(ctx context.Context, client *cloudclient.CloudClient, token, path, uploadID string, chunkSize int, size int64, progress cloudProgress) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read bundle: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()

	progress.line("Uploading bundle…")
	reader := bufio.NewReaderSize(f, chunkSize)
	chunk := make([]byte, chunkSize)
	var total int64
	for index := 1; ; index++ {
		n, readErr := io.ReadFull(reader, chunk)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return fmt.Errorf("read bundle: %w", readErr)
		}
		if n > 0 {
			if err := uploadCloudChunkWithRetry(ctx, client, token, uploadID, index, chunk[:n]); err != nil {
				if cloudDoesNotAcceptBundles(err) {
					return errOlderCloud
				}
				return err
			}
			total += int64(n)
			progress.chunk(index)
		}
		if readErr != nil {
			break
		}
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("read bundle: %w", err)
	}
	closed = true
	if total != size {
		return fmt.Errorf("run bundle size mismatch: got %d want %d", total, size)
	}
	return nil
}
