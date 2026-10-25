package job

import (
	"strings"
	"testing"

	"github.com/atm1020/gojenkins"
)

const cacheSentinel = "CACHED_ROW_SENTINEL"

func newCacheTestModel(runs ...*gojenkins.PipelineRun) *StageViewModel {
	svm := NewStageViewModel(120, 40, StageViewKeyMap{})
	prs := make([]gojenkins.PipelineRun, len(runs))
	for i, r := range runs {
		prs[i] = *r
	}
	svm.SetPipelineRun(prs)
	return svm
}

func TestPausedInputRunRendersWaitingForApproval(t *testing.T) {
	svm := newCacheTestModel(fakePipelineRun("42", 1, "PAUSED_PENDING_INPUT"))

	view := svm.View(120, 40)

	if !strings.Contains(view, "Waiting for approval") {
		t.Fatalf("paused input run does not explain required action: %q", view)
	}
}

// A finished run that is neither selected nor previously selected must be
// served from the render cache instead of being rebuilt every frame.
func TestViewServesFinishedNonSelectedRowFromCache(t *testing.T) {
	svm := newCacheTestModel(
		fakePipelineRun("1", 3, "SUCCESS"),
		fakePipelineRun("2", 3, "SUCCESS"),
	)
	// First frame establishes the cache width; the sentinel then proves the
	// second frame reads the cache instead of re-rendering.
	svm.View(120, 40)
	// selectedIndex is 0, so run "2" is a static row.
	poisonStageRow(t, svm, "2", cacheSentinel)

	view := svm.View(120, 40)

	if !strings.Contains(view, cacheSentinel) {
		t.Fatal("finished non-selected row was re-rendered instead of served from cache")
	}
}

// The first render must populate the cache so the next frame can reuse it.
func TestViewPopulatesCacheOnFirstRender(t *testing.T) {
	svm := newCacheTestModel(
		fakePipelineRun("1", 3, "SUCCESS"),
		fakePipelineRun("2", 3, "SUCCESS"),
	)

	svm.View(120, 40)

	entry, ok := svm.stageRenderCache["2"]
	if !ok || entry.Content == "" {
		t.Fatal("cache was not populated for finished run after first render")
	}
}

// Selected and running rows stay cached until their visible state changes.
// A frame that does not move selection or advance animation must not rebuild them.
func TestUnchangedSelectedAndRunningRowsStayCached(t *testing.T) {
	svm := newCacheTestModel(
		fakePipelineRun("1", 3, "SUCCESS"),
		fakePipelineRun("2", 3, "IN_PROGRESS"),
	)
	svm.hasInProgress = true
	svm.View(120, 40)
	poisonStageRow(t, svm, "1", cacheSentinel+"-selected")
	poisonStageRow(t, svm, "2", cacheSentinel+"-running")

	view := svm.View(120, 40)

	if !strings.Contains(view, cacheSentinel+"-selected") || !strings.Contains(view, cacheSentinel+"-running") {
		t.Fatal("unchanged selected or running row was rebuilt")
	}
}

// Fresh poll data may change any run's status or duration, so it must drop
// the whole cache.
func TestSetPipelineRunInvalidatesCache(t *testing.T) {
	svm := newCacheTestModel(
		fakePipelineRun("1", 3, "SUCCESS"),
		fakePipelineRun("2", 3, "IN_PROGRESS"),
	)
	svm.View(120, 40)
	poisonStageRow(t, svm, "2", cacheSentinel)

	// Poll update: run 2 finished.
	svm.SetPipelineRun([]gojenkins.PipelineRun{
		*fakePipelineRun("1", 3, "SUCCESS"),
		*fakePipelineRun("2", 3, "SUCCESS"),
	})
	view := svm.View(120, 40)

	if strings.Contains(view, cacheSentinel) {
		t.Fatal("stale cached row survived a SetPipelineRun data refresh")
	}
}

// Cached rows were rendered for a specific box width; a resize must not
// reuse them.
func TestViewInvalidatesCacheOnWidthChange(t *testing.T) {
	svm := newCacheTestModel(
		fakePipelineRun("1", 3, "SUCCESS"),
		fakePipelineRun("2", 3, "SUCCESS"),
	)
	svm.View(120, 40)
	poisonStageRow(t, svm, "2", cacheSentinel)

	view := svm.View(80, 40)

	if strings.Contains(view, cacheSentinel) {
		t.Fatal("cached row rendered at old width was reused after resize")
	}
}
