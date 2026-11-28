package stagegraph

import (
	"github.com/atm1020/jenktui/src/buildstatus"

	"github.com/atm1020/gojenkins"
)

const (
	// overlapJitterMaxMs guards against tiny clock or rounding skew between
	// adjacent sequential stages without missing short overlapping stages.
	overlapJitterMaxMs int64 = 50
	// wrapperMaxGapMs is the maximum gap between a Declarative wrapper stage's
	// pre-fork end time and the first parallel branch's start time.
	wrapperMaxGapMs int64 = 150
	// wrapperFullDetailsGapMs allows a slightly wider gap when stageFlowNodes
	// confirms the preceding stage has zero steps of its own.
	wrapperFullDetailsGapMs int64 = 500
	// wrapperMaxSetupDurationMs bounds pre-fork setup duration when classic
	// wfapi omits stageFlowNodes, preventing real sequential stages from being
	// misclassified as Declarative parallel wrappers.
	wrapperMaxSetupDurationMs int64 = 1500
	// defaultParallelGroupName labels synthesized clusters that have no
	// preceding Declarative wrapper stage (for example scripted parallel).
	defaultParallelGroupName = "Parallel"
)

// FromWFAPI reconstructs a stage Graph purely from a Jenkins wfapi PipelineRun
// with zero additional HTTP calls.
func FromWFAPI(run gojenkins.PipelineRun) Graph {
	return FromWFAPIAt(run, 0)
}

