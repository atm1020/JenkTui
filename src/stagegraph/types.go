// Package stagegraph builds a unified stage hierarchy and timeline geometry
// from either Jenkins' optional pipeline-graph-view plugin or standard wfapi
// stage summaries. Both the Job page and the Build page consume this package so
// parallel grouping, status aggregation, and timeline placement stay consistent.
package stagegraph

import (
	"slices"

	"github.com/atm1020/jenktui/src/buildstatus"
)

// Source identifies where a Graph's hierarchy originated.
type Source string

const (
	SourceWFAPI       Source = "wfapi"
	SourcePluginGraph Source = "plugin-graph"
)

// Graph is the normalized stage hierarchy for a single pipeline run.
type Graph struct {
	Source          Source
	StartTimeMillis int64
	EndTimeMillis   int64
	DurationMillis  int64
	Active          bool
	Nodes           []StageNode
}

// StageNode represents one stage, parallel branch, or parallel group container.
type StageNode struct {
	ID              string
	Name            string
	Status          buildstatus.PipelineStatus
	StartTimeMillis int64
	DurationMillis  int64
	ExecNode        string
	IsParallel      bool
	IsWrapper       bool
	Children        []StageNode
}

// IsGroup reports whether this node is a container holding child stages.
func (n StageNode) IsGroup() bool {
	return len(n.Children) > 0
}

// IsNavigable reports whether stage navigation (prev/next stage) should land on
// this node. Empty Declarative wrappers and synthetic parallel group containers
// are skipped so users step directly across actionable stages and branches.
func (n StageNode) IsNavigable() bool {
	if n.IsWrapper && len(n.Children) > 0 {
		return false
	}
	return n.ID != ""
}

// EndTimeMillis returns the node's recorded end timestamp in milliseconds, or 0
// when StartTimeMillis is unset.
func (n StageNode) EndTimeMillis() int64 {
	if n.StartTimeMillis <= 0 {
		return 0
	}
	if n.DurationMillis <= 0 {
		return n.StartTimeMillis
	}
	return n.StartTimeMillis + n.DurationMillis
}

// HasRunningStage reports whether any stage in the graph is currently executing.
func (g Graph) HasRunningStage() bool {
	return nodesHaveRunning(g.Nodes)
}

func nodesHaveRunning(nodes []StageNode) bool {
	for i := range nodes {
		if nodes[i].Status.Activity(0) == buildstatus.ActivityRunning {
			return true
		}
		if nodesHaveRunning(nodes[i].Children) {
			return true
		}
	}
	return false
}

// FindNodeByID looks up the first node matching id anywhere in the graph.
func (g Graph) FindNodeByID(id string) (StageNode, bool) {
	if id == "" {
		return StageNode{}, false
	}
	return findNodeByID(g.Nodes, id)
}

func findNodeByID(nodes []StageNode, id string) (StageNode, bool) {
	for i := range nodes {
		if nodes[i].ID == id {
			return nodes[i], true
		}
		if found, ok := findNodeByID(nodes[i].Children, id); ok {
			return found, true
		}
	}
	return StageNode{}, false
}

// AggregateStatus returns the worst status among nodes using severity order:
// failed > running > paused > unstable > aborted > pending/queued > success >
// not executed > unknown.
func AggregateStatus(nodes []StageNode) buildstatus.PipelineStatus {
	if len(nodes) == 0 {
		return buildstatus.StatusNotExecuted
	}
	var best buildstatus.PipelineStatus
	bestRank := -1
	for i := range nodes {
		s := nodes[i].Status
		if len(nodes[i].Children) > 0 {
			childAgg := AggregateStatus(nodes[i].Children)
			if statusSeverity(childAgg) > statusSeverity(s) {
				s = childAgg
			}
		}
		if r := statusSeverity(s); r > bestRank {
			best = s
			bestRank = r
		}
	}
	return best
}

func statusSeverity(status buildstatus.PipelineStatus) int {
	outcome := status.Outcome()
	activity := status.Activity(0)
	switch {
	case outcome == buildstatus.OutcomeFailure:
		return 80
	case activity == buildstatus.ActivityRunning:
		return 70
	case activity == buildstatus.ActivityPaused:
		return 60
	case outcome == buildstatus.OutcomeUnstable:
		return 50
	case outcome == buildstatus.OutcomeAborted:
		return 40
	case activity == buildstatus.ActivityQueued, activity == buildstatus.ActivityPending:
		return 30
	case outcome == buildstatus.OutcomeSuccess:
		return 20
	case outcome == buildstatus.OutcomeNotBuilt:
		return 10
	default:
		return 0
	}
}

// SelectCardBranches selects up to maxVisible branches for a fixed-height Job
// page Parallel Group Card. When len(branches) <= maxVisible, all branches are
// returned in their original order with overflow == 0. When exceeded, one slot
// is reserved for the "+N more" summary line and the remaining slots are filled
// by status priority (failed, running, paused, unstable, aborted, pending,
// success, not executed) with stable original-order tie-breaking.
func SelectCardBranches(branches []StageNode, maxVisible int) (visible []StageNode, overflow int) {
	if maxVisible <= 0 || len(branches) == 0 {
		return nil, len(branches)
	}
	if len(branches) <= maxVisible {
		return slices.Clone(branches), 0
	}
	slots := max(0, maxVisible-1)
	if slots == 0 {
		return nil, len(branches)
	}

	type indexed struct {
		node StageNode
		idx  int
		rank int
	}
	ranked := make([]indexed, len(branches))
	for i, b := range branches {
		ranked[i] = indexed{node: b, idx: i, rank: statusSeverity(b.Status)}
	}
	slices.SortStableFunc(ranked, func(a, b indexed) int {
		if a.rank != b.rank {
			// Higher severity comes first.
			return b.rank - a.rank
		}
		return a.idx - b.idx
	})

	visible = make([]StageNode, slots)
	for i := range slots {
		visible[i] = ranked[i].node
	}
	return visible, len(branches) - slots
}
