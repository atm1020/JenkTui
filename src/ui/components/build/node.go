package build

import (
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

const builtInNodeDisplayName = "built-in"

// NodeUsage is one distinct executor and the stages that ran on it.
type NodeUsage struct {
	Name   string
	Stages []string
}

// DisplayName renders Name for the UI. Jenkins identifies its built-in node
// with the empty string, which would otherwise render as a blank line.
func (n NodeUsage) DisplayName() string {
	if n.Name == "" {
		return builtInNodeDisplayName
	}
	return n.Name
}

// collectNodes reduces a run's stages to distinct executors, keeping
// first-appearance order.
func collectNodes(stages []gojenkins.PipelineNode) []NodeUsage {
	var nodes []NodeUsage
	indices := make(map[string]int)
	for i := range stages {
		stage := &stages[i]
		index, ok := indices[stage.ExecNode]
		if !ok {
			index = len(nodes)
			indices[stage.ExecNode] = index
			nodes = append(nodes, NodeUsage{Name: stage.ExecNode})
		}
		nodes[index].Stages = append(nodes[index].Stages, stage.Name)
	}
	return nodes
}

// middleTruncate shortens s to maxWidth while keeping its head and tail.
func middleTruncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}

	const ellipsis = "…"
	ellipsisWidth := lipgloss.Width(ellipsis)
	if maxWidth <= ellipsisWidth {
		return ansi.Truncate(ellipsis, maxWidth, "")
	}

	remaining := maxWidth - ellipsisWidth
	headWidth := (remaining + 1) / 2
	tailWidth := remaining - headWidth
	width := lipgloss.Width(s)

	head := ansi.Truncate(s, headWidth, "")
	tail := ansi.Cut(s, width-tailWidth, width)
	return head + ellipsis + tail
}

func copyNoti(node NodeUsage) string {
	if node.Name == "" {
		return "Copied node name (built-in — empty)"
	}
	return "Copied node name"
}