// FromWFAPIAt reconstructs a stage Graph from a wfapi PipelineRun using
// nowMillis (when > 0) as the upper bound for open-ended IN_PROGRESS stages.
func FromWFAPIAt(run gojenkins.PipelineRun, nowMillis int64) Graph {
	runStatus := buildstatus.PipelineStatus(run.Status).
		NormalizeQueueState(len(run.Stages), run.EndTime)
	active := runStatus.IsActive(run.EndTime)

	graph := Graph{
		Source:          SourceWFAPI,
		StartTimeMillis: run.StartTime,
		EndTimeMillis:   run.EndTime,
		DurationMillis:  run.Duration,
		Active:          active,
	}
	if len(run.Stages) == 0 {
		return graph
	}

	hasFlowNodeData := false
	liveEnd := liveUpperBound(run, nowMillis)
	for i := range run.Stages {
		if len(run.Stages[i].StageFlowNodes) > 0 {
			hasFlowNodeData = true
			break
		}
	}

	nodes := make([]StageNode, 0, len(run.Stages))
	rawIndices := make([]int, 0, len(run.Stages))

	i := 0
	for i < len(run.Stages) {
		first := stageNodeFromWFAPI(run.Stages[i])
		if !canParticipateInOverlap(first) {
			nodes = append(nodes, first)
			rawIndices = append(rawIndices, i)
			i++
			continue
		}

		// When a Declarative parallel block is still IN_PROGRESS, the wrapper
		// stage itself also stays IN_PROGRESS while reporting only its brief
		// pre-fork setup duration ending at the first branch's start time.
		// Emit it standalone so the following overlapping branches cluster and
		// attach it as their Declarative wrapper instead of pulling it in as a
		// peer branch.
		if isLivePreForkWrapper(first, run.Stages, i, hasFlowNodeData, liveEnd) {
			nodes = append(nodes, first)
			rawIndices = append(rawIndices, i)
			i++
			continue
		}

		clusterStart := first.StartTimeMillis
		clusterMaxEnd := effectiveEnd(first, liveEnd)
		clusterHasLive := first.Status.IsExecuting()
		clusterMinPosDur := first.DurationMillis

		j := i + 1
		for j < len(run.Stages) {
			cand := stageNodeFromWFAPI(run.Stages[j])
			if !canParticipateInOverlap(cand) {
				break
			}
			if !overlapsCluster(clusterMaxEnd, clusterHasLive, clusterMinPosDur, cand) {
				break
			}
			if cand.StartTimeMillis < clusterStart {
				clusterStart = cand.StartTimeMillis
			}
			candEnd := effectiveEnd(cand, liveEnd)
			if candEnd > clusterMaxEnd {
				clusterMaxEnd = candEnd
			}
			if cand.Status.IsExecuting() {
				clusterHasLive = true
			}
			if cand.DurationMillis > 0 && (clusterMinPosDur <= 0 || cand.DurationMillis < clusterMinPosDur) {
				clusterMinPosDur = cand.DurationMillis
			}
			j++
		}

		if j-i < 2 {
			nodes = append(nodes, first)
			rawIndices = append(rawIndices, i)
			i++
			continue
		}

		children := make([]StageNode, 0, j-i)
		for k := i; k < j; k++ {
			child := stageNodeFromWFAPI(run.Stages[k])
			child.IsParallel = true
			children = append(children, child)
		}

		// Attach an immediately preceding Declarative wrapper stage when its
		// flow-node list is empty (if full details are present) or its pre-fork
		// end time aligns tightly with the start of the overlapping cluster.
		if len(nodes) > 0 && !nodes[len(nodes)-1].IsGroup() {
			prev := nodes[len(nodes)-1]
			prevRaw := run.Stages[rawIndices[len(rawIndices)-1]]
			if isDeclarativeWrapper(prev, prevRaw, hasFlowNodeData, clusterStart, clusterMaxEnd) {
				groupStart := minPositive(prev.StartTimeMillis, clusterStart)
				groupEnd := max(prev.EndTimeMillis(), clusterMaxEnd)
				groupDur := max(int64(0), groupEnd-groupStart)
				if groupDur == 0 {
					groupDur = maxChildDuration(children)
				}
				nodes[len(nodes)-1] = StageNode{
					ID:              prev.ID,
					Name:            prev.Name,
					Status:          AggregateStatus(children),
					StartTimeMillis: groupStart,
					DurationMillis:  groupDur,
					ExecNode:        prev.ExecNode,
					IsWrapper:       true,
					Children:        children,
				}
				i = j
				continue
			}
		}

		groupDur := max(int64(0), clusterMaxEnd-clusterStart)
		if groupDur == 0 {
			groupDur = maxChildDuration(children)
		}
		nodes = append(nodes, StageNode{
			Name:            defaultParallelGroupName,
			Status:          AggregateStatus(children),
			StartTimeMillis: clusterStart,
			DurationMillis:  groupDur,
			IsWrapper:       true,
			Children:        children,
		})
		rawIndices = append(rawIndices, i)
		i = j
	}

	graph.Nodes = nodes
	return graph
}

func stageNodeFromWFAPI(stage gojenkins.PipelineNode) StageNode {
	return StageNode{
		ID:              stage.ID,
		Name:            stage.Name,
		Status:          buildstatus.PipelineStatus(stage.Status),
		StartTimeMillis: stage.StartTime,
		DurationMillis:  stage.Duration,
		ExecNode:        stage.ExecNode,
	}
}

func canParticipateInOverlap(node StageNode) bool {
	if node.StartTimeMillis <= 0 {
		return false
	}
	if node.Status.Outcome() == buildstatus.OutcomeNotBuilt {
		return false
	}
	if node.DurationMillis > 0 {
		return true
	}
	activity := node.Status.Activity(0)
	return activity == buildstatus.ActivityRunning || activity == buildstatus.ActivityPaused
}

