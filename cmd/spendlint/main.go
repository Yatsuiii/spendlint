package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"io"

	"github.com/Yatsuiii/spendlint/internal/agent"
	diffpkg "github.com/Yatsuiii/spendlint/internal/diff"
	"github.com/Yatsuiii/spendlint/internal/gitlab"
	"github.com/Yatsuiii/spendlint/internal/ledger"
	"github.com/Yatsuiii/spendlint/internal/project"
	"github.com/Yatsuiii/spendlint/internal/web"
	"github.com/spf13/cobra"
)

const defaultDB = "spendlint.db"

func dbPath(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("SPENDLINT_DB"); env != "" {
		return env
	}
	return defaultDB
}

func main() {
	root := &cobra.Command{
		Use:   "spendlint",
		Short: "A linter for your LLM bill. Reviews GitLab merge requests for cost impact before they ship.",
	}
	root.AddCommand(cmdCheckMCP(), cmdStats(), cmdReview(), cmdReviewMR(), cmdServe())
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


func cmdStats() *cobra.Command {
	var db string
	c := &cobra.Command{
		Use:   "stats",
		Short: "Show per-label cost from the ledger (Phase 1 gate)",
		RunE: func(cmd *cobra.Command, args []string) error {
			led, err := ledger.Open(dbPath(db))
			if err != nil {
				return err
			}
			defer led.Close()
			rows, err := led.Stats()
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				fmt.Println("No calls recorded yet. Route LLM traffic through spendlint or use `spendlint ingest` to import historical data.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "LABEL\tMODEL\tCALLS/DAY\tAVG IN\tAVG OUT\t$/DAY\tTOTAL $")
			var totalDay, total float64
			for _, s := range rows {
				fmt.Fprintf(w, "%s\t%s\t%.1f\t%.0f\t%.0f\t$%.2f\t$%.2f\n",
					s.Label, s.DominantModel, s.CallsPerDay, s.AvgInTokens, s.AvgOutTokens,
					s.CostPerDayUSD, s.TotalCostUSD)
				totalDay += s.CostPerDayUSD
				total += s.TotalCostUSD
			}
			fmt.Fprintf(w, "\t\t\t\t\t$%.2f\t$%.2f\n", totalDay, total)
			return w.Flush()
		},
	}
	c.Flags().StringVar(&db, "db", "", "ledger path (default spendlint.db, or $SPENDLINT_DB)")
	return c
}

func cmdReview() *cobra.Command {
	var db, diffFile string
	c := &cobra.Command{
		Use:   "review",
		Short: "Project cost impact of a unified diff (Phase 2 gate)",
		Long:  "Reads a unified diff from --diff file (or stdin) and prints a cost projection against the ledger.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var raw []byte
			var err error
			if diffFile == "" || diffFile == "-" {
				raw, err = io.ReadAll(os.Stdin)
			} else {
				raw, err = os.ReadFile(diffFile)
			}
			if err != nil {
				return fmt.Errorf("read diff: %w", err)
			}
			hunks, err := diffpkg.Parse(string(raw))
			if err != nil {
				return fmt.Errorf("parse diff: %w", err)
			}
			changes := diffpkg.ClassifyAll(hunks)
			if len(changes) == 0 {
				fmt.Println("No cost-relevant changes detected.")
				return nil
			}
			led, err := ledger.Open(dbPath(db))
			if err != nil {
				return err
			}
			defer led.Close()
			proj := project.New(led)
			results, err := proj.Project(changes)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "TYPE\tLABEL\tOLD\tNEW\tBASELINE/DAY\tPROJECTED/DAY\tDELTA/DAY\tCONF")
			for _, r := range results {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t$%.4f\t$%.4f\t%+.4f\t%s\n",
					r.ChangeType, r.Label, r.OldValue, r.NewValue,
					r.BaselineDayUSD, r.ProjectedDayUSD, r.DeltaDayUSD, r.Confidence)
			}
			w.Flush()
			fmt.Println()
			total := project.TotalDelta(results)
			fmt.Printf("Total delta: %+.4f $/day\n", total)
			fmt.Println("Verdict:", project.Verdict(total))
			for _, r := range results {
				if r.Assumption != "" {
					fmt.Printf("  [%s] %s\n", r.ChangeType, r.Assumption)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&db, "db", "", "ledger path (default spendlint.db, or $SPENDLINT_DB)")
	c.Flags().StringVar(&diffFile, "diff", "-", "path to unified diff file (- for stdin)")
	return c
}

func cmdReviewMR() *cobra.Command {
	var db, project, gcpProject, gcpLocation string
	var mrIID int
	c := &cobra.Command{
		Use:   "review-mr",
		Short: "Run the Gemini agent to review a GitLab MR for LLM cost impact (Phase 3)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if project == "" {
				return fmt.Errorf("--project is required (e.g. group/repo)")
			}
			if mrIID == 0 {
				return fmt.Errorf("--mr is required")
			}
			if gcpProject == "" {
				gcpProject = os.Getenv("GOOGLE_CLOUD_PROJECT")
			}
			if gcpProject == "" {
				return fmt.Errorf("--gcp-project or $GOOGLE_CLOUD_PROJECT is required")
			}
			if gcpLocation == "" {
				gcpLocation = "us-central1"
			}
			led, err := ledger.Open(dbPath(db))
			if err != nil {
				return err
			}
			defer led.Close()
			ctx := cmd.Context()
			a, err := agent.New(ctx, gcpProject, gcpLocation, "", led)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Reviewing MR !%d in %s...\n", mrIID, project)
			comment, err := a.ReviewMR(ctx, project, mrIID)
			if err != nil {
				return err
			}
			fmt.Println(comment)
			return nil
		},
	}
	c.Flags().StringVar(&db, "db", "", "ledger path (default spendlint.db, or $SPENDLINT_DB)")
	c.Flags().StringVar(&project, "project", "", "GitLab project path (e.g. group/repo)")
	c.Flags().IntVar(&mrIID, "mr", 0, "Merge request IID")
	c.Flags().StringVar(&gcpProject, "gcp-project", "", "GCP project ID (or $GOOGLE_CLOUD_PROJECT)")
	c.Flags().StringVar(&gcpLocation, "gcp-location", "us-central1", "Vertex AI region")
	return c
}

