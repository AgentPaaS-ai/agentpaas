package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newDoctorCmd creates the `agent doctor` command (v0 stub).
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run system diagnostics (v0 stub — not yet implemented)",
		Long: `Run system diagnostics to verify agentpaas is configured correctly.

This is a v0 stub. Real diagnostics checks will be added in a
future release (B2-T05).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Doctor checks not yet implemented")
			return nil
		},
	}
}