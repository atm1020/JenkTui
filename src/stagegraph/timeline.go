package stagegraph

import (
	"fmt"
	"math"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
)

// TimelineRow is one flattened row in the Build page's Stage Timeline View.
type TimelineRow struct {
	Node       StageNode
	Depth      int
	IsLast     bool
	TreePrefix string
	Navigable  bool
	NavIndex   int

	HasBar   bool
	BarStart int
	BarLen   int
}

// AxisTick marks one timestamp label along the horizontal time axis.
type AxisTick struct {
	Col   int
	Label string
}

// TimelineLayout holds the precomputed rows, axis ticks, and live marker
// placement for a given bar track width.
type TimelineLayout struct {
	OriginMillis    int64
	TotalSpanMillis int64
	BarWidth        int
	Active          bool
	NowCol          int
	Rows            []TimelineRow
	Ticks           []AxisTick
	NavigableCount  int
}

// Flatten walks the stage hierarchy in display order and assigns tree connector
// prefixes and navigable indices without computing bar geometry.
func Flatten(g Graph) []TimelineRow {
	var rows []TimelineRow
	navIdx := 0
	flattenNodes(g.Nodes, 0, nil, &rows, &navIdx)

	// If every node was marked non-navigable (for example malformed wrapper-only
	// data), fall back to making any node with an ID navigable so the user can
	// still inspect stage details.
	if navIdx == 0 && len(rows) > 0 {
		for i := range rows {
			if rows[i].Node.ID != "" {
				rows[i].Navigable = true
				rows[i].NavIndex = navIdx
				navIdx++
			}
		}
	}
	return rows
}

func flattenNodes(nodes []StageNode, depth int, ancestorMore []bool, out *[]TimelineRow, navIdx *int) {
	for i := range nodes {
		node := nodes[i]
		isLast := i == len(nodes)-1
		navigable := node.IsNavigable()
		idx := -1
		if navigable {
			idx = *navIdx
			*navIdx++
		}
		*out = append(*out, TimelineRow{
			Node:       node,
			Depth:      depth,
			IsLast:     isLast,
			TreePrefix: buildTreePrefix(depth, isLast, ancestorMore),
			Navigable:  navigable,
			NavIndex:   idx,
		})
		if len(node.Children) > 0 {
			nextAncestors := append(append([]bool(nil), ancestorMore...), !isLast)
			flattenNodes(node.Children, depth+1, nextAncestors, out, navIdx)
		}
	}
}

func buildTreePrefix(depth int, isLast bool, ancestorMore []bool) string {
	if depth <= 0 {
		return ""
	}
	var b strings.Builder
	for d := 1; d < depth; d++ {
		if d < len(ancestorMore) && ancestorMore[d] {
			b.WriteString("│  ")
		} else {
			b.WriteString("   ")
		}
	}
	if isLast {
		b.WriteString("└─ ")
	} else {
		b.WriteString("├─ ")
	}
	return b.String()
}

// InitialFocusIndex selects the initial focused navigable stage index:
//  1. First failed navigable stage.
//  2. First executing or paused navigable stage.
//  3. First navigable stage (0), or -1 if none exist.
func InitialFocusIndex(rows []TimelineRow) int {
	firstActive := -1
	firstNavigable := -1
	for i := range rows {
		r := &rows[i]
		if !r.Navigable {
			continue
		}
		if firstNavigable < 0 {
			firstNavigable = r.NavIndex
		}
		if r.Node.Status.Outcome() == buildstatus.OutcomeFailure {
			return r.NavIndex
		}
		if firstActive < 0 {
			activity := r.Node.Status.Activity(0)
			if activity == buildstatus.ActivityRunning || activity == buildstatus.ActivityPaused {
				firstActive = r.NavIndex
			}
		}
	}
	if firstActive >= 0 {
		return firstActive
	}
	return firstNavigable
}

