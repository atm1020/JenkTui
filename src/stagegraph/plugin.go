package stagegraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"

	"github.com/atm1020/gojenkins"
)

// PluginTree is the decoded stage hierarchy returned by Jenkins' optional
// pipeline-graph-view plugin (modern pipeline-overview/tree or legacy
// pipeline-graph/tree). It can be re-joined with newer wfapi snapshots during
// live polling without re-parsing JSON.
type PluginTree struct {
	Complete bool
	Stages   []StageNode
}

type pluginEnvelope struct {
	Status   string          `json:"status"`
	Data     *pluginTreeData `json:"data"`
	Complete *bool           `json:"complete"`
	Stages   []pluginStage   `json:"stages"`
}

type pluginTreeData struct {
	Complete bool          `json:"complete"`
	Stages   []pluginStage `json:"stages"`
}

type pluginStage struct {
	ID                  flexString    `json:"id"`
	Name                string        `json:"name"`
	Title               string        `json:"title"`
	State               string        `json:"state"`
	Status              string        `json:"status"`
	Result              string        `json:"result"`
	Type                string        `json:"type"`
	Agent               string        `json:"agent"`
	ExecNode            string        `json:"execNode"`
	StartTimeMillis     flexTimestamp `json:"startTimeMillis"`
	DurationMillis      flexDuration  `json:"durationMillis"`
	TotalDurationMillis flexDuration  `json:"totalDurationMillis"`
	PauseDurationMillis flexDuration  `json:"pauseDurationMillis"`
	IsSequential        bool          `json:"isSequential"`
	Synthetic           bool          `json:"synthetic"`
	Children            []pluginStage `json:"children"`
	NextSibling         *pluginStage  `json:"nextSibling"`
}

// DecodePluginTree parses a JSON payload from pipeline-overview/tree or
// pipeline-graph/tree, tolerating schema differences across plugin versions.
func DecodePluginTree(payload []byte) (*PluginTree, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty pipeline graph response")
	}

	var env pluginEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, fmt.Errorf("decode pipeline graph tree: %w", err)
	}

	var rawStages []pluginStage
	complete := false
	switch {
	case env.Data != nil:
		rawStages = env.Data.Stages
		complete = env.Data.Complete
	case env.Stages != nil:
		rawStages = env.Stages
		if env.Complete != nil {
			complete = *env.Complete
		}
	default:
		return nil, fmt.Errorf("pipeline graph response missing stages")
	}

	tree := &PluginTree{
		Complete: complete,
		Stages:   convertPluginList(rawStages, false),
	}
	return tree, nil
}

// FromPluginTree produces a unified Graph from a decoded PluginTree and joins
// exact millisecond timestamps, durations, and agents from wfapi run stages by
// matching node ID whenever available.
func FromPluginTree(tree *PluginTree, run gojenkins.PipelineRun) Graph {
	if tree == nil {
		return FromWFAPI(run)
	}
	runStatus := buildstatus.PipelineStatus(run.Status).
		NormalizeQueueState(len(run.Stages), run.EndTime)
	wfByID := make(map[string]gojenkins.PipelineNode, len(run.Stages))
	for i := range run.Stages {
		if run.Stages[i].ID != "" {
			wfByID[run.Stages[i].ID] = run.Stages[i]
		}
	}

	nodes := cloneAndJoinWFAPI(tree.Stages, wfByID)
	return Graph{
		Source:          SourcePluginGraph,
		StartTimeMillis: run.StartTime,
		EndTimeMillis:   run.EndTime,
		DurationMillis:  run.Duration,
		Active:          runStatus.IsActive(run.EndTime),
		Nodes:           nodes,
	}
}

func convertPluginList(raw []pluginStage, parentIsParallel bool) []StageNode {
	if len(raw) == 0 {
		return nil
	}
	out := make([]StageNode, 0, len(raw))
	for i := range raw {
		out = append(out, convertPluginChain(&raw[i], parentIsParallel)...)
	}
	return out
}

