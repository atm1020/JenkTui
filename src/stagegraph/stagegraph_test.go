package stagegraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"

	"github.com/atm1020/gojenkins"
)

func TestFromWFAPI_WebappBuildShapeAttachesWrapperAndComputesWallClockSpan(t *testing.T) {
	// Mirrors real Jenkins 2.568.2 wfapi output for DevTeam/Frontend/webapp-build:
	// Parallel Tests reports only its ~350ms pre-fork setup and ends at the
	// exact millisecond Unit begins, while Unit, Integration, and Lint overlap.
	base := int64(1_700_000_000_000)
	run := gojenkins.PipelineRun{
		ID:        "5",
		Name:      "#5",
		Status:    "SUCCESS",
		StartTime: base,
		EndTime:   base + 12_900,
		Duration:  12_900,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base + 100, Duration: 2100},
			{ID: "11", Name: "Parallel Tests", Status: "SUCCESS", StartTime: base + 2250, Duration: 350},
			{ID: "15", Name: "Unit", Status: "SUCCESS", StartTime: base + 2600, Duration: 4200},
			{ID: "16", Name: "Integration", Status: "SUCCESS", StartTime: base + 2615, Duration: 6200},
			{ID: "17", Name: "Lint", Status: "SUCCESS", StartTime: base + 2620, Duration: 3200},
			{ID: "30", Name: "Bundle", Status: "SUCCESS", StartTime: base + 8900, Duration: 3100},
		},
	}

	g := FromWFAPI(run)
	if g.Source != SourceWFAPI {
		t.Fatalf("source = %q, want %q", g.Source, SourceWFAPI)
	}
	if len(g.Nodes) != 3 {
		t.Fatalf("top-level nodes = %d, want 3 (Install, Parallel Tests group, Bundle): %#v", len(g.Nodes), g.Nodes)
	}
	if g.Nodes[0].Name != "Install" || g.Nodes[0].IsGroup() {
		t.Fatalf("node[0] = %#v, want sequential Install", g.Nodes[0])
	}

	group := g.Nodes[1]
	if group.Name != "Parallel Tests" || group.ID != "11" || !group.IsWrapper {
		t.Fatalf("group = %#v, want Declarative wrapper Parallel Tests (ID 11)", group)
	}
	if len(group.Children) != 3 {
		t.Fatalf("group children = %d, want 3", len(group.Children))
	}
	wantSpan := (base + 2615 + 6200) - (base + 2250)
	if group.DurationMillis != wantSpan {
		t.Fatalf("group duration = %dms, want wall-clock span %dms", group.DurationMillis, wantSpan)
	}
	for i, wantName := range []string{"Unit", "Integration", "Lint"} {
		if group.Children[i].Name != wantName || !group.Children[i].IsParallel {
			t.Fatalf("child[%d] = %#v, want parallel %q", i, group.Children[i], wantName)
		}
	}

	if g.Nodes[2].Name != "Bundle" || g.Nodes[2].IsGroup() {
		t.Fatalf("node[2] = %#v, want sequential Bundle", g.Nodes[2])
	}
}

