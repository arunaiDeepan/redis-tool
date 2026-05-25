package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// realistic CLUSTER NODES dump from a 6-node 3+3 cluster on 10.10.0.11-16
// 40-char hex node ids; flags include "myself" on the local-view node
const clusterNodesFixture = `aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 10.10.0.11:6379@16379 myself,master - 0 0 1 connected 0-5460
bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 10.10.0.12:6379@16379 master - 0 0 2 connected 5461-10922
cccccccccccccccccccccccccccccccccccccccc 10.10.0.13:6379@16379 master - 0 0 3 connected 10923-16383
dddddddddddddddddddddddddddddddddddddddd 10.10.0.14:6379@16379 slave aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 0 0 1 connected
eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee 10.10.0.15:6379@16379 slave bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 0 0 2 connected
ffffffffffffffffffffffffffffffffffffffff 10.10.0.16:6379@16379 slave cccccccccccccccccccccccccccccccccccccccc 0 0 3 connected`

func buildFixture(version string) *ClusterStatus {
	cs := &ClusterStatus{
		GeneratedAt:     "2026-05-25T07:00:00Z",
		ClusterState:    "ok",
		ClusterInfoRaw:  "cluster_state:ok\ncluster_slots_assigned:16384\n",
		ClusterNodesRaw: clusterNodesFixture,
	}

	for i, ip := range []string{"10.10.0.11", "10.10.0.12", "10.10.0.13", "10.10.0.14", "10.10.0.15", "10.10.0.16"} {
		cs.Nodes = append(cs.Nodes, Node{
			Name: "redis-node-" + string(rune('1'+i)), IP: ip, Port: 6379, Reachable: true,
			Version: version, MemoryHuman: "2.0M", DBSize: 333,
			InfoReplicationRaw: "role:" + ifelse(i < 3, "master", "slave") + "\nmaster_link_status:up\n",
		})
	}

	return cs
}

func ifelse(b bool, a, c string) string {
	if b {
		return a
	}
	return c
}

func TestParseClusterNodes_RolesAndSlots(t *testing.T) {
	cs := buildFixture("7.0.15")

	roles := cs.ParseClusterNodes()
	if len(roles) != 6 {
		t.Fatalf("expected 6 roles, got %d", len(roles))
	}

	masters, replicas := 0, 0
	totalSlots := 0

	for _, r := range roles {
		if r.IsMaster {
			masters++
			totalSlots += r.SlotsCount
		}
		if r.IsReplica {
			replicas++
		}
	}

	if masters != 3 || replicas != 3 {
		t.Errorf("expected 3+3, got %d masters / %d replicas", masters, replicas)
	}

	if totalSlots != 16384 {
		t.Errorf("expected 16384 slots covered, got %d", totalSlots)
	}
}

func TestParseClusterNodes_MasterIDLinking(t *testing.T) {
	cs := buildFixture("7.0.15")

	roles := cs.ParseClusterNodes()

	master := map[string]bool{}

	for _, r := range roles {
		if r.IsMaster {
			master[r.NodeID] = true
		}
	}

	for _, r := range roles {
		if r.IsReplica && !master[r.MasterID] {
			t.Errorf("replica %s points at unknown master %s", r.Address, r.MasterID)
		}
	}
}

func TestLoad_RoundTrip(t *testing.T) {
	cs := buildFixture("7.0.15")

	b, err := json.Marshal(cs)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "status.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.ClusterState != "ok" {
		t.Errorf("expected ok, got %q", loaded.ClusterState)
	}

	if len(loaded.Nodes) != 6 {
		t.Errorf("expected 6 nodes, got %d", len(loaded.Nodes))
	}
}

func TestVerifyResult_Passed(t *testing.T) {
	cases := []struct {
		v    VerifyResult
		want bool
	}{
		{VerifyResult{Verified: 1000, Total: 1000}, true},
		{VerifyResult{Verified: 999, Missing: 1, Total: 1000}, false},
		{VerifyResult{Verified: 999, Mismatched: 1, Total: 1000}, false},
		{VerifyResult{Verified: 1000, Total: 1001}, false},
	}
	for _, tc := range cases {
		if got := tc.v.Passed(); got != tc.want {
			t.Errorf("Passed(%+v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestByAddress(t *testing.T) {
	cs := buildFixture("7.0.15")

	m := cs.ByAddress()
	if len(m) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(m))
	}

	if n, ok := m["10.10.0.11:6379"]; !ok || n.Name != "redis-node-1" {
		t.Errorf("missing or wrong entry for 10.10.0.11:6379: %+v", n)
	}
}
