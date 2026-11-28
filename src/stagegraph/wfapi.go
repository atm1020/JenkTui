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

	normalized, rawEnds, mustBeBranch := normalizeWFAPIStages(run.Stages, active, hasFlowNodeData, liveEnd)

	nodes := make([]StageNode, 0, len(normalized))
	rawIndices := make([]int, 0, len(normalized))

	i := 0
	for i < len(normalized) {
		first := normalized[i]
		if !canParticipateInOverlap(first) {
			nodes = append(nodes, first)
			rawIndices = append(rawIndices, i)
			i++
			continue
		}

		// When a Declarative parallel block is running or completed, Jenkins
		// wfapi reports the wrapper stage with only its brief pre-fork setup
		// duration (and while branches are running, its recorded end time can
		// advance to the latest running branch's start time). Emit it
		// standalone so the following overlapping branches cluster and attach
		// it as their Declarative wrapper instead of pulling it in as a peer
		// branch.
		if isPreForkWrapper(i, normalized, run.Stages, rawEnds, mustBeBranch, hasFlowNodeData, liveEnd) {
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
		for j < len(normalized) {
			cand := normalized[j]
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
			child := normalized[k]
			child.IsParallel = true
			children = append(children, child)
		}

		// Attach an immediately preceding Declarative wrapper stage when its
		// flow-node list is empty (if full details are present) or its pre-fork
		// end time aligns tightly with the start of the overlapping cluster (or
		// lands directly on a parallel branch's start time while live).
		if len(nodes) > 0 && !nodes[len(nodes)-1].IsGroup() {
			prevIdx := rawIndices[len(rawIndices)-1]
			prev := nodes[len(nodes)-1]
			prevRaw := run.Stages[prevIdx]
			if !mustBeBranch[prevIdx] && isDeclarativeWrapper(prev, prevRaw, rawEnds[prevIdx], hasFlowNodeData, clusterStart, clusterMaxEnd, children) {
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

// normalizeWFAPIStages repairs Jenkins pipeline-rest-api (wfapi) artifacts that
// occur while a parallel block is actively executing.
//
// Because Jenkins' StandardChunkVisitor walks the flow graph linearly, live
// parallel runs exhibit two wire quirks:
//  1. Concurrent running branches beyond the first head report status="SUCCESS"
//     with a negative durationMillis whose raw end equals an earlier sibling's
//     startTimeMillis (e.g. Unit.start - Integration.start = -13ms).
//  2. Once a later sibling finishes while an earlier sibling is still running,
//     the still-running sibling reports status="SUCCESS" with a tiny positive
//     durationMillis whose raw end equals that finished sibling's startTimeMillis.
//     By contrast, the Declarative wrapper stage while live always ends at a
//     still-live branch's startTimeMillis (never a terminal branch).
//
// When StageFlowNodes are present (fullStages=true), executing flow nodes give
// an authoritative signal directly. Otherwise, the exact millisecond pointer
// invariants above reconstruct live status and elapsed duration from liveEnd.
func normalizeWFAPIStages(
	raw []gojenkins.PipelineNode,
	active bool,
	hasFlowNodeData bool,
	liveEnd int64,
) (nodes []StageNode, rawEnds []int64, mustBeBranch []bool) {
	n := len(raw)
	nodes = make([]StageNode, n)
	rawEnds = make([]int64, n)
	mustBeBranch = make([]bool, n)

	for i := range raw {
		nodes[i] = stageNodeFromWFAPI(raw[i])
		if raw[i].StartTime > 0 {
			rawEnds[i] = raw[i].StartTime + raw[i].Duration
		}
	}
	if !active || n == 0 {
		return nodes, rawEnds, mustBeBranch
	}

	if hasFlowNodeData {
		for i := range raw {
			if stageHasExecutingFlowNode(raw[i].StageFlowNodes) {
				markLiveStage(&nodes[i], liveEnd)
				mustBeBranch[i] = true
			}
		}
	}

	backPtr := make([]int, n)
	fwdPtr := make([]int, n)
	for i := range n {
		backPtr[i] = -1
		fwdPtr[i] = -1
	}

	maxBackIdx := -1
	for i := range raw {
		if raw[i].StartTime <= 0 {
			continue
		}
		if raw[i].Duration < 0 {
			markLiveStage(&nodes[i], liveEnd)
			mustBeBranch[i] = true
			if i > maxBackIdx {
				maxBackIdx = i
			}
			for j := i - 1; j >= 0; j-- {
				if raw[j].StartTime == rawEnds[i] {
					backPtr[i] = j
					mustBeBranch[j] = true
					break
				}
			}
		} else if !hasFlowNodeData && raw[i].Duration > 0 && !nodes[i].Status.IsExecuting() {
			for j := i + 1; j < n; j++ {
				if raw[j].StartTime == rawEnds[i] {
					fwdPtr[i] = j
					break
				}
			}
		}
	}

	if hasFlowNodeData {
		return nodes, rawEnds, mustBeBranch
	}

	isTerminalBranch := func(idx int) bool {
		if idx < 0 || idx >= n {
			return false
		}
		if raw[idx].Duration <= 0 || backPtr[idx] >= 0 || fwdPtr[idx] >= 0 {
			return false
		}
		act := nodes[idx].Status.Activity(0)
		return act != buildstatus.ActivityRunning &&
			act != buildstatus.ActivityPaused &&
			nodes[idx].Status.Outcome() != buildstatus.OutcomeNotBuilt
	}

	for i := range n {
		j := fwdPtr[i]
		if j < 0 || !isTerminalBranch(j) {
			continue
		}
		liveBranch := mustBeBranch[i]
		if !liveBranch && raw[i].Duration <= wrapperMaxSetupDurationMs && liveEnd > rawEnds[i] {
			lastBranch := max(j, maxBackIdx)
			if lastBranch == n-1 {
				hasWrapperBefore := i > 0 && fwdPtr[i-1] == i && raw[i-1].Duration <= wrapperMaxSetupDurationMs
				overlappedByEarlier := false
				for e := range i {
					if fwdPtr[e] < 0 && raw[e].StartTime > 0 && raw[e].Duration > 0 &&
						raw[e].StartTime < raw[i].StartTime &&
						raw[e].StartTime+raw[e].Duration > raw[i].StartTime+overlapJitterMaxMs {
						overlappedByEarlier = true
						break
					}
				}
				skipsIntoCluster := j > i+1 &&
					raw[i+1].StartTime > 0 &&
					raw[i+1].Duration > 0 &&
					raw[i+1].StartTime+raw[i+1].Duration > raw[j].StartTime+overlapJitterMaxMs

				liveBranch = hasWrapperBefore || overlappedByEarlier || skipsIntoCluster
			}
		}
		if liveBranch {
			markLiveStage(&nodes[i], liveEnd)
			mustBeBranch[i] = true
		}
	}

	return nodes, rawEnds, mustBeBranch
}

func stageHasExecutingFlowNode(flowNodes []gojenkins.PipelineNode) bool {
	for i := range flowNodes {
		if buildstatus.PipelineStatus(flowNodes[i].Status).IsExecuting() {
			return true
		}
	}
	return false
}

func markLiveStage(node *StageNode, liveEnd int64) {
	node.Status = buildstatus.StatusInProgress
	elapsed := max(int64(0), liveEnd-node.StartTimeMillis)
	if elapsed > node.DurationMillis || node.DurationMillis < 0 {
		node.DurationMillis = elapsed
	}
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

func isPreForkWrapper(
	idx int,
	nodes []StageNode,
	raw []gojenkins.PipelineNode,
	rawEnds []int64,
	mustBeBranch []bool,
	hasFlowNodeData bool,
	liveEnd int64,
) bool {
	if idx+2 >= len(nodes) || mustBeBranch[idx] {
		return false
	}
	first := nodes[idx]
	if first.Status.Outcome() == buildstatus.OutcomeNotBuilt || raw[idx].Duration <= 0 {
		return false
	}
	if hasFlowNodeData && len(raw[idx].StageFlowNodes) > 0 {
		return false
	}
	if !hasFlowNodeData && first.DurationMillis > wrapperMaxSetupDurationMs {
		return false
	}
	second := nodes[idx+1]
	third := nodes[idx+2]
	if !canParticipateInOverlap(second) || !canParticipateInOverlap(third) {
		return false
	}
	if !overlapsCluster(effectiveEnd(second, liveEnd), second.Status.IsExecuting(), second.DurationMillis, third) {
		return false
	}

	// Find the full extent of the following overlapping cluster so we can
	// recognize a live Declarative wrapper whose raw end time lands on a later
	// branch's start time (not only the first branch's start time).
	cMax := max(effectiveEnd(second, liveEnd), effectiveEnd(third, liveEnd))
	cLive := second.Status.IsExecuting() || third.Status.IsExecuting()
	cMin := positiveMin(second.DurationMillis, third.DurationMillis)
	endIdx := idx + 3
	for endIdx < len(nodes) {
		cand := nodes[endIdx]
		if !canParticipateInOverlap(cand) || !overlapsCluster(cMax, cLive, cMin, cand) {
			break
		}
		if ce := effectiveEnd(cand, liveEnd); ce > cMax {
			cMax = ce
		}
		if cand.Status.IsExecuting() {
			cLive = true
		}
		cMin = positiveMin(cMin, cand.DurationMillis)
		endIdx++
	}

	rawEnd := rawEnds[idx]
	for b := idx + 1; b < endIdx; b++ {
		if rawEnd == nodes[b].StartTimeMillis {
			return true
		}
	}

	gap := second.StartTimeMillis - rawEnd
	maxGap := wrapperMaxGapMs
	if hasFlowNodeData {
		maxGap = wrapperFullDetailsGapMs
	}
	return gap >= -overlapJitterMaxMs && gap <= maxGap
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

func isDeclarativeWrapper(
	prev StageNode,
	prevRaw gojenkins.PipelineNode,
	prevRawEnd int64,
	hasFlowNodeData bool,
	clusterStart, clusterMaxEnd int64,
	children []StageNode,
) bool {
	if prev.StartTimeMillis <= 0 || prev.Status.Outcome() == buildstatus.OutcomeNotBuilt {
		return false
	}
	prevEnd := prevRawEnd
	if prevEnd <= 0 {
		prevEnd = prev.EndTimeMillis()
	}
	matchesChildStart := false
	for i := range children {
		if prevEnd == children[i].StartTimeMillis {
			matchesChildStart = true
			break
		}
	}
	if hasFlowNodeData {
		if len(prevRaw.StageFlowNodes) > 0 {
			return false
		}
		if matchesChildStart {
			return true
		}
		if prev.StartTimeMillis <= clusterStart && prevEnd >= clusterStart {
			return true
		}
		gap := clusterStart - prevEnd
		return gap >= -overlapJitterMaxMs && gap <= wrapperFullDetailsGapMs
	}
	// Classic wfapi omits stageFlowNodes on all stages. Recognize either a
	// wrapper that encloses the full cluster span, a live wrapper whose raw end
	// lands directly on a child branch's start timestamp, or Jenkins' standard
	// pre-fork Declarative wrapper whose short setup ends tightly at the first
	// branch's start time.
	if prev.StartTimeMillis <= clusterStart && prevEnd >= clusterMaxEnd && clusterMaxEnd > clusterStart {
		return true
	}
	if prev.DurationMillis > wrapperMaxSetupDurationMs {
		return false
	}
	if matchesChildStart {
		return true
	}
	gap := clusterStart - prevEnd
	return gap >= -overlapJitterMaxMs && gap <= wrapperMaxGapMs
}

func positiveMin(a, b int64) int64 {
	switch {
	case a <= 0:
		return max(int64(0), b)
	case b <= 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
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