func TestFromWFAPI_RespectsFullStageFlowNodesWhenPresent(t *testing.T) {
	base := int64(1_700_000_000_000)
	// When full stage details exist, a preceding stage WITH steps must NOT be
	// swallowed as a wrapper even if its duration is short and end time aligns.
	run := gojenkins.PipelineRun{
		ID: "7", Status: "SUCCESS", StartTime: base, Duration: 5000,
		Stages: []gojenkins.PipelineNode{
			{
				ID: "6", Name: "Quick Prep", Status: "SUCCESS", StartTime: base, Duration: 200,
				StageFlowNodes: []gojenkins.PipelineNode{{ID: "7", Name: "sh"}},
			},
			{
				ID: "10", Name: "Branch A", Status: "SUCCESS", StartTime: base + 200, Duration: 2000,
				StageFlowNodes: []gojenkins.PipelineNode{{ID: "11", Name: "sh"}},
			},
			{
				ID: "12", Name: "Branch B", Status: "SUCCESS", StartTime: base + 210, Duration: 1800,
				StageFlowNodes: []gojenkins.PipelineNode{{ID: "13", Name: "sh"}},
			},
		},
	}

	g := FromWFAPI(run)
	if len(g.Nodes) != 2 {
		t.Fatalf("top-level nodes = %d, want 2 (Quick Prep + synthetic Parallel)", len(g.Nodes))
	}
	if g.Nodes[0].Name != "Quick Prep" || g.Nodes[0].IsGroup() {
		t.Fatalf("Quick Prep with flow nodes was falsely swallowed as wrapper: %#v", g.Nodes[0])
	}
	if !g.Nodes[1].IsWrapper || g.Nodes[1].Name != defaultParallelGroupName || len(g.Nodes[1].Children) != 2 {
		t.Fatalf("node[1] = %#v, want synthetic Parallel wrapper with 2 children", g.Nodes[1])
	}
}

func TestFromWFAPI_ZeroMsAndNotExecutedStagesNeverFalselyGrouped(t *testing.T) {
	base := int64(1_700_000_000_000)
	tests := []struct {
		name   string
		stages []gojenkins.PipelineNode
	}{
		{
			name: "zero millisecond sequential stages",
			stages: []gojenkins.PipelineNode{
				{ID: "1", Name: "Init", Status: "SUCCESS", StartTime: base, Duration: 0},
				{ID: "2", Name: "Check", Status: "SUCCESS", StartTime: base, Duration: 0},
				{ID: "3", Name: "Done", Status: "SUCCESS", StartTime: base, Duration: 0},
			},
		},
		{
			name: "zero start time stages",
			stages: []gojenkins.PipelineNode{
				{ID: "1", Name: "A", Status: "SUCCESS", StartTime: 0, Duration: 3000},
				{ID: "2", Name: "B", Status: "SUCCESS", StartTime: 0, Duration: 3000},
			},
		},
		{
			name: "not executed stages with identical timestamps",
			stages: []gojenkins.PipelineNode{
				{ID: "1", Name: "Skipped A", Status: "NOT_EXECUTED", StartTime: base, Duration: 1000},
				{ID: "2", Name: "Skipped B", Status: "NOT_EXECUTED", StartTime: base, Duration: 1000},
			},
		},
		{
			name: "adjacent stages with tiny clock skew within jitter guard",
			stages: []gojenkins.PipelineNode{
				{ID: "1", Name: "Build", Status: "SUCCESS", StartTime: base, Duration: 2000},
				{ID: "2", Name: "Test", Status: "SUCCESS", StartTime: base + 1985, Duration: 3000},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := FromWFAPI(gojenkins.PipelineRun{ID: "1", Status: "SUCCESS", StartTime: base, Stages: tt.stages})
			if len(g.Nodes) != len(tt.stages) {
				t.Fatalf("nodes = %d, want %d flat stages; got %#v", len(g.Nodes), len(tt.stages), g.Nodes)
			}
			for i, n := range g.Nodes {
				if n.IsGroup() {
					t.Fatalf("node[%d] unexpectedly grouped: %#v", i, n)
				}
			}
		})
	}
}

func TestFromWFAPI_SimultaneousInProgressOverlapEvenAtZeroDuration(t *testing.T) {
	base := int64(1_700_000_000_000)
	run := gojenkins.PipelineRun{
		ID: "9", Status: "IN_PROGRESS", StartTime: base, Duration: 500,
		Stages: []gojenkins.PipelineNode{
			{ID: "10", Name: "Parallel", Status: "IN_PROGRESS", StartTime: base, Duration: 50},
			{ID: "11", Name: "Unit", Status: "IN_PROGRESS", StartTime: base + 50, Duration: 0},
			{ID: "12", Name: "Lint", Status: "IN_PROGRESS", StartTime: base + 55, Duration: 0},
		},
	}

	g := FromWFAPIAt(run, base+500)
	if len(g.Nodes) != 1 || !g.Nodes[0].IsWrapper || len(g.Nodes[0].Children) != 2 {
		t.Fatalf("expected live parallel group with 2 branches, got %#v", g.Nodes)
	}
	if g.Nodes[0].Status != buildstatus.StatusInProgress {
		t.Fatalf("group status = %q, want IN_PROGRESS", g.Nodes[0].Status)
	}
}

