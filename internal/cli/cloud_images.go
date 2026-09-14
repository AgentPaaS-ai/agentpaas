package cli

import (
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

func newCloudImagesDeleteCmd() *cobra.Command {
	var yes bool
	var confirmID string
	cmd := &cobra.Command{
		Use:   "delete <img_|sha256:digest>",
		Short: "Delete an unused admitted cloud image",
		Long: `Delete an admitted cloud image by id or digest.

This calls DELETE /v1/images/:idOrDigest. It does not undeploy anything.
If the image is still referenced, the API returns 409 and this command
prints 'agentpaas cloud undeploy <dep_>' for each blocking deployment.

Requires --yes. JSON mode still requires --yes.
When stdin is not a TTY, also requires --confirm-id equal to the image id or digest.
When stdin is a TTY, type the id or digest at the prompt (even with --yes).
Never prints secrets.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const verb = "cloud images delete"
			if !yes {
				return fmt.Errorf("%s: refusing without --yes (this deletes an admitted image)", verb)
			}
			idOrDigest := args[0]
			if strings.ContainsAny(idOrDigest, "/\\\n\r") {
				return fmt.Errorf("%s: invalid image id %q: must not contain '/', '\\', newline, or carriage return", verb, idOrDigest)
			}
			if err := confirmCloudDestructive(cmd, verb, "Type the image id or digest to confirm delete: ", idOrDigest, confirmID); err != nil {
				return err
			}

			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("%s: %w", verb, err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			if err := client.DeleteImage(cmd.Context(), token, idOrDigest); err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return wrapDeleteConflict(verb, err, undeployNextCommands(err))
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, map[string]string{"id": idOrDigest, "status": "deleted"}, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted image: %s\n", idOrDigest)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm delete (required; this deletes an admitted image)")
	cmd.Flags().StringVar(&confirmID, "confirm-id", "", "Exact image id or digest (required when stdin is not a TTY)")
	return cmd
}