func convertPluginChain(head *pluginStage, parentIsParallel bool) []StageNode {
	var out []StageNode
	seen := make(map[*pluginStage]struct{})
	for cur := head; cur != nil; cur = cur.NextSibling {
		if _, dup := seen[cur]; dup {
			break
		}
		seen[cur] = struct{}{}
		out = append(out, convertPluginNode(cur, parentIsParallel))
	}
	return out
}

func convertPluginNode(raw *pluginStage, parentIsParallel bool) StageNode {
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		name = strings.TrimSpace(raw.Title)
	}
	agent := strings.TrimSpace(raw.Agent)
	if agent == "" {
		agent = strings.TrimSpace(raw.ExecNode)
	}

	stageType := strings.ToUpper(strings.TrimSpace(raw.Type))
	isParallelBranch := parentIsParallel || stageType == "PARALLEL"
	isParallelContainer := stageType == "PARALLEL_BLOCK"

	// Children of a PARALLEL_BLOCK are parallel branches; children of a
	// PARALLEL branch are nested sequential stages (unless they themselves have
	// type PARALLEL / PARALLEL_BLOCK).
	children := convertPluginList(raw.Children, isParallelContainer)

	// In pipeline-graph-view, a Declarative parallel wrapper stage often has
	// type "STAGE" while its children have type "PARALLEL".
	allChildrenParallel := len(children) > 0
	for i := range children {
		if !children[i].IsParallel {
			allChildrenParallel = false
			break
		}
	}

	dur := raw.DurationMillis.ms
	if dur <= 0 && raw.TotalDurationMillis.ms > 0 {
		dur = raw.TotalDurationMillis.ms
	}

	isWrapper := raw.Synthetic || isParallelContainer || (len(children) > 0 && (allChildrenParallel || isParallelBranch))
	status := normalizePluginStatus(raw)
	if len(children) > 0 && (isWrapper || status == "") {
		childAgg := AggregateStatus(children)
		if statusSeverity(childAgg) > statusSeverity(status) || status == "" {
			status = childAgg
		}
	}

	return StageNode{
		ID:              string(raw.ID),
		Name:            name,
		Status:          status,
		StartTimeMillis: raw.StartTimeMillis.ms,
		DurationMillis:  dur,
		ExecNode:        agent,
		IsParallel:      isParallelBranch,
		IsWrapper:       isWrapper,
		Children:        children,
	}
}

func normalizePluginStatus(raw *pluginStage) buildstatus.PipelineStatus {
	candidates := []string{raw.State, raw.Status, raw.Result}
	for _, candidate := range candidates {
		switch strings.ToUpper(strings.TrimSpace(candidate)) {
		case "FINISHED", "SUCCESS":
			return buildstatus.StatusSuccess
		case "FAILED", "FAILURE":
			return buildstatus.StatusFailed
		case "UNSTABLE":
			return buildstatus.StatusUnstable
		case "ABORTED":
			return buildstatus.StatusAborted
		case "RUNNING", "IN_PROGRESS":
			return buildstatus.StatusInProgress
		case "PAUSED", "PAUSED_PENDING_INPUT":
			return buildstatus.StatusPausedPendingInput
		case "QUEUED":
			return buildstatus.StatusQueued
		case "PENDING":
			return buildstatus.StatusPending
		case "SKIPPED", "NOT_BUILT", "NOT_EXECUTED":
			return buildstatus.StatusNotExecuted
		}
	}
	if strings.Contains(strings.ToUpper(raw.Type), "SKIPPED") {
		return buildstatus.StatusNotExecuted
	}
	return buildstatus.StatusNotExecuted
}