func isLivePreForkWrapper(first StageNode, stages []gojenkins.PipelineNode, idx int, hasFlowNodeData bool, liveEnd int64) bool {
	if !first.Status.IsExecuting() || first.DurationMillis <= 0 || idx+2 >= len(stages) {
		return false
	}
	if hasFlowNodeData && len(stages[idx].StageFlowNodes) > 0 {
		return false
	}
	if !hasFlowNodeData && first.DurationMillis > wrapperMaxSetupDurationMs {
		return false
	}
	second := stageNodeFromWFAPI(stages[idx+1])
	third := stageNodeFromWFAPI(stages[idx+2])
	if !canParticipateInOverlap(second) || !canParticipateInOverlap(third) {
		return false
	}
	gap := second.StartTimeMillis - first.EndTimeMillis()
	maxGap := wrapperMaxGapMs
	if hasFlowNodeData {
		maxGap = wrapperFullDetailsGapMs
	}
	if gap < -overlapJitterMaxMs || gap > maxGap {
		return false
	}
	return overlapsCluster(effectiveEnd(second, liveEnd), second.Status.IsExecuting(), second.DurationMillis, third)
}

func overlapsCluster(clusterMaxEnd int64, clusterHasLive bool, clusterMinPosDur int64, cand StageNode) bool {
	if clusterHasLive && cand.Status.IsExecuting() {
		return true
	}
	if cand.StartTimeMillis >= clusterMaxEnd {
		return false
	}
	overlap := clusterMaxEnd - cand.StartTimeMillis
	jitter := overlapJitterMaxMs
	minDur := clusterMinPosDur
	if cand.DurationMillis > 0 && (minDur <= 0 || cand.DurationMillis < minDur) {
		minDur = cand.DurationMillis
	}
	if minDur > 0 && minDur/2 < jitter {
		jitter = minDur / 2
	}
	return overlap > jitter && overlap > 0
}

func isDeclarativeWrapper(prev StageNode, prevRaw gojenkins.PipelineNode, hasFlowNodeData bool, clusterStart, clusterMaxEnd int64) bool {
	if prev.StartTimeMillis <= 0 || prev.Status.Outcome() == buildstatus.OutcomeNotBuilt {
		return false
	}
	prevEnd := prev.EndTimeMillis()
	if hasFlowNodeData {
		if len(prevRaw.StageFlowNodes) > 0 {
			return false
		}
		if prev.StartTimeMillis <= clusterStart && prevEnd >= clusterStart {
			return true
		}
		gap := clusterStart - prevEnd
		return gap >= -overlapJitterMaxMs && gap <= wrapperFullDetailsGapMs
	}
	// Classic wfapi omits stageFlowNodes on all stages. Recognize either a
	// wrapper that encloses the full cluster span or Jenkins' standard
	// pre-fork Declarative wrapper whose short setup ends tightly at the first
	// branch's start time.
	if prev.StartTimeMillis <= clusterStart && prevEnd >= clusterMaxEnd && clusterMaxEnd > clusterStart {
		return true
	}
	gap := clusterStart - prevEnd
	return gap >= -overlapJitterMaxMs && gap <= wrapperMaxGapMs && prev.DurationMillis <= wrapperMaxSetupDurationMs
}

func liveUpperBound(run gojenkins.PipelineRun, nowMillis int64) int64 {
	upper := nowMillis
	if run.EndTime > upper {
		upper = run.EndTime
	}
	if run.StartTime > 0 && run.Duration > 0 {
		if end := run.StartTime + run.Duration; end > upper {
			upper = end
		}
	}
	for i := range run.Stages {
		s := &run.Stages[i]
		if s.StartTime > 0 {
			end := s.StartTime + max(int64(0), s.Duration)
			if end > upper {
				upper = end
			}
		}
	}
	return upper
}

func effectiveEnd(node StageNode, liveEnd int64) int64 {
	end := node.EndTimeMillis()
	if node.Status.IsExecuting() && liveEnd > end {
		return liveEnd
	}
	return end
}

func minPositive(a, b int64) int64 {
	switch {
	case a <= 0:
		return b
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

func maxChildDuration(children []StageNode) int64 {
	var best int64
	for i := range children {
		if children[i].DurationMillis > best {
			best = children[i].DurationMillis
		}
	}
	return best
}