func TestDecodePluginTree_NestedHierarchySiblingsAndMixedFormats(t *testing.T) {
	// Tests nested parallel -> sequential (via nextSibling) -> nested parallel,
	// scripted parallel branches, skipped branches, numeric+string IDs, and
	// mixed numeric/formatted duration/timestamp fields.
	payload := `{
		"status": "ok",
		"data": {
			"complete": true,
			"stages": [
				{
					"id": 6,
					"name": "Install",
					"state": "FINISHED",
					"type": "STAGE",
					"agent": "linux-1",
					"startTimeMillis": "1700000000000",
					"totalDurationMillis": "2s"
				},
				{
					"id": "11",
					"title": "Matrix",
					"state": "FAILED",
					"type": "PARALLEL_BLOCK",
					"children": [
						{
							"id": 15,
							"name": "Linux",
							"state": "FINISHED",
							"type": "PARALLEL",
							"children": [
								{
									"id": 16,
									"name": "Compile",
									"state": "FINISHED",
									"type": "STAGE",
									"durationMillis": "1 min 5 sec",
									"nextSibling": {
										"id": 17,
										"name": "Test",
										"state": "FINISHED",
										"type": "STAGE",
										"durationMillis": 3500
									}
								}
							]
						},
						{
							"id": 20,
							"name": "MacOS",
							"state": "FAILED",
							"type": "PARALLEL",
							"totalDurationMillis": "< 1s"
						},
						{
							"id": 25,
							"name": "Windows",
							"state": "SKIPPED",
							"type": "SKIPPED_STAGE"
						}
					]
				}
			]
		}
	}`

	tree, err := DecodePluginTree([]byte(payload))
	if err != nil {
		t.Fatalf("DecodePluginTree: %v", err)
	}
	if !tree.Complete || len(tree.Stages) != 2 {
		t.Fatalf("tree = %#v, want complete with 2 top-level stages", tree)
	}

	base := int64(1_700_000_000_000)
	run := gojenkins.PipelineRun{
		ID: "42", Status: "FAILED", StartTime: base, EndTime: base + 12_000, Duration: 12_000,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base, Duration: 2100, ExecNode: "linux-1"},
			{ID: "16", Name: "Compile", Status: "SUCCESS", StartTime: base + 2200, Duration: 4000},
			{ID: "17", Name: "Test", Status: "SUCCESS", StartTime: base + 6200, Duration: 3500},
			{ID: "20", Name: "MacOS", Status: "FAILED", StartTime: base + 2250, Duration: 5100, ExecNode: "mac-1"},
		},
	}

	g := FromPluginTree(tree, run)
	if g.Source != SourcePluginGraph {
		t.Fatalf("source = %q, want %q", g.Source, SourcePluginGraph)
	}
	if g.Nodes[0].DurationMillis != 2100 {
		t.Fatalf("wfapi duration join failed: got %d, want 2100", g.Nodes[0].DurationMillis)
	}

	matrix := g.Nodes[1]
	if matrix.Name != "Matrix" || !matrix.IsWrapper || matrix.Status != buildstatus.StatusFailed {
		t.Fatalf("matrix = %#v, want failed wrapper", matrix)
	}
	if len(matrix.Children) != 3 {
		t.Fatalf("matrix children = %d, want 3", len(matrix.Children))
	}
	linux := matrix.Children[0]
	if len(linux.Children) != 2 || linux.Children[0].Name != "Compile" || linux.Children[1].Name != "Test" {
		t.Fatalf("nextSibling expansion failed: got %#v", linux.Children)
	}
	if linux.DurationMillis != 7500 {
		t.Fatalf("linux span = %d, want 7500 (Compile+Test)", linux.DurationMillis)
	}
	macos := matrix.Children[1]
	if macos.DurationMillis != 5100 || macos.ExecNode != "mac-1" {
		t.Fatalf("macos wfapi join = %#v, want dur=5100 execNode=mac-1", macos)
	}
	if matrix.Children[2].Status != buildstatus.StatusNotExecuted {
		t.Fatalf("skipped branch status = %q, want NOT_EXECUTED", matrix.Children[2].Status)
	}

	rows := Flatten(g)
	// Expected rows: Install (0), Matrix (0, wrapper), Linux (1, wrapper), Compile (2), Test (2), MacOS (1), Windows (1)
	if len(rows) != 7 {
		t.Fatalf("flattened rows = %d, want 7", len(rows))
	}
	if rows[3].TreePrefix != "│  ├─ " || rows[4].TreePrefix != "│  └─ " {
		t.Fatalf("nested tree prefixes = %q / %q, want '│  ├─ ' / '│  └─ '", rows[3].TreePrefix, rows[4].TreePrefix)
	}
	// Initial focus should jump directly to MacOS (the first failed navigable stage).
	focus := InitialFocusIndex(rows)
	var focused StageNode
	for _, r := range rows {
		if r.Navigable && r.NavIndex == focus {
			focused = r.Node
		}
	}
	if focused.ID != "20" || focused.Name != "MacOS" {
		t.Fatalf("initial focus = %#v (idx %d), want MacOS (ID 20)", focused, focus)
	}
}