// ComputeTimeline calculates horizontal bar intervals, axis ticks, and now
// marker placement for g inside a bar track of width barWidth cells.
func ComputeTimeline(g Graph, barWidth int, nowMillis int64) TimelineLayout {
	rows := Flatten(g)
	navCount := 0
	for i := range rows {
		if rows[i].Navigable {
			navCount++
		}
	}

	origin, maxEnd := timelineBounds(g, rows, nowMillis)
	totalSpan := int64(0)
	if origin > 0 && maxEnd > origin {
		totalSpan = maxEnd - origin
	}

	layout := TimelineLayout{
		OriginMillis:    origin,
		TotalSpanMillis: totalSpan,
		BarWidth:        max(0, barWidth),
		Active:          g.Active,
		NowCol:          -1,
		Rows:            rows,
		NavigableCount:  navCount,
	}

	if barWidth <= 0 {
		return layout
	}

	for i := range layout.Rows {
		r := &layout.Rows[i]
		if r.Node.Status.Outcome() == buildstatus.OutcomeNotBuilt || r.Node.StartTimeMillis <= 0 {
			continue
		}
		if totalSpan <= 0 {
			r.HasBar = true
			r.BarStart = 0
			r.BarLen = 1
			continue
		}

		relStart := clampInt64(r.Node.StartTimeMillis-origin, 0, totalSpan)
		dur := max(int64(0), r.Node.DurationMillis)
		if r.Node.Status.IsExecuting() && g.Active && nowMillis > r.Node.StartTimeMillis {
			if elapsed := nowMillis - r.Node.StartTimeMillis; elapsed > dur {
				dur = elapsed
			}
		}
		relEnd := clampInt64(relStart+dur, relStart, totalSpan)

		startCol := int(math.Floor(float64(relStart) / float64(totalSpan) * float64(barWidth)))
		endCol := int(math.Ceil(float64(relEnd) / float64(totalSpan) * float64(barWidth)))
		startCol = clampInt(startCol, 0, barWidth-1)
		endCol = clampInt(endCol, startCol+1, barWidth)

		r.HasBar = true
		r.BarStart = startCol
		r.BarLen = endCol - startCol
	}

	if g.Active {
		if totalSpan > 0 && nowMillis > origin {
			relNow := clampInt64(nowMillis-origin, 0, totalSpan)
			layout.NowCol = clampInt(int(math.Round(float64(relNow)/float64(totalSpan)*float64(barWidth-1))), 0, barWidth-1)
		} else {
			layout.NowCol = barWidth - 1
		}
	}

	layout.Ticks = computeAxisTicks(totalSpan, barWidth, g.Active, layout.NowCol)
	return layout
}

func timelineBounds(g Graph, rows []TimelineRow, nowMillis int64) (origin, maxEnd int64) {
	if g.StartTimeMillis > 0 {
		origin = g.StartTimeMillis
	}
	if g.EndTimeMillis > maxEnd {
		maxEnd = g.EndTimeMillis
	}
	if g.StartTimeMillis > 0 && g.DurationMillis > 0 {
		if end := g.StartTimeMillis + g.DurationMillis; end > maxEnd {
			maxEnd = end
		}
	}

	for i := range rows {
		n := &rows[i].Node
		if n.Status.Outcome() == buildstatus.OutcomeNotBuilt {
			continue
		}
		if n.StartTimeMillis > 0 {
			if origin == 0 || n.StartTimeMillis < origin {
				origin = n.StartTimeMillis
			}
			if end := n.EndTimeMillis(); end > maxEnd {
				maxEnd = end
			}
		}
	}

	if g.Active && origin > 0 && nowMillis > maxEnd {
		maxEnd = nowMillis
	}
	return origin, maxEnd
}

func computeAxisTicks(totalSpan int64, barWidth int, active bool, nowCol int) []AxisTick {
	if barWidth <= 0 {
		return nil
	}
	tickCount := 5
	switch {
	case barWidth < 14:
		tickCount = 2
	case barWidth < 28:
		tickCount = 3
	case barWidth < 48:
		tickCount = 4
	}

	ticks := make([]AxisTick, 0, tickCount)
	lastRight := -1
	for i := range tickCount {
		var col int
		var ms int64
		if tickCount == 1 || i == 0 {
			col = 0
			ms = 0
		} else if i == tickCount-1 {
			col = barWidth - 1
			ms = totalSpan
		} else {
			col = (i * (barWidth - 1)) / (tickCount - 1)
			ms = (totalSpan * int64(i)) / int64(tickCount-1)
		}

		label := FormatTickDuration(ms)
		if active && i == tickCount-1 && nowCol >= 0 {
			label = "now"
			col = nowCol
		}
		// Right-align final tick so it never overflows barWidth.
		start := col
		if start+len(label) > barWidth {
			start = max(0, barWidth-len(label))
		}
		if start <= lastRight && i > 0 {
			continue
		}
		ticks = append(ticks, AxisTick{Col: start, Label: label})
		lastRight = start + len(label)
	}
	return ticks
}

// FormatTickDuration formats a relative axis offset concisely (e.g. "0s",
// "500ms", "12s", "1m30s", "2h5m").
func FormatTickDuration(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	remSecs := secs % 60
	if mins < 60 {
		if remSecs == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dm%ds", mins, remSecs)
	}
	hours := mins / 60
	remMins := mins % 60
	if remMins == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, remMins)
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func clampInt64(v, low, high int64) int64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
