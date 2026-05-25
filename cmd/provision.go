package cmd

import (
	"context"
	"fmt"

	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/spf13/cobra"
)

var (
	provisionVersion           string
	provisionMasters           int
	provisionReplicasPerMaster int
)

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Install Redis, configure cluster mode, start nodes, form the cluster.",
	RunE: runWithPrereq("provision", func(ctx context.Context, r *runner.Runner) error {
		ver := provisionVersion
		if ver == "" {
			ver = cfg.DefaultRedisVersion
		}
		// Note: --masters and --replicas-per-master are CLI ergonomics for the
		// assignment's interface; topology is hard-coded at 3+3 by the inventory
		// and `--cluster-replicas 1` in provision.yml. If you change either,
		// also update the inventory.
		if provisionMasters != 3 || provisionReplicasPerMaster != 1 {
			fmt.Fprintf(r.Stdout, "Note: this lab is fixed at 3 masters + 3 replicas; ignoring --masters/--replicas-per-master\n")
		}

		if _, err := r.Run(ctx, "preflight.yml", nil, ""); err != nil {
			return err
		}
		if _, err := r.Run(ctx, "provision.yml", map[string]any{
			"redis_version": ver,
		}, ""); err != nil {
			return err
		}

		// Refresh + print final topology, per the assignment.
		if err := gatherAndPrint(ctx, r); err != nil {
			return err
		}
		fmt.Fprintf(r.Stdout, "\nPROVISION COMPLETE - cluster on Redis v%s\n", ver)

		return nil
	}),
}

func init() {
	f := provisionCmd.Flags()
	f.StringVar(&provisionVersion, "version", "", "Redis version to install (default from config)")
	f.IntVar(&provisionMasters, "masters", 3, "number of master nodes")
	f.IntVar(&provisionReplicasPerMaster, "replicas-per-master", 1, "replicas per master")

	rootCmd.AddCommand(provisionCmd)
}
