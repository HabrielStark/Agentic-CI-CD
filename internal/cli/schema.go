package cli

import (
	"fmt"

	"github.com/reproforge/reproforge/internal/capsule"
	"github.com/spf13/cobra"
)

func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Print the supported capsule schema versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n", capsule.SchemaV1)
			return nil
		},
	}
	return cmd
}
