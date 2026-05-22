package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Yatsuiii/spendlint/internal/gitlab"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:   "spendlint",
		Short: "A linter for your LLM bill. Reviews GitLab merge requests for cost impact before they ship.",
	}
	root.AddCommand(cmdCheckMCP(), cmdSeed(), cmdServe())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func cmdCheckMCP() *cobra.Command {
	return &cobra.Command{
		Use:   "check-mcp",
		Short: "Verify connectivity to the GitLab MCP server via mcp-remote (Phase 0 gate)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Starting mcp-remote. On first run a browser opens for GitLab OAuth consent; approve it.")
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			version, err := gitlab.CheckMCP(ctx)
			if err != nil {
				return err
			}
			fmt.Println("GitLab MCP server reachable via mcp-remote.")
			fmt.Println("get_mcp_server_version ->", version)
			return nil
		},
	}
}

func cmdSeed() *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Seed a deterministic demo ledger (Phase 1)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (Phase 1)")
		},
	}
}

func cmdServe() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the webhook receiver and dashboard (Phase 4)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet (Phase 4)")
		},
	}
}
