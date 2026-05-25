package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/redis-tool/redis-tool/internal/model"
	"github.com/redis-tool/redis-tool/internal/render"
	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/redis-tool/redis-tool/internal/upgrade"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Print cluster topology + per-node version, role, slots, memory.",
	RunE: runWithPrereq("status", func(ctx context.Context, r *runner.Runner) error {
		return gatherAndPrint(ctx, r)
	}),
}

func init() { rootCmd.AddCommand(statusCmd) }

// gatherAndPrint runs gather_status.yml then either prints the table or emits
// the raw status.json (when --json is set). Used by `provision`, `status`,
// and the post-upgrade summary too.
func gatherAndPrint(ctx context.Context, r *runner.Runner) error {
	if err := r.CleanRunArtifacts("status.json"); err != nil {
		return err
	}
	if _, err := r.Run(ctx, "gather_status.yml", nil, ""); err != nil {
		return err
	}

	if cfg.DryRun {
		fmt.Println("[dry-run] would parse .run/status.json and render the topology table")
		return nil
	}

	cs, err := model.Load(upgrade.StatusJSONPath(cfg.RunDir))
	if err != nil {
		return fmt.Errorf("read status.json: %w", err)
	}

	if cfg.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cs)
	}

	render.Status(os.Stdout, cs, cfg.NoColor)

	return nil
}
