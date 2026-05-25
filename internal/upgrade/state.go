// Package upgrade holds the rolling-upgrade state machine. Go owns the order
// (replicas first, then masters via failover), the health gates between
// nodes, and the user-facing progress lines. Ansible is the dumb verb.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/redis-tool/redis-tool/internal/logging"
	"github.com/redis-tool/redis-tool/internal/model"
	"github.com/redis-tool/redis-tool/internal/runner"
)

// inventoryHostFor maps a Redis announce IP (10.10.0.1X) back to the inventory
// hostname (redis-node-N). The map is built once from the gather_status output.
type Plan struct {
	Replicas []Step // upgrade these first
	Masters  []Step // upgrade these second, each preceded by a failover
}

type Step struct {
	Address     string // 10.10.0.1X:6379
	InvHost     string // inventory hostname
	IsMaster    bool
	ReplicaInv  string // for master steps: inventory hostname of the replica we'll fail over to
	ReplicaAddr string
}

type Orchestrator struct {
	Runner        *runner.Runner
	Logger        *logging.Logger
	Out           io.Writer
	TargetVer     string
	StatusJSON    string // path to .run/status.json
	VerifyJSON    string // path to .run/verify.json
	HealthDelay   time.Duration
	HealthTimeout time.Duration
}

// BuildPlan inspects current cluster topology and returns the upgrade plan.
// Hosts already on TargetVer are skipped (idempotency).
func (o *Orchestrator) BuildPlan(cs *model.ClusterStatus, hostByAddr map[string]string) (*Plan, error) {
	roles := cs.ParseClusterNodes()
	byAddr := cs.ByAddress()

	// Index replicas by their master id so we can pair them up.
	replicasByMasterID := make(map[string][]model.NodeRole)
	roleByID := make(map[string]model.NodeRole, len(roles))
	for _, r := range roles {
		roleByID[r.NodeID] = r
		if r.IsReplica {
			replicasByMasterID[r.MasterID] = append(replicasByMasterID[r.MasterID], r)
		}
	}

	plan := &Plan{}
	for _, r := range roles {
		invHost := hostByAddr[r.Address]
		n := byAddr[r.Address]
		// Skip nodes already on the target version.
		if n != nil && n.Version == o.TargetVer {
			continue
		}
		if r.IsReplica {
			plan.Replicas = append(plan.Replicas, Step{
				Address: r.Address, InvHost: invHost, IsMaster: false,
			})
		}
	}

	for _, r := range roles {
		if !r.IsMaster {
			continue
		}

		invHost := hostByAddr[r.Address]
		n := byAddr[r.Address]
		if n != nil && n.Version == o.TargetVer {
			continue
		}

		reps := replicasByMasterID[r.NodeID]
		if len(reps) == 0 {
			return nil, fmt.Errorf("master %s has no replica - cannot fail over safely", r.Address)
		}

		// Prefer a replica that is already on the target version (we just
		// upgraded them first), otherwise fall back to any.
		var chosen model.NodeRole
		for _, rep := range reps {
			if rn := byAddr[rep.Address]; rn != nil && rn.Version == o.TargetVer {
				chosen = rep
				break
			}
		}

		if chosen.NodeID == "" {
			chosen = reps[0]
		}

		plan.Masters = append(plan.Masters, Step{
			Address:     r.Address,
			InvHost:     invHost,
			IsMaster:    true,
			ReplicaInv:  hostByAddr[chosen.Address],
			ReplicaAddr: chosen.Address,
		})
	}

	return plan, nil
}

// Run executes the plan. Stops on first error and leaves cluster as-is.
func (o *Orchestrator) Run(ctx context.Context, plan *Plan, refresh func(context.Context) (*model.ClusterStatus, error)) error {
	ok := color.New(color.FgGreen).SprintFunc()
	fail := color.New(color.FgRed).SprintFunc()
	total := len(plan.Replicas) + len(plan.Masters)

	if total == 0 {
		fmt.Fprintln(o.out(), "All nodes already at target version - nothing to do.")
		return nil
	}

	done := 0

	step := func(s Step, label string) error {
		done++
		o.Logger.Step(ctx, "upgrade_node", "start",
			"node", s.InvHost, "address", s.Address, "role", label, "target", o.TargetVer)
		fmt.Fprintf(o.out(), "[%d/%d] Upgrading %s %s -> %s ... ", done, total, label, s.Address, o.TargetVer)
		_, err := o.Runner.Run(ctx, "upgrade_node.yml", map[string]any{
			"target_node":   s.InvHost,
			"redis_version": o.TargetVer,
		}, "")

		if err != nil {
			fmt.Fprintf(o.out(), "%s\n", fail("FAILED"))
			o.Logger.Step(ctx, "upgrade_node", "error", "node", s.InvHost, "err", err.Error())
			return fmt.Errorf("upgrade %s (%s): %w", s.InvHost, s.Address, err)
		}

		// Health gate
		if err := o.waitClusterOK(ctx, refresh); err != nil {
			fmt.Fprintf(o.out(), "%s\n", fail("UNHEALTHY"))
			return err
		}

		fmt.Fprintf(o.out(), "%s - cluster: ok\n", ok("done"))
		o.Logger.Step(ctx, "upgrade_node", "ok", "node", s.InvHost)

		return nil
	}

	// Phase 1: replicas
	for _, s := range plan.Replicas {
		if err := step(s, "replica"); err != nil {
			return err
		}
	}

	// Phase 2: masters (each preceded by a failover)
	for _, s := range plan.Masters {
		fmt.Fprintf(o.out(), "      Failing over %s -> %s ... ", s.Address, s.ReplicaAddr)

		o.Logger.Step(ctx, "failover", "start", "from", s.InvHost, "to", s.ReplicaInv)
		_, err := o.Runner.Run(ctx, "failover.yml", map[string]any{
			"replica_node": s.ReplicaInv,
		}, "")
		if err != nil {
			fmt.Fprintf(o.out(), "%s\n", fail("FAILED"))
			return fmt.Errorf("failover to %s: %w", s.ReplicaInv, err)
		}

		fmt.Fprintf(o.out(), "%s\n", ok("done"))
		o.Logger.Step(ctx, "failover", "ok", "from", s.InvHost, "to", s.ReplicaInv)

		// At this point s.InvHost is a replica (or about to be) - safe to upgrade.
		if err := step(s, "former-master"); err != nil {
			return err
		}
	}

	return nil
}

func (o *Orchestrator) waitClusterOK(ctx context.Context, refresh func(context.Context) (*model.ClusterStatus, error)) error {
	deadline := time.Now().Add(o.HealthTimeout)
	if o.HealthTimeout == 0 {
		deadline = time.Now().Add(60 * time.Second)
	}

	delay := o.HealthDelay
	if delay == 0 {
		delay = 2 * time.Second
	}

	for {
		cs, err := refresh(ctx)
		if err == nil && strings.EqualFold(cs.ClusterState, "ok") && allReachable(cs) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("cluster did not return to ok within health window")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

func allReachable(cs *model.ClusterStatus) bool {
	for _, n := range cs.Nodes {
		if !n.Reachable {
			return false
		}
	}

	return true
}

func (o *Orchestrator) out() io.Writer {
	if o.Out != nil {
		return o.Out
	}

	return io.Discard
}

// --------------------------- Helpers ---------------------------

func StatusJSONPath(runDir string) string { return filepath.Join(runDir, "status.json") }
func VerifyJSONPath(runDir string) string { return filepath.Join(runDir, "verify.json") }
func SeedJSONPath(runDir string) string   { return filepath.Join(runDir, "seed.json") }
