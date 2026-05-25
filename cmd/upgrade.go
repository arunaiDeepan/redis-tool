package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/redis-tool/redis-tool/internal/model"
	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/redis-tool/redis-tool/internal/upgrade"
	"github.com/spf13/cobra"
)

var (
	upgradeTarget   string
	upgradeStrategy string
	upgradeKeys     int
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Rolling upgrade with zero downtime + verified data integrity.",
	Long: `Rolling upgrade strategy (the only one supported and the one that achieves
zero client-visible downtime):

  1. Preflight: cluster_state:ok, all nodes reachable, target != current,
     pre-upgrade data verify establishes integrity baseline.
  2. Upgrade replicas one at a time. Replica downtime never affects clients.
     Wait for master_link_status:up + cluster_state:ok before continuing.
  3. For each remaining old master: CLUSTER FAILOVER to its (already upgraded)
     replica, then upgrade the demoted node. Wait for cluster_state:ok.
  4. Post-upgrade: data verify (1000/1000), status (all on target).`,
	RunE: runWithPrereq("upgrade", func(ctx context.Context, r *runner.Runner) error {
		if upgradeStrategy != "rolling" {
			return fmt.Errorf("only --strategy rolling is supported")
		}
		if upgradeTarget == "" {
			return fmt.Errorf("--target-version is required")
		}

		// 1. Preflight
		fmt.Println("== Preflight ==")
		if cfg.DryRun {
			// Walk the full upgrade flow with no real topology data.
			_, _ = r.Run(ctx, "gather_status.yml", nil, "")
			_ = verify(ctx, r, upgradeKeys)
			fmt.Println("[dry-run] would compute upgrade plan and walk replicas->failovers->masters")
			_, _ = r.Run(ctx, "upgrade_node.yml", map[string]any{"target_node": "<picked-from-plan>", "redis_version": upgradeTarget}, "")
			_, _ = r.Run(ctx, "failover.yml", map[string]any{"replica_node": "<picked-from-plan>"}, "")
			_ = verify(ctx, r, upgradeKeys)
			_ = gatherAndPrint(ctx, r)
			return nil
		}

		cs, hostByAddr, err := gatherTopology(ctx, r)
		if err != nil {
			return err
		}
		if !strings.EqualFold(cs.ClusterState, "ok") {
			return fmt.Errorf("preflight: cluster_state is %q, refusing to upgrade", cs.ClusterState)
		}

		// Idempotency: short-circuit if every node is already on target.
		allMatch := true
		for _, n := range cs.Nodes {
			if n.Version != upgradeTarget {
				allMatch = false
				break
			}
		}
		if allMatch {
			fmt.Printf("All nodes already on v%s - nothing to do.\n", upgradeTarget)
			return nil
		}

		fmt.Println("== Pre-upgrade data baseline ==")
		if err := verify(ctx, r, upgradeKeys); err != nil {
			return fmt.Errorf("preflight verify failed: %w", err)
		}

		// 2+3. State machine
		o := &upgrade.Orchestrator{
			Runner:        r,
			Logger:        logger,
			Out:           r.Stdout,
			TargetVer:     upgradeTarget,
			HealthDelay:   2 * time.Second,
			HealthTimeout: 90 * time.Second,
		}
		plan, err := o.BuildPlan(cs, hostByAddr)
		if err != nil {
			return err
		}
		fmt.Printf("Plan: %d replicas + %d masters to upgrade\n", len(plan.Replicas), len(plan.Masters))

		refresh := func(ctx context.Context) (*model.ClusterStatus, error) {
			cs, _, err := gatherTopology(ctx, r)
			return cs, err
		}
		if err := o.Run(ctx, plan, refresh); err != nil {
			return err
		}

		// 4. Post-upgrade verification
		fmt.Println("\n== Post-upgrade verification ==")
		if err := verify(ctx, r, upgradeKeys); err != nil {
			return fmt.Errorf("post-upgrade verify failed: %w", err)
		}
		if err := gatherAndPrint(ctx, r); err != nil {
			return err
		}

		ok := color.New(color.FgGreen).SprintFunc()
		if cfg.NoColor {
			color.NoColor = true
		}
		fmt.Printf("\n%s - all nodes on v%s, data integrity verified\n",
			ok(fmt.Sprintf("UPGRADE COMPLETE")), upgradeTarget)

		return nil
	}),
}

// gatherTopology runs gather_status, loads it, AND builds an IP:port ->
// inventory-host map (needed by the state machine to pass --extra-vars).
func gatherTopology(ctx context.Context, r *runner.Runner) (*model.ClusterStatus, map[string]string, error) {
	if err := r.CleanRunArtifacts("status.json"); err != nil {
		return nil, nil, err
	}
	if _, err := r.Run(ctx, "gather_status.yml", nil, ""); err != nil {
		return nil, nil, err
	}

	cs, err := model.Load(upgrade.StatusJSONPath(cfg.RunDir))
	if err != nil {
		return nil, nil, err
	}

	hostByAddr := make(map[string]string, len(cs.Nodes))
	for _, n := range cs.Nodes {
		hostByAddr[fmt.Sprintf("%s:%d", n.IP, n.Port)] = n.Name
	}

	return cs, hostByAddr, nil
}

func init() {
	f := upgradeCmd.Flags()
	f.StringVar(&upgradeTarget, "target-version", "", "target Redis version (e.g. 7.2.6)")
	f.StringVar(&upgradeStrategy, "strategy", "rolling", "upgrade strategy (only 'rolling' is supported)")
	f.IntVar(&upgradeKeys, "keys", 1000, "number of seeded keys to verify before/after")

	rootCmd.AddCommand(upgradeCmd)
}