func TestFetchTree_ProbingCachingFallbackAndReset(t *testing.T) {
	t.Run("modern overview endpoint caches CapabilityOverview", func(t *testing.T) {
		var overviewHits, legacyHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/job/demo/1/pipeline-overview/tree":
				overviewHits.Add(1)
				_, _ = w.Write([]byte(`{"status":"ok","data":{"complete":true,"stages":[{"id":1,"name":"Build","state":"FINISHED","type":"STAGE"}]}}`))
			case "/job/demo/1/pipeline-graph/tree":
				legacyHits.Add(1)
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		cache := NewCapabilityCache()
		jenkins := gojenkins.CreateJenkins(srv.Client(), srv.URL)
		run := gojenkins.PipelineRun{ID: "1", Base: "/job/demo/1"}

		tree, ok, err := FetchTree(context.Background(), jenkins, cache, "srv-a", run)
		if err != nil || !ok || tree == nil || len(tree.Stages) != 1 {
			t.Fatalf("first fetch = (%v, %v, %v), want 1 stage", tree, ok, err)
		}
		if cache.Get("srv-a") != CapabilityOverview {
			t.Fatalf("capability = %v, want CapabilityOverview", cache.Get("srv-a"))
		}
		// Second fetch hits overview directly without touching legacy.
		_, ok, err = FetchTree(context.Background(), jenkins, cache, "srv-a", run)
		if err != nil || !ok || overviewHits.Load() != 2 || legacyHits.Load() != 0 {
			t.Fatalf("hits overview=%d legacy=%d, want 2 and 0", overviewHits.Load(), legacyHits.Load())
		}
	})

	t.Run("404 overview falls back to legacy endpoint and caches CapabilityLegacy", func(t *testing.T) {
		var overviewHits, legacyHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/job/demo/2/pipeline-overview/tree":
				overviewHits.Add(1)
				http.NotFound(w, r)
			case "/job/demo/2/pipeline-graph/tree":
				legacyHits.Add(1)
				_, _ = w.Write([]byte(`{"stages":[{"id":"1","name":"Test","state":"FINISHED","type":"STAGE"}]}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		cache := NewCapabilityCache()
		jenkins := gojenkins.CreateJenkins(srv.Client(), srv.URL)
		run := gojenkins.PipelineRun{ID: "2", Base: "/job/demo/2"}

		tree, ok, err := FetchTree(context.Background(), jenkins, cache, "srv-b", run)
		if err != nil || !ok || tree == nil {
			t.Fatalf("legacy fetch = (%v, %v, %v)", tree, ok, err)
		}
		if cache.Get("srv-b") != CapabilityLegacy {
			t.Fatalf("capability = %v, want CapabilityLegacy", cache.Get("srv-b"))
		}
		// Second call goes straight to legacy without re-probing overview.
		_, ok, err = FetchTree(context.Background(), jenkins, cache, "srv-b", run)
		if err != nil || !ok || overviewHits.Load() != 1 || legacyHits.Load() != 2 {
			t.Fatalf("hits overview=%d legacy=%d, want 1 and 2", overviewHits.Load(), legacyHits.Load())
		}
	})

	t.Run("double 404 caches CapabilityUnavailable and skips repeat probes", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			http.NotFound(w, nil)
		}))
		defer srv.Close()

		cache := NewCapabilityCache()
		jenkins := gojenkins.CreateJenkins(srv.Client(), srv.URL)
		run := gojenkins.PipelineRun{ID: "3", Base: "/job/demo/3"}

		_, ok, err := FetchTree(context.Background(), jenkins, cache, "srv-c", run)
		if err != nil || ok || cache.Get("srv-c") != CapabilityUnavailable || hits.Load() != 2 {
			t.Fatalf("first probe = (ok=%v, err=%v, cap=%v, hits=%d)", ok, err, cache.Get("srv-c"), hits.Load())
		}
		// Second build load makes zero requests.
		_, ok, err = FetchTree(context.Background(), jenkins, cache, "srv-c", run)
		if err != nil || ok || hits.Load() != 2 {
			t.Fatalf("second probe unexpectedly hit server: hits=%d", hits.Load())
		}
		// Reset clears the cache so the server is probed again.
		cache.Reset()
		if cache.Get("srv-c") != CapabilityUnknown {
			t.Fatalf("after Reset cap = %v, want CapabilityUnknown", cache.Get("srv-c"))
		}
	})

	t.Run("transient 500 does not poison cache and recovers", func(t *testing.T) {
		var attempt atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/job/demo/4/pipeline-overview/tree" {
				http.NotFound(w, r)
				return
			}
			if attempt.Add(1) == 1 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"stages":[{"id":1,"name":"Build","state":"FINISHED","type":"STAGE"}]}}`))
		}))
		defer srv.Close()

		cache := NewCapabilityCache()
		jenkins := gojenkins.CreateJenkins(srv.Client(), srv.URL)
		run := gojenkins.PipelineRun{ID: "4", Base: "/job/demo/4"}

		_, ok, err := FetchTree(context.Background(), jenkins, cache, "srv-d", run)
		if err == nil || ok {
			t.Fatalf("expected transient error on 500, got ok=%v err=%v", ok, err)
		}
		if cache.Get("srv-d") != CapabilityUnknown {
			t.Fatalf("500 poisoned cache: cap=%v, want CapabilityUnknown", cache.Get("srv-d"))
		}
		// Next attempt succeeds.
		tree, ok, err := FetchTree(context.Background(), jenkins, cache, "srv-d", run)
		if err != nil || !ok || tree == nil || cache.Get("srv-d") != CapabilityOverview {
			t.Fatalf("recovery failed: ok=%v err=%v cap=%v", ok, err, cache.Get("srv-d"))
		}
	})
}

