package cli

import (
	"fmt"
	"strings"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/spf13/cobra"
)

// newDeployCmd creates the `agent deploy` command group for exact deployment
// creation, inspect, alias CAS, and deactivation (state-only B26 surface).
func newDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Manage exact deployments and aliases (state-only)",
		Long: `Create and manage exact deployment identities and aliases.

In B26, deploy/list/inspect/alias/deactivate are state-only operations.
Durable routed invocation remains disabled until later blocks.`,
	}
	cmd.AddCommand(newDeployCreateCmd())
	cmd.AddCommand(newDeployListCmd())
	cmd.AddCommand(newDeployInspectCmd())
	cmd.AddCommand(newDeployDeactivateCmd())
	cmd.AddCommand(newDeployAliasCmd())
	return cmd
}

func newDeployCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <package-name>@<version>",
		Short: "Create an exact deployment from installed package identity",
		// Also support: agentpaas deploy <exact-installed-ref>
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]
			pkgName, pkgVersion, err := splitPackageRef(ref)
			if err != nil {
				return err
			}
			bundleDigest, _ := cmd.Flags().GetString("bundle-digest")
			policyDigest, _ := cmd.Flags().GetString("policy-digest")
			imageLock, _ := cmd.Flags().GetString("image-lock-digest")
			provenance, _ := cmd.Flags().GetString("provenance-digest")
			maxConcurrent, _ := cmd.Flags().GetInt32("max-concurrent-runs")
			alias, _ := cmd.Flags().GetString("alias")
			actor, _ := cmd.Flags().GetString("actor")

			if bundleDigest == "" {
				// Allow create with placeholder digests for state-only tests;
				// production deploy should pass real digests from install.
				bundleDigest = "sha256:placeholder-bundle"
			}
			if maxConcurrent <= 0 {
				maxConcurrent = 1
			}
			if actor == "" {
				actor = "cli"
			}

			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			ctx, cancel := contextWithTimeout(30 * time.Second)
			defer cancel()

			resp, err := client.CreateDeployment(ctx, &controlv1.CreateDeploymentRequest{
				PackageName:       pkgName,
				PackageVersion:    pkgVersion,
				BundleDigest:      bundleDigest,
				PolicyDigest:      policyDigest,
				ImageLockDigest:   imageLock,
				ProvenanceDigest:  provenance,
				MaxConcurrentRuns: maxConcurrent,
				ActorIdentity:     actor,
			})
			if err != nil {
				return fmt.Errorf("deploy create failed: %w", err)
			}
			dep := resp.GetDeployment()
			if dep == nil {
				if e := resp.GetError(); e != nil {
					return fmt.Errorf("%s: %s", e.GetCodeName(), e.GetMessage())
				}
				return fmt.Errorf("deploy create returned empty deployment")
			}

			if alias != "" {
				if _, err := client.CreateDeploymentAlias(ctx, &controlv1.CreateDeploymentAliasRequest{
					Alias:              alias,
					TargetDeploymentId: dep.GetDeploymentId(),
					ActorIdentity:      actor,
				}); err != nil {
					return fmt.Errorf("deployment created (%s) but alias set failed: %w", dep.GetDeploymentId(), err)
				}
			}

			result := struct {
				DeploymentID   string `json:"deployment_id"`
				PackageName    string `json:"package_name"`
				PackageVersion string `json:"package_version"`
				Status         string `json:"status"`
				Generation     int64  `json:"generation"`
				Alias          string `json:"alias,omitempty"`
			}{
				DeploymentID:   dep.GetDeploymentId(),
				PackageName:    dep.GetPackageName(),
				PackageVersion: dep.GetPackageVersion(),
				Status:         dep.GetStatus(),
				Generation:     dep.GetGeneration(),
				Alias:          alias,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					DeploymentID   string `json:"deployment_id"`
					PackageName    string `json:"package_name"`
					PackageVersion string `json:"package_version"`
					Status         string `json:"status"`
					Generation     int64  `json:"generation"`
					Alias          string `json:"alias,omitempty"`
				})
				out := fmt.Sprintf("Deployment created: %s (%s@%s) [%s gen=%d]",
					r.DeploymentID, r.PackageName, r.PackageVersion, r.Status, r.Generation)
				if r.Alias != "" {
					out += fmt.Sprintf("\nAlias set: %s -> %s", r.Alias, r.DeploymentID)
				}
				return out
			})
		},
	}
	cmd.Flags().String("bundle-digest", "", "Exact bundle digest")
	cmd.Flags().String("policy-digest", "", "Policy digest")
	cmd.Flags().String("image-lock-digest", "", "Image lock digest")
	cmd.Flags().String("provenance-digest", "", "Provenance digest")
	cmd.Flags().Int32("max-concurrent-runs", 1, "Max concurrent runs for this deployment")
	cmd.Flags().String("alias", "", "Optional alias to point at the new deployment")
	cmd.Flags().String("actor", "cli", "Actor identity for audit")
	return cmd
}

func newDeployListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List exact deployments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.ListDeployments(ctx, &controlv1.ListDeploymentsRequest{})
			if err != nil {
				return fmt.Errorf("list deployments failed: %w", err)
			}
			return printTextOrJSON(jsonOutput(cmd), resp, func(v interface{}) string {
				r := v.(*controlv1.ListDeploymentsResponse)
				if len(r.GetDeployments()) == 0 {
					return "No deployments."
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Deployments (%d):\n", len(r.GetDeployments()))
				for _, d := range r.GetDeployments() {
					fmt.Fprintf(&b, "  %s  %s@%s  [%s] gen=%d\n",
						d.GetDeploymentId(), d.GetPackageName(), d.GetPackageVersion(),
						d.GetStatus(), d.GetGeneration())
				}
				return b.String()
			})
		},
	}
}

func newDeployInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <deployment-id>",
		Short: "Inspect an exact deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.GetDeployment(ctx, &controlv1.GetDeploymentRequest{
				DeploymentId: args[0],
			})
			if err != nil {
				return fmt.Errorf("inspect deployment failed: %w", err)
			}
			return printTextOrJSON(jsonOutput(cmd), resp.GetDeployment(), func(v interface{}) string {
				d := v.(*controlv1.DeploymentRecord)
				return fmt.Sprintf(
					"Deployment: %s\nPackage: %s@%s\nStatus: %s\nGeneration: %d\nMax concurrent: %d\nBundle: %s\nPolicy: %s\nImage lock: %s\nCreated by: %s\n",
					d.GetDeploymentId(), d.GetPackageName(), d.GetPackageVersion(),
					d.GetStatus(), d.GetGeneration(), d.GetMaxConcurrentRuns(),
					d.GetBundleDigest(), d.GetPolicyDigest(), d.GetImageLockDigest(),
					d.GetCreatedBy(),
				)
			})
		},
	}
}

func newDeployDeactivateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deactivate <deployment-id>",
		Short: "Deactivate an exact deployment (state-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.DeactivateDeployment(ctx, &controlv1.DeactivateDeploymentRequest{
				DeploymentId:  args[0],
				ActorIdentity: "cli",
			})
			if err != nil {
				return fmt.Errorf("deactivate failed: %w", err)
			}
			dep := resp.GetDeployment()
			result := struct {
				DeploymentID string `json:"deployment_id"`
				Status       string `json:"status"`
			}{DeploymentID: dep.GetDeploymentId(), Status: dep.GetStatus()}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					DeploymentID string `json:"deployment_id"`
					Status       string `json:"status"`
				})
				return fmt.Sprintf("Deactivated: %s [%s]", r.DeploymentID, r.Status)
			})
		},
	}
}

func newDeployAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias",
		Short: "Manage deployment aliases (set / promote / rollback / list)",
	}
	cmd.AddCommand(newDeployAliasSetCmd())
	cmd.AddCommand(newDeployAliasPromoteCmd())
	cmd.AddCommand(newDeployAliasRollbackCmd())
	cmd.AddCommand(newDeployAliasListCmd())
	return cmd
}

func newDeployAliasSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <alias> <deployment-id>",
		Short: "Create an alias pointing at a deployment (fails if exists)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.CreateDeploymentAlias(ctx, &controlv1.CreateDeploymentAliasRequest{
				Alias:              args[0],
				TargetDeploymentId: args[1],
				ActorIdentity:      "cli",
			})
			if err != nil {
				return fmt.Errorf("alias set failed: %w", err)
			}
			a := resp.GetAlias()
			return printTextOrJSON(jsonOutput(cmd), a, func(v interface{}) string {
				al := v.(*controlv1.DeploymentAliasRecord)
				return fmt.Sprintf("Alias set: %s -> %s (gen=%d)", al.GetAlias(), al.GetTargetDeploymentId(), al.GetGeneration())
			})
		},
	}
	return cmd
}

func newDeployAliasPromoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote <alias> <deployment-id>",
		Short: "CAS-promote an alias to a new deployment (requires --expected-generation)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			expGen, _ := cmd.Flags().GetInt64("expected-generation")
			if !cmd.Flags().Changed("expected-generation") {
				return fmt.Errorf("--expected-generation is required for promote (alias CAS)")
			}
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.CasDeploymentAlias(ctx, &controlv1.CasDeploymentAliasRequest{
				Alias:              args[0],
				TargetDeploymentId: args[1],
				ExpectedGeneration: expGen,
				ActorIdentity:      "cli",
			})
			if err != nil {
				return fmt.Errorf("alias promote failed: %w", err)
			}
			a := resp.GetAlias()
			return printTextOrJSON(jsonOutput(cmd), a, func(v interface{}) string {
				al := v.(*controlv1.DeploymentAliasRecord)
				return fmt.Sprintf("Alias promoted: %s -> %s (gen=%d)", al.GetAlias(), al.GetTargetDeploymentId(), al.GetGeneration())
			})
		},
	}
	cmd.Flags().Int64("expected-generation", 0, "Expected current alias generation (CAS)")
	return cmd
}

func newDeployAliasRollbackCmd() *cobra.Command {
	// Rollback is CAS pointing the alias back to a prior deployment ID.
	cmd := &cobra.Command{
		Use:   "rollback <alias> <deployment-id>",
		Short: "CAS-rollback an alias to a prior deployment (requires --expected-generation)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			expGen, _ := cmd.Flags().GetInt64("expected-generation")
			if !cmd.Flags().Changed("expected-generation") {
				return fmt.Errorf("--expected-generation is required for rollback (alias CAS)")
			}
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.CasDeploymentAlias(ctx, &controlv1.CasDeploymentAliasRequest{
				Alias:              args[0],
				TargetDeploymentId: args[1],
				ExpectedGeneration: expGen,
				ActorIdentity:      "cli",
			})
			if err != nil {
				return fmt.Errorf("alias rollback failed: %w", err)
			}
			a := resp.GetAlias()
			return printTextOrJSON(jsonOutput(cmd), a, func(v interface{}) string {
				al := v.(*controlv1.DeploymentAliasRecord)
				return fmt.Sprintf("Alias rolled back: %s -> %s (gen=%d)", al.GetAlias(), al.GetTargetDeploymentId(), al.GetGeneration())
			})
		},
	}
	cmd.Flags().Int64("expected-generation", 0, "Expected current alias generation (CAS)")
	return cmd
}

func newDeployAliasListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List deployment aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := dialControl(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			ctx, cancel := contextWithTimeout(15 * time.Second)
			defer cancel()
			resp, err := client.ListDeploymentAliases(ctx, &controlv1.ListDeploymentAliasesRequest{})
			if err != nil {
				return fmt.Errorf("list aliases failed: %w", err)
			}
			return printTextOrJSON(jsonOutput(cmd), resp, func(v interface{}) string {
				r := v.(*controlv1.ListDeploymentAliasesResponse)
				if len(r.GetAliases()) == 0 {
					return "No aliases."
				}
				var b strings.Builder
				fmt.Fprintf(&b, "Aliases (%d):\n", len(r.GetAliases()))
				for _, a := range r.GetAliases() {
					fmt.Fprintf(&b, "  %s -> %s (gen=%d ver=%s)\n",
						a.GetAlias(), a.GetTargetDeploymentId(), a.GetGeneration(), a.GetTargetVersion())
				}
				return b.String()
			})
		},
	}
}

func dialControl(cmd *cobra.Command) (controlv1.ControlServiceClient, interface{ Close() error }, error) {
	sock, err := socketPath(cmd)
	if err != nil {
		return nil, nil, err
	}
	client, conn, err := ConnectToDaemon(sock)
	if err != nil {
		return nil, nil, err
	}
	return client, conn, nil
}

func splitPackageRef(ref string) (name, version string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("package ref is required (name@version)")
	}
	// Support name@version or bare name (version defaults to "0.0.0").
	at := strings.LastIndex(ref, "@")
	if at <= 0 {
		return ref, "0.0.0", nil
	}
	name = ref[:at]
	version = ref[at+1:]
	if name == "" || version == "" {
		return "", "", fmt.Errorf("invalid package ref %q (want name@version)", ref)
	}
	// Reject pure numeric-only garbage versions only if empty after trim.
	if strings.TrimSpace(version) == "" {
		return "", "", fmt.Errorf("invalid package version in %q", ref)
	}
	return name, version, nil
}
