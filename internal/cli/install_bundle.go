package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentpaas "github.com/AgentPaaS-ai/agentpaas"
	"github.com/AgentPaaS-ai/agentpaas/internal/binresolve"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	installpkg "github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
	"github.com/spf13/cobra"
)

type displayError interface{ DisplayMessage() string }

func newInstallBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <file.agentpaas>",
		Short: "Verify and install a signed AgentPaaS bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			b, err := bundle.Open(path)
			if err != nil {
				return err
			}
			defer b.Close()
			vr, err := bundle.Verify(b)
			if err != nil {
				return err
			}
			report, err := bundle.Inspect(path, b, vr)
			if err != nil {
				return err
			}
			if !report.Verified {
				return errors.New("bundle integrity verification failed")
			}
			if b.Lock == nil || b.Lock.Publisher == nil {
				return errors.New("bundle has no publisher identity")
			}
			yes := mustBool(cmd.Flags().GetBool("yes"))

			homeDir, err := getAgentpaasHome(cmd)
			if err != nil {
				return err
			}
			trustStore, err := trust.Load(filepath.Join(homeDir, "trust", "publishers.json"))
			if err != nil {
				return err
			}
			isTTY := isTerminal(os.Stdin) && !yes
			trustResult, err := installpkg.ResolveTrust(installpkg.TrustResolveOpts{
				Store: trustStore, PublisherName: b.Lock.Publisher.Name,
				PublisherFingerprint:  b.Lock.Publisher.Fingerprint,
				PublisherPublicKeyPEM: b.Lock.Publisher.PublicKeyPEM, IsTTY: isTTY,
				ConfirmedFingerprint: mustString(cmd.Flags().GetString("confirm-fingerprint")),
				Prompt:               promptReader(),
			})
			if err != nil {
				return formatInstallError(err)
			}
			for _, line := range trustResult.DisplayLines {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}

			state := &installpkg.FileInstallState{StateRoot: filepath.Join(homeDir, "state")}
			accept := mustString(cmd.Flags().GetString("accept-policy"))
			allowDowngrade := mustBool(cmd.Flags().GetBool("allow-downgrade"))
			consent, err := installpkg.ResolvePolicyConsent(installpkg.PolicyConsentOpts{
				Report: report, PolicyDigest: b.Lock.PolicyDigest, PolicyYAML: b.PolicyYAML,
				PublisherFingerprint: b.Lock.Publisher.Fingerprint, PublisherName: b.Lock.Publisher.Name,
				AgentName: b.Lock.AgentName, AgentVersion: b.Lock.AgentVersion, State: state,
				IsTTY: isTTY, AcceptPolicyDigest: accept, AllowDowngrade: allowDowngrade,
				Prompt: promptReader(),
			})
			if err != nil {
				return formatInstallError(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), consent.CardText)
			digest, err := bundle.FileBundleDigest(path)
			if err != nil {
				return err
			}
			preferImage := mustBool(cmd.Flags().GetBool("prefer-image"))
			allowUnlocked := mustBool(cmd.Flags().GetBool("allow-unlocked-deps"))
			builder, cleanup, err := installBuilder()
			if err != nil {
				return err
			}
			if cleanup != nil {
				defer cleanup()
			}
			result, err := installpkg.MaterializeInstall(context.Background(), installpkg.MaterializeOpts{
				StateRoot: filepath.Join(homeDir, "state"), Bundle: b, BundlePath: path, BundleDigest: digest,
				Manifest: consent.Manifest, PreferImage: preferImage, AllowUnlockedDeps: allowUnlocked,
				IsTTY: isTTY, PromptUnlocked: promptReader(), Builder: builder,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed: %s\n", result.AgentRef)
			return nil
		},
	}
	cmd.Flags().String("confirm-fingerprint", "", "Confirm an unknown publisher fingerprint")
	cmd.Flags().String("accept-policy", "", "Accept the bundle policy digest")
	cmd.Flags().Bool("yes", false, "Skip confirmation prompts")
	cmd.Flags().Bool("allow-downgrade", false, "Allow version downgrade")
	cmd.Flags().Bool("allow-unlocked-deps", false, "Allow install without uv.lock")
	cmd.Flags().Bool("prefer-image", false, "Use a prebuilt image included in the bundle")
	return cmd
}

// installBuilder resolves the harness binary and Python SDK directory,
// mirroring the daemon pack path (control_handlers.go). If the SDK is not on
// disk (brew-only install), it falls back to the SDK embedded in the binary.
// The returned cleanup function (if non-nil) removes the extracted temp SDK
// directory and MUST be called after the build completes.
func installBuilder() (*installpkg.PackImageBuilder, func(), error) {
	harnessPath := binresolve.HarnessBinary()
	sdkDir := binresolve.SDKDir(harnessPath)

	// If the SDK is not on disk (brew-only install, release tarball without
	// python/), fall back to the SDK embedded in the binary.
	if sdkDir == "" {
		embeddedSDKDir, cleanup, err := agentpaas.ExtractEmbeddedSDKToTemp()
		if err != nil {
			return nil, nil, fmt.Errorf("SDK not found on disk and embedded SDK extraction failed: %w", err)
		}
		sdkDir = embeddedSDKDir
		return &installpkg.PackImageBuilder{
			HarnessPath: harnessPath,
			SDKDir:      sdkDir,
		}, cleanup, nil
	}

	return &installpkg.PackImageBuilder{
		HarnessPath: harnessPath,
		SDKDir:      sdkDir,
	}, nil, nil
}

func promptReader() func(string) (string, error) {
	return func(prompt string) (string, error) {
		fmt.Fprint(os.Stderr, prompt)
		return bufio.NewReader(os.Stdin).ReadString('\n')
	}
}

func formatInstallError(err error) error {
	if e, ok := err.(displayError); ok {
		return errors.New(e.DisplayMessage())
	}
	return err
}

func mustString(v string, err error) string {
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
func mustBool(v bool, err error) bool { return err == nil && v }