func TestAggregateStatusAndSelectCardBranches(t *testing.T) {
	branches := []StageNode{
		{ID: "1", Name: "OK-1", Status: buildstatus.StatusSuccess},
		{ID: "2", Name: "OK-2", Status: buildstatus.StatusSuccess},
		{ID: "3", Name: "Paused", Status: buildstatus.StatusPausedPendingInput},
		{ID: "4", Name: "Running", Status: buildstatus.StatusInProgress},
		{ID: "5", Name: "OK-3", Status: buildstatus.StatusSuccess},
		{ID: "6", Name: "Failed", Status: buildstatus.StatusFailed},
		{ID: "7", Name: "Unstable", Status: buildstatus.StatusUnstable},
	}

	if agg := AggregateStatus(branches); agg != buildstatus.StatusFailed {
		t.Fatalf("AggregateStatus = %q, want FAILED", agg)
	}

	visible, overflow := SelectCardBranches(branches, 5)
	if len(visible) != 4 || overflow != 3 {
		t.Fatalf("SelectCardBranches(7, 5) = (%d visible, %d overflow), want (4, 3)", len(visible), overflow)
	}
	wantOrder := []string{"Failed", "Running", "Paused", "Unstable"}
	for i, want := range wantOrder {
		if visible[i].Name != want {
			t.Fatalf("visible[%d] = %q, want %q", i, visible[i].Name, want)
		}
	}

	// <= 5 branches preserves declaration order with zero overflow.
	small := branches[:4]
	visSmall, ovSmall := SelectCardBranches(small, 5)
	if len(visSmall) != 4 || ovSmall != 0 {
		t.Fatalf("small SelectCardBranches = (%d, %d), want (4, 0)", len(visSmall), ovSmall)
	}
	if visSmall[0].Name != "OK-1" || visSmall[3].Name != "Running" {
		t.Fatalf("small branch order changed: %#v", visSmall)
	}
}

