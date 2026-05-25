package cmd

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/redis-tool/redis-tool/internal/model"
	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/redis-tool/redis-tool/internal/upgrade"
	"github.com/spf13/cobra"
)

var (
	seedKeys int
)

var dataCmd = &cobra.Command{
	Use:   "data",
	Short: "Seed and verify deterministic keys to prove data integrity.",
}

var dataSeedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Insert N deterministic key/value pairs (default 1000).",
	RunE: runWithPrereq("data-seed", func(ctx context.Context, r *runner.Runner) error {
		if err := r.CleanRunArtifacts("seed.json"); err != nil {
			return err
		}

		if _, err := r.Run(ctx, "data_seed.yml", map[string]any{
			"keys_count": seedKeys,
		}, ""); err != nil {
			return err
		}

		if cfg.DryRun {
			fmt.Printf("[dry-run] would parse .run/seed.json and report inserted/%d\n", seedKeys)
			return nil
		}

		s, err := model.LoadSeed(upgrade.SeedJSONPath(cfg.RunDir))
		if err != nil {
			return err
		}

		fmt.Printf("Inserted: %d / %d  (failed: %d)\n", s.Result.Inserted, s.Result.Total, s.Result.Failed)

		if s.DistributionRaw != "" {
			fmt.Println("\nPer-node DBSIZE:")
			fmt.Println(s.DistributionRaw)
		}

		return nil
	}),
}

var dataVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Recompute SHA256 for every seeded key and compare to GET.",
	RunE: runWithPrereq("data-verify", func(ctx context.Context, r *runner.Runner) error {
		return verify(ctx, r, seedKeys)
	}),
}

// verify is reused by `upgrade` (pre-flight baseline + post-upgrade check) and
// by `verify --full`. Returns nil on PASS, an error on FAIL.
func verify(ctx context.Context, r *runner.Runner, count int) error {
	if err := r.CleanRunArtifacts("verify.json"); err != nil {
		return err
	}

	if _, err := r.Run(ctx, "data_verify.yml", map[string]any{
		"keys_count": count,
	}, ""); err != nil {
		return err
	}

	if cfg.DryRun {
		fmt.Printf("[dry-run] would parse .run/verify.json and report PASS/FAIL of %d keys\n", count)
		return nil
	}

	v, err := model.LoadVerify(upgrade.VerifyJSONPath(cfg.RunDir))
	if err != nil {
		return err
	}

	ok := color.New(color.FgGreen).SprintFunc()
	bad := color.New(color.FgRed).SprintFunc()
	if cfg.NoColor {
		color.NoColor = true
	}

	if v.Passed() {
		fmt.Printf("%s - %d/%d keys verified\n", ok("PASS"), v.Verified, v.Total)
		return nil
	}

	fmt.Printf("%s - %d missing, %d mismatched (of %d)\n",
		bad("FAIL"), v.Missing, v.Mismatched, v.Total)
	if v.SampleFailures != "" {
		fmt.Printf("Sample failing keys:%s\n", v.SampleFailures)
	}

	return fmt.Errorf("data verify failed")
}

func init() {
	dataSeedCmd.Flags().IntVar(&seedKeys, "keys", 1000, "number of keys to insert")
	dataVerifyCmd.Flags().IntVar(&seedKeys, "keys", 1000, "number of keys to verify (must match what was seeded)")
	dataCmd.AddCommand(dataSeedCmd, dataVerifyCmd)
	rootCmd.AddCommand(dataCmd)
}
