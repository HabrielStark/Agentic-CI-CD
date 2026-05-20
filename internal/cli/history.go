package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reproforge/reproforge/internal/store"
	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show recurrence and aggregate stats from the local SQLite store",
	}
	cmd.AddCommand(newHistoryShowCmd())
	cmd.AddCommand(newHistoryStatsCmd())
	return cmd
}

func newHistoryShowCmd() *cobra.Command {
	var (
		fp    string
		limit int
		jsonO string
	)
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show recent runs grouped by failure fingerprint",
		RunE: func(cmd *cobra.Command, args []string) error {
			if fp == "" {
				return fmt.Errorf("--fingerprint is required")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			runs, err := s.HistoryByFingerprint(context.Background(), fp, limit)
			if err != nil {
				return err
			}
			if jsonO != "" {
				b, _ := json.MarshalIndent(runs, "", "  ")
				return os.WriteFile(jsonO, b, 0o644)
			}
			for _, r := range runs {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\trun=%d\trepo=%s\tjob=%s\tcat=%s\tconfidence=%.2f\n",
					r.CreatedAt.Format("2026-01-02T15:04:05Z"), r.RunID, r.Repo, r.Job, r.Category, r.Confidence)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fp, "fingerprint", "", "fingerprint to look up (sha256:...)")
	cmd.Flags().IntVar(&limit, "limit", 50, "max rows")
	cmd.Flags().StringVar(&jsonO, "json", "", "write history as JSON")
	return cmd
}

func newHistoryStatsCmd() *cobra.Command {
	var repo string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Aggregate counts of diagnosis categories for a repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			if repo == "" {
				return fmt.Errorf("--repo is required")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			st, err := s.Aggregate(context.Background(), repo)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "repo=%s totalRuns=%d\n", repo, st.TotalRuns)
			for k, v := range st.Categories {
				fmt.Fprintf(cmd.OutOrStdout(), "  %-30s %d\n", k, v)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "owner/repo")
	return cmd
}

func openStore() (*store.Store, error) {
	dir := filepath.Join(".reproforge")
	_ = os.MkdirAll(dir, 0o755)
	return store.Open(filepath.Join(dir, "runs.db"))
}
