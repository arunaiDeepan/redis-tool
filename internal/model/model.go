// Package model defines the shapes that mirror the JSON our Ansible playbooks
// write to the control node's .run/ directory. The Go CLI parses these and
// never has to regex stdout
package model

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ClusterStatus mirrors .run/status.json written by playbooks/gather_status.yml
type ClusterStatus struct {
	GeneratedAt     string `json:"generated_at"`
	ClusterState    string `json:"cluster_state"` // "ok" | "fail"
	ClusterInfoRaw  string `json:"cluster_info_raw"`
	ClusterNodesRaw string `json:"cluster_nodes_raw"`
	Nodes           []Node `json:"nodes"`
}

type Node struct {
	Name               string `json:"name"`
	IP                 string `json:"ip"`
	Port               int    `json:"port"`
	Reachable          bool   `json:"reachable"`
	Version            string `json:"version"`
	InfoReplicationRaw string `json:"info_replication_raw"`
	MemoryHuman        string `json:"memory_human"`
	DBSize             int    `json:"dbsize"`
}

// UnmarshalJSON tolerates Ansible serializing dbsize as either a JSON number
// or a quoted string. Ansible's native-type preservation in set_fact is
// inconsistent for filtered values, so we accept both forms.
func (n *Node) UnmarshalJSON(data []byte) error {
	type alias Node
	aux := struct {
		DBSize json.RawMessage `json:"dbsize"`
		*alias
	}{alias: (*alias)(n)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.DBSize) == 0 {
		return nil
	}
	if err := json.Unmarshal(aux.DBSize, &n.DBSize); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(aux.DBSize, &s); err != nil {
		return fmt.Errorf("dbsize: not a number or string: %s", aux.DBSize)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		n.DBSize = -1
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("dbsize: cannot parse %q as int", s)
	}
	n.DBSize = v
	return nil
}

// Role + slot data parsed from cluster_nodes_raw.
type NodeRole struct {
	NodeID     string // 40-char hex
	Address    string // "10.10.0.11:6379"
	IP         string
	Port       int
	Flags      []string // master, slave, myself, fail?, ...
	IsMaster   bool
	IsReplica  bool
	MasterID   string // for replicas: their master's node id
	LinkState  string // "connected" | "disconnected"
	Slots      []SlotRange
	SlotsCount int
}

type SlotRange struct {
	Start int
	End   int
}

func (s SlotRange) String() string {
	if s.Start == s.End {
		return fmt.Sprintf("%d", s.Start)
	}
	return fmt.Sprintf("%d-%d", s.Start, s.End)
}

// Load reads and parses status.json.
func Load(path string) (*ClusterStatus, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cs ClusterStatus
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cs, nil
}

// ParseClusterNodes turns the `CLUSTER NODES` text dump into structured roles
// Format reference: https://redis.io/commands/cluster-nodes/
func (cs *ClusterStatus) ParseClusterNodes() []NodeRole {
	var out []NodeRole
	for _, line := range strings.Split(strings.TrimSpace(cs.ClusterNodesRaw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		role := NodeRole{NodeID: fields[0]}
		// fields[1]: <ip>:<port>@<bus-port>[,hostname]

		addr := fields[1]
		if at := strings.Index(addr, "@"); at >= 0 {
			addr = addr[:at]
		}

		role.Address = addr
		if colon := strings.LastIndex(addr, ":"); colon > 0 {
			role.IP = addr[:colon]
			fmt.Sscanf(addr[colon+1:], "%d", &role.Port)
		}

		role.Flags = strings.Split(fields[2], ",")
		for _, f := range role.Flags {
			switch f {
			case "master":
				role.IsMaster = true
			case "slave":
				role.IsReplica = true
			}
		}

		if fields[3] != "-" {
			role.MasterID = fields[3]
		}

		role.LinkState = fields[7]

		// Remaining fields are slot ranges (masters only) or empty (replicas)
		for _, slot := range fields[8:] {
			if strings.HasPrefix(slot, "[") {
				// importing/migrating annotation - skip for status display
				continue
			}
			sr := parseSlot(slot)
			role.Slots = append(role.Slots, sr)
			role.SlotsCount += sr.End - sr.Start + 1
		}

		out = append(out, role)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].IsMaster != out[j].IsMaster {
			return out[i].IsMaster // masters first
		}

		return out[i].Address < out[j].Address
	})

	return out
}

func parseSlot(s string) SlotRange {
	var sr SlotRange

	if dash := strings.Index(s, "-"); dash >= 0 {
		fmt.Sscanf(s[:dash], "%d", &sr.Start)
		fmt.Sscanf(s[dash+1:], "%d", &sr.End)
	} else {
		fmt.Sscanf(s, "%d", &sr.Start)
		sr.End = sr.Start
	}

	return sr
}

// Convenience: lookup table by IP:port.
func (cs *ClusterStatus) ByAddress() map[string]*Node {
	m := make(map[string]*Node, len(cs.Nodes))

	for i := range cs.Nodes {
		n := &cs.Nodes[i]
		m[fmt.Sprintf("%s:%d", n.IP, n.Port)] = n
	}

	return m
}

// VerifyResult mirrors .run/verify.json.
type VerifyResult struct {
	Verified       int    `json:"verified"`
	Missing        int    `json:"missing"`
	Mismatched     int    `json:"mismatched"`
	Total          int    `json:"total"`
	SampleFailures string `json:"sample_failures"`
}

func (v VerifyResult) Passed() bool {
	return v.Missing == 0 && v.Mismatched == 0 && v.Verified == v.Total
}

func LoadVerify(path string) (*VerifyResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var v VerifyResult
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &v, nil
}

// SeedResult mirrors .run/seed.json
type SeedResult struct {
	Requested       int             `json:"requested"`
	Result          SeedResultInner `json:"result"`
	DistributionRaw string          `json:"distribution_raw"`
}

type SeedResultInner struct {
	Inserted int `json:"inserted"`
	Failed   int `json:"failed"`
	Total    int `json:"total"`
}

func LoadSeed(path string) (*SeedResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s SeedResult
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	return &s, nil
}