func cmdServe() *cobra.Command {
	var db, gcpProject, gcpLocation, mcpURL, webhookSecret, port string
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run the webhook receiver and dashboard (Phase 4)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if port == "" {
				port = os.Getenv("PORT")
			}
			if port == "" {
				port = "8080"
			}
			if gcpProject == "" {
				gcpProject = os.Getenv("GOOGLE_CLOUD_PROJECT")
			}
			if gcpProject == "" {
				return fmt.Errorf("--gcp-project or $GOOGLE_CLOUD_PROJECT is required")
			}
			if gcpLocation == "" {
				gcpLocation = "us-central1"
			}

			led, err := ledger.Open(dbPath(db))
			if err != nil {
				return err
			}
			defer led.Close()

			srv := web.New(web.Config{
				Port:          port,
				GCPProject:    gcpProject,
				GCPLocation:   gcpLocation,
				MCPUrl:        mcpURL,
				WebhookSecret: webhookSecret,
				Ledger:        led,
			})
			addr := ":" + port
			fmt.Fprintf(os.Stderr, "spendlint listening on %s\n", addr)
			return srv.ListenAndServe(addr)
		},
	}
	c.Flags().StringVar(&db, "db", "", "ledger path (default spendlint.db, or $SPENDLINT_DB)")
	c.Flags().StringVar(&gcpProject, "gcp-project", "", "GCP project ID (or $GOOGLE_CLOUD_PROJECT)")
	c.Flags().StringVar(&gcpLocation, "gcp-location", "us-central1", "Vertex AI region")
	c.Flags().StringVar(&mcpURL, "mcp-url", "", "GitLab MCP URL (default: $GITLAB_MCP_URL or GitLab.com)")
	c.Flags().StringVar(&webhookSecret, "webhook-secret", "", "GitLab webhook secret token (X-Gitlab-Token)")
	c.Flags().StringVar(&port, "port", "", "HTTP port (default 8080, or $PORT)")
	return c
}
