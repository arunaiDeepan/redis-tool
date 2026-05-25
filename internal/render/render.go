// Package render formats cluster status into the table,
// text/tabwriter handles alignment
package render

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/redis-tool/redis-tool/internal/model"
)

func Status(out io.Writer, cs *model.ClusterStatus, noColor bool) {
	if noColor {
		color.NoColor = true
	}

	state := strings.ToLower(cs.ClusterState)
	stateColor := color.New(color.FgGreen).SprintFunc()
	if state != "ok" {
		stateColor = color.New(color.FgRed).SprintFunc()
	}

	fmt.Fprintf(out, "Cluster State: %s\n\n", stateColor(state))

	roles := cs.ParseClusterNodes()
	byAddr := cs.ByAddress()

	idToAddr := make(map[string]string, len(roles))
	for _, r := range roles {
		idToAddr[r.NodeID] = r.Address
	}

	// MASTERS
	fmt.Fprintln(out, "MASTERS")
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	masters := filterRoles(roles, true)
	sort.Slice(masters, func(i, j int) bool { return masters[i].Address < masters[j].Address })
	for _, m := range masters {
		n := byAddr[m.Address]
		keys, mem, ver := nodeFacts(n)
		fmt.Fprintf(tw, "%s\t[master]\tv%s\tslots: %s\tkeys: %d\tmem: %s\n",
			m.Address, ver, fmtSlots(m.Slots), keys, mem)
	}

	tw.Flush()

	// REPLICAS
	fmt.Fprintln(out, "\nREPLICAS")
	tw = tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)

	replicas := filterRoles(roles, false)
	sort.Slice(replicas, func(i, j int) bool { return replicas[i].Address < replicas[j].Address })

	for _, r := range replicas {
		n := byAddr[r.Address]
		_, mem, ver := nodeFacts(n)
		masterAddr := idToAddr[r.MasterID]
		fmt.Fprintf(tw, "%s\t[replica]\tv%s\treplicating: %s\tmem: %s\n",
			r.Address, ver, masterAddr, mem)
	}

	tw.Flush()
}

func filterRoles(in []model.NodeRole, masters bool) []model.NodeRole {
	var out []model.NodeRole

	for _, r := range in {
		if masters && r.IsMaster {
			out = append(out, r)
		} else if !masters && r.IsReplica {
			out = append(out, r)
		}
	}

	return out
}

func fmtSlots(srs []model.SlotRange) string {
	if len(srs) == 0 {
		return "-"
	}

	parts := make([]string, 0, len(srs))
	for _, s := range srs {
		parts = append(parts, s.String())
	}

	return strings.Join(parts, ",")
}

func nodeFacts(n *model.Node) (keys int, mem, ver string) {
	if n == nil {
		return 0, "?", "?"
	}

	ver = n.Version
	mem = n.MemoryHuman
	keys = n.DBSize

	if mem == "" {
		mem = "?"
	}

	if ver == "" {
		ver = "?"
	}

	return
}
