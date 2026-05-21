package cli

import (
	"os"

	"github.com/reproforge/reproforge/internal/server"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var (
		addr    string
		storage string
		token   string
		maxMB   int64
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start a self-hosted capsule sharing server (FR-031)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("REPROFORGE_SERVER_TOKEN")
			}
			s, err := server.New(server.Config{
				Addr: addr, Storage: storage, Token: token,
				MaxBody: maxMB * 1024 * 1024, Logger: rootLogger,
			})
			if err != nil {
				return err
			}
			return s.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	cmd.Flags().StringVar(&storage, "storage", "./reproforge-server", "storage directory")
	cmd.Flags().StringVar(&token, "token", "", "bearer token (or REPROFORGE_SERVER_TOKEN env)")
	cmd.Flags().Int64Var(&maxMB, "max-mb", 50, "max upload size in MB")
	return cmd
}