func TestComputeTimeline_GeometryAcrossWidthsAndLiveNowMarker(t *testing.T) {
	base := int64(1_700_000_000_000)
	g := Graph{
		Source:          SourceWFAPI,
		StartTimeMillis: base,
		DurationMillis:  10_000,
		Active:          true,
		Nodes: []StageNode{
			{ID: "1", Name: "Install", Status: buildstatus.StatusSuccess, StartTimeMillis: base, DurationMillis: 2000},
			{
				ID: "2", Name: "Parallel", Status: buildstatus.StatusInProgress, StartTimeMillis: base + 2000, DurationMillis: 8000, IsWrapper: true,
				Children: []StageNode{
					{ID: "3", Name: "Unit", Status: buildstatus.StatusSuccess, StartTimeMillis: base + 2000, DurationMillis: 4000, IsParallel: true},
					{ID: "4", Name: "Integration", Status: buildstatus.StatusInProgress, StartTimeMillis: base + 2000, DurationMillis: 8000, IsParallel: true},
				},
			},
			{ID: "5", Name: "Deploy", Status: buildstatus.StatusNotExecuted},
		},
	}

	for _, barW := range []int{10, 24, 48, 80} {
		layout := ComputeTimeline(g, barW, base+10_000)
		if layout.NowCol < 0 || layout.NowCol >= barW {
			t.Fatalf("barW=%d: NowCol=%d out of [0,%d)", barW, layout.NowCol, barW)
		}
		if len(layout.Ticks) < 2 {
			t.Fatalf("barW=%d: expected >=2 ticks, got %v", barW, layout.Ticks)
		}
		prevEnd := -1
		for _, tick := range layout.Ticks {
			if tick.Col < prevEnd || tick.Col+len(tick.Label) > barW {
				t.Fatalf("barW=%d: tick %+v overlaps previous (end %d) or overflows barW", barW, tick, prevEnd)
			}
			prevEnd = tick.Col + len(tick.Label)
		}
		for i, r := range layout.Rows {
			if r.Node.Status == buildstatus.StatusNotExecuted {
				if r.HasBar {
					t.Fatalf("barW=%d: NOT_EXECUTED row %d should not have a bar", barW, i)
				}
				continue
			}
			if !r.HasBar || r.BarStart < 0 || r.BarLen < 1 || r.BarStart+r.BarLen > barW {
				t.Fatalf("barW=%d: invalid bar on row %d: start=%d len=%d", barW, i, r.BarStart, r.BarLen)
			}
		}
	}
}