func cloneAndJoinWFAPI(nodes []StageNode, wfByID map[string]gojenkins.PipelineNode) []StageNode {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]StageNode, len(nodes))
	for i := range nodes {
		n := nodes[i]
		if wf, ok := wfByID[n.ID]; ok {
			if wf.StartTime > 0 {
				n.StartTimeMillis = wf.StartTime
			}
			if wf.Duration > 0 || n.DurationMillis <= 0 {
				n.DurationMillis = wf.Duration
			}
			if n.ExecNode == "" && wf.ExecNode != "" {
				n.ExecNode = wf.ExecNode
			}
			if wf.Status != "" {
				wfStatus := buildstatus.PipelineStatus(wf.Status)
				// Prefer wfapi's live/terminal status when the cached plugin
				// tree is older than a newer wfapi poll snapshot.
				if n.Status == "" || wfStatus.Outcome() != buildstatus.OutcomeUnknown || wfStatus.IsActive(0) {
					n.Status = wfStatus
				}
			}
		}

		if len(n.Children) > 0 {
			n.Children = cloneAndJoinWFAPI(n.Children, wfByID)
			spanStart, spanEnd := childrenSpan(n.Children)
			if spanStart > 0 {
				n.StartTimeMillis = minPositive(n.StartTimeMillis, spanStart)
			}
			end := max(n.EndTimeMillis(), spanEnd)
			if n.StartTimeMillis > 0 && end >= n.StartTimeMillis {
				n.DurationMillis = end - n.StartTimeMillis
			} else if n.DurationMillis <= 0 {
				n.DurationMillis = maxChildDuration(n.Children)
			}
			if n.IsWrapper {
				n.Status = AggregateStatus(n.Children)
			} else {
				childAgg := AggregateStatus(n.Children)
				if statusSeverity(childAgg) > statusSeverity(n.Status) {
					n.Status = childAgg
				}
			}
		}

		out[i] = n
	}
	return out
}

func childrenSpan(children []StageNode) (minStart, maxEnd int64) {
	for i := range children {
		c := &children[i]
		if c.StartTimeMillis > 0 {
			if minStart == 0 || c.StartTimeMillis < minStart {
				minStart = c.StartTimeMillis
			}
		}
		if end := c.EndTimeMillis(); end > maxEnd {
			maxEnd = end
		}
	}
	return minStart, maxEnd
}

// flexString decodes a JSON string or number into a Go string (pipeline-graph-view
// has emitted node IDs as both integers and strings across versions).
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = ""
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(trimmed, &num); err != nil {
		return err
	}
	*f = flexString(num.String())
	return nil
}

// flexTimestamp decodes epoch milliseconds (number or numeric string) or an
// RFC3339 timestamp string into milliseconds since Unix epoch.
type flexTimestamp struct {
	ms int64
}

func (f *flexTimestamp) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		f.ms = 0
		return nil
	}
	if trimmed[0] != '"' {
		var v float64
		if err := json.Unmarshal(trimmed, &v); err != nil {
			return err
		}
		f.ms = int64(math.Round(v))
		return nil
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		f.ms = 0
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		f.ms = n
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		f.ms = t.UnixMilli()
		return nil
	}
	f.ms = 0
	return nil
}

// flexDuration decodes numeric milliseconds or human-formatted durations
// ("350ms", "4s", "1m 20s", "2 min 5 sec", "< 1s") into milliseconds.
type flexDuration struct {
	ms int64
}

func (f *flexDuration) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		f.ms = 0
		return nil
	}
	if trimmed[0] != '"' {
		var v float64
		if err := json.Unmarshal(trimmed, &v); err != nil {
			return err
		}
		f.ms = max(int64(0), int64(math.Round(v)))
		return nil
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err != nil {
		return err
	}
	f.ms = parseFormattedDurationMillis(s)
	return nil
}

func parseFormattedDurationMillis(raw string) int64 {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" || s == "-" || s == "n/a" {
		return 0
	}
	if strings.HasPrefix(s, "<") {
		if strings.Contains(s, "ms") {
			return 1
		}
		return 500
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return max(int64(0), n)
	}
	// Try Go's native duration syntax after collapsing spaces between numbers
	// and units (e.g. "2m 5s" -> "2m5s", "350 ms" -> "350ms").
	normalized := strings.NewReplacer(
		"hours", "h", "hour", "h", "hrs", "h", "hr", "h",
		"minutes", "m", "minute", "m", "mins", "m", "min", "m",
		"seconds", "s", "second", "s", "secs", "s", "sec", "s",
		"milliseconds", "ms", "millisecond", "ms",
		",", " ",
	).Replace(s)
	compact := strings.Join(strings.Fields(normalized), "")
	if d, err := time.ParseDuration(compact); err == nil && d >= 0 {
		return d.Milliseconds()
	}
	return 0
}
