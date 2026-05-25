package upgrade

import (
	"testing"

	"github.com/redis-tool/redis-tool/internal/model"
)

const clusterNodesFixture = `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 10.10.0.11:6379@16379 myself,master - 0 0 1 connected 0-5460
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 10.10.0.12:6379@16379 master - 0 0 2 connected 5461-10922
cccccccccccccccccccccccccccccccccccccccc 10.10.0.13:6379@16379 master - 0 0 3 connected 10923-16383
dddddddddddddddddddddddddddddddddddddddd 10.10.0.14:6379@16379 slave aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 0 0 1 connected
eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee 10.10.0.15:6379@16379 slave bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 0 0 2 connected
ffffffffffffffffffffffffffffffffffffffff 10.10.0.16:6379@16379 slave cccccccccccccccccccccccccccccccccccccccc 0 0 3 connected`

func buildStatus(masterVer, replicaVer string) *model.ClusterStatus {
	cs := &model.ClusterStatus{
		ClusterState:    "ok",
		ClusterNodesRaw: clusterNodesFixture,
	}

	addrs := []string{"10.10.0.11", "10.10.0.12", "10.10.0.13", "10.10.0.14", "10.10.0.15", "10.10.0.16"}
	for i, ip := range addrs {
		ver := masterVer
		if i >= 3 {
			ver = replicaVer
		}
		cs.Nodes = append(cs.Nodes, model.Node{
			Name: "redis-node-" + string(rune('1'+i)), IP: ip, Port: 6379, Reachable: true,
			Version: ver,
		})
	}

	return cs
}

func hostMap() map[string]string {
	return map[string]string{
		"10.10.0.11:6379": "redis-node-1",
		"10.10.0.12:6379": "redis-node-2",
		"10.10.0.13:6379": "redis-node-3",
		"10.10.0.14:6379": "redis-node-4",
		"10.10.0.15:6379": "redis-node-5",
		"10.10.0.16:6379": "redis-node-6",
	}
}

func TestBuildPlan_FreshClusterUpgrade(t *testing.T) {
	// Every node on 7.0.15 -> target 7.2.6. Expect 3 replicas + 3 masters in plan.
	o := &Orchestrator{TargetVer: "7.2.6"}
	plan, err := o.BuildPlan(buildStatus("7.0.15", "7.0.15"), hostMap())
	if err != nil {
		t.Fatal(err)
	}

	if got := len(plan.Replicas); got != 3 {
		t.Errorf("expected 3 replicas in plan, got %d", got)
	}

	if got := len(plan.Masters); got != 3 {
		t.Errorf("expected 3 masters in plan, got %d", got)
	}
}

func TestBuildPlan_Idempotent(t *testing.T) {
	// Every node already on target -> plan is empty.
	o := &Orchestrator{TargetVer: "7.2.6"}
	plan, err := o.BuildPlan(buildStatus("7.2.6", "7.2.6"), hostMap())
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Replicas) != 0 || len(plan.Masters) != 0 {
		t.Errorf("expected empty plan, got %d+%d", len(plan.Replicas), len(plan.Masters))
	}
}

func TestBuildPlan_MidUpgradeRestart(t *testing.T) {
	// Replicas already upgraded (someone re-ran after a partial run);
	// masters still on old version. Plan should contain ONLY masters.
	o := &Orchestrator{TargetVer: "7.2.6"}
	plan, err := o.BuildPlan(buildStatus("7.0.15", "7.2.6"), hostMap())
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Replicas) != 0 {
		t.Errorf("expected 0 replicas, got %d", len(plan.Replicas))
	}

	if len(plan.Masters) != 3 {
		t.Errorf("expected 3 masters, got %d", len(plan.Masters))
	}

	// Each master step must name a replica to fail over to, and that replica
	// should already be on the target version.
	for _, s := range plan.Masters {
		if s.ReplicaInv == "" {
			t.Errorf("master %s has no replica selected", s.Address)
		}
	}
}

func TestBuildPlan_MasterPairsWithReplica(t *testing.T) {
	o := &Orchestrator{TargetVer: "7.2.6"}
	plan, err := o.BuildPlan(buildStatus("7.0.15", "7.0.15"), hostMap())
	if err != nil {
		t.Fatal(err)
	}

	// Specific pairings from the fixture: 11↔14, 12↔15, 13↔16.
	expected := map[string]string{
		"10.10.0.11:6379": "10.10.0.14:6379",
		"10.10.0.12:6379": "10.10.0.15:6379",
		"10.10.0.13:6379": "10.10.0.16:6379",
	}

	for _, s := range plan.Masters {
		if got := s.ReplicaAddr; got != expected[s.Address] {
			t.Errorf("master %s: expected replica %s, got %s", s.Address, expected[s.Address], got)
		}
	}
}
