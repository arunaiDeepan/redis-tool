package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/redis-tool/redis-tool/internal/model"
	"github.com/redis-tool/redis-tool/internal/runner"
	"github.com/spf13/cobra"
)

var (
	verifyFull bool
	verifyKeys int
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Comprehensive post-upgrade health check.",
	RunE: runWithPrereq("verify", func(ctx context.Context, r *runner.Runner) error {
		if !verifyFull {
			// Plain --no-full: just run data verify; same as `data verify`.
			return verify(ctx, r, verifyKeys)
		}
		return fullVerify(ctx, r, verifyKeys)
	}),
}

func init() {
	f := verifyCmd.Flags()
	f.BoolVar(&verifyFull, "full", false, "run the full battery: data + version + slots + state + replication")
	f.IntVar(&verifyKeys, "keys", 1000, "number of seeded keys to check")
	rootCmd.AddCommand(verifyCmd)
}

// fullVerify implements the 5 checks from Phase 5.
func fullVerify(ctx context.Context, r *runner.Runner, keys int) error {
	ok := color.New(color.FgGreen).SprintFunc()
	bad := color.New(color.FgRed).SprintFunc()
	if cfg.NoColor {
		color.NoColor = true
	}

	type check struct {
		name string
		fn   func() (bool, string)
	}

	cs, _, err := gatherTopology(ctx, r)
	if err != nil {
		return err
	}
	roles := cs.ParseClusterNodes()

	checks := []check{
		{"Cluster state", func() (bool, string) {
			return strings.EqualFold(cs.ClusterState, "ok"), "cluster_state=" + cs.ClusterState
		}},
		{"Version consistency", func() (bool, string) {
			seen := map[string]int{}
			for _, n := range cs.Nodes {
				seen[n.Version]++
			}
			parts := []string{}
			for v, c := range seen {
				parts = append(parts, fmt.Sprintf("%s×%d", v, c))
			}
			return len(seen) == 1, strings.Join(parts, ", ")
		}},
		{"Slot coverage (16384)", func() (bool, string) {
			covered := 0
			for _, r := range roles {
				if r.IsMaster {
					covered += r.SlotsCount
				}
			}
			return covered == 16384, fmt.Sprintf("%d/16384", covered)
		}},
		{"Every master has a replica", func() (bool, string) {
			countByMaster := map[string]int{}
			masters := map[string]bool{}
			for _, r := range roles {
				if r.IsMaster {
					masters[r.NodeID] = true
				}
			}
			for _, r := range roles {
				if r.IsReplica {
					countByMaster[r.MasterID]++
				}
			}
			missing := 0
			for id := range masters {
				if countByMaster[id] == 0 {
					missing++
				}
			}
			return missing == 0, fmt.Sprintf("%d master(s) without replica", missing)
		}},
		{"Replication links up", func() (bool, string) {
			down := 0
			for _, n := range cs.Nodes {
				if isReplica(&n) && !linkUp(&n) {
					down++
				}
			}
			return down == 0, fmt.Sprintf("%d replicas with master_link_status != up", down)
		}},
	}

	pass := true
	for _, c := range checks {
		good, detail := c.fn()
		if good {
			fmt.Printf("%s %-30s  %s\n", ok("PASS"), c.name, detail)
		} else {
			fmt.Printf("%s %-30s  %s\n", bad("FAIL"), c.name, detail)
			pass = false
		}
	}

	// Data integrity (separate so output ordering matches the spec).
	if err := verify(ctx, r, keys); err != nil {
		pass = false
	}

	if !pass {
		return fmt.Errorf("verify --full reported failures")
	}
	return nil
}

func isReplica(n *model.Node) bool {
	return strings.Contains(n.InfoReplicationRaw, "role:slave")
}

func linkUp(n *model.Node) bool {
	return strings.Contains(n.InfoReplicationRaw, "master_link_status:up")
}
