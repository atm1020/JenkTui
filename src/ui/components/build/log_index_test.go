package build

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atm1020/gojenkins"
)

const sampleParallelProgressiveHTML = `Started by user <a href='/user/admin'>admin</a>
[Pipeline] Start of Pipeline
<span class="pipeline-new-node" nodeId="3" startId="3" enclosingId="2">[Pipeline] node
</span><span class="pipeline-node-3">Running on <a href='/computer/'>Jenkins</a> in /workspace
</span><span class="pipeline-new-node" nodeId="6" startId="6" enclosingId="5" label="Install">[Pipeline] { (Install)
</span><span class="pipeline-new-node" nodeId="7" enclosingId="6">[Pipeline] echo
</span><span class="pipeline-node-7">Installing deps &amp; tools...
</span><span class="pipeline-new-node" nodeId="9" startId="6">[Pipeline] }
</span><span class="pipeline-new-node" nodeId="20" startId="20" enclosingId="19" label="Unit">[Pipeline] { (Unit)
</span><span class="pipeline-new-node" nodeId="22" startId="22" enclosingId="21" label="Integration">[Pipeline] { (Integration)
</span><span class="pipeline-new-node" nodeId="26" enclosingId="20">[Pipeline] sh
</span><span class="pipeline-new-node" nodeId="28" enclosingId="22">[Pipeline] sh
</span><span class="pipeline-node-26">+ unit-test-1
+ unit-test-2
</span><span class="pipeline-node-28">+ integration-test-1
</span><span class="pipeline-node-26">+ unit-done
</span><span class="pipeline-new-node" nodeId="36" startId="20">[Pipeline] }
</span><span class="pipeline-new-node" nodeId="40" startId="22">[Pipeline] }
</span><span class="pipeline-new-node" nodeId="53" startId="2">[Pipeline] End of Pipeline
</span>java.lang.NoSuchMethodError: No such DSL method 'errors'
Finished: FAILURE
`

func TestParseProgressiveHTMLMapsParallelStagesStepsAndOutsideLines(t *testing.T) {
	stageIDs := map[string]struct{}{
		"6":  {},
		"20": {},
		"22": {},
	}
	var plain bytes.Buffer
	idx, err := parseProgressiveHTML(strings.NewReader(sampleParallelProgressiveHTML), &plain, stageIDs)
	if err != nil {
		t.Fatalf("parseProgressiveHTML error = %v", err)
	}
	if !idx.HasNodeTags {
		t.Fatal("expected HasNodeTags = true")
	}

	plainStr := plain.String()
	if strings.Contains(plainStr, "<span") || strings.Contains(plainStr, "<a ") {
		t.Fatalf("HTML tags leaked into plain log:\n%s", plainStr)
	}
	if !strings.Contains(plainStr, "Installing deps & tools...") {
		t.Fatalf("HTML entity was not unescaped:\n%s", plainStr)
	}

	// Verify stage jump lines and step output jump lines.
	if line, ok := idx.JumpLineForStage("22"); !ok || line != 10 {
		t.Fatalf("JumpLineForStage(Integration=22) = (%d, %v), want (10, true)", line, ok)
	}
	if line, ok := idx.JumpLineForStep("26"); !ok || line != 13 {
		t.Fatalf("JumpLineForStep(Unit sh=26) = (%d, %v), want (13, true) [first output line]", line, ok)
	}
	if line, ok := idx.JumpLineForStep("28"); !ok || line != 15 {
		t.Fatalf("JumpLineForStep(Integration sh=28) = (%d, %v), want (15, true)", line, ok)
	}
	if line, ok := idx.JumpLineForPostStage(); !ok || line != 19 {
		t.Fatalf("JumpLineForPostStage() = (%d, %v), want (19, true)", line, ok)
	}

	// Verify parallel stage extraction cleanly de-interleaves Unit (20) from Integration (22).
	dir := t.TempDir()
	fullPath := filepath.Join(dir, "full.log")
	if err := os.WriteFile(fullPath, plain.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	unitPath := filepath.Join(dir, "unit.log")
	n, err := extractLogRanges(fullPath, idx.StageRanges["20"], unitPath)
	if err != nil || n == 0 {
		t.Fatalf("extractLogRanges(Unit) = (%d, %v)", n, err)
	}
	unitBytes, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	unitText := string(unitBytes)
	if !strings.Contains(unitText, "+ unit-test-1") || !strings.Contains(unitText, "+ unit-done") {
		t.Fatalf("Unit extracted log missing Unit lines:\n%s", unitText)
	}
	if strings.Contains(unitText, "+ integration-test-1") || strings.Contains(unitText, "NoSuchMethodError") {
		t.Fatalf("Unit extracted log leaked Integration or Post-stage lines:\n%s", unitText)
	}

	// Verify Outside-of-stage extraction captures both Pre-stage and Post-stage errors.
	outsidePath := filepath.Join(dir, "outside.log")
	n, err = extractOutsideStageLog(fullPath, idx, outsidePath)
	if err != nil || n == 0 {
		t.Fatalf("extractOutsideStageLog = (%d, %v)", n, err)
	}
	outsideBytes, err := os.ReadFile(outsidePath)
	if err != nil {
		t.Fatal(err)
	}
	outsideText := string(outsideBytes)
	for _, want := range []string{"Pre-stage", "Started by user admin", "Running on Jenkins", "Post-stage", "NoSuchMethodError", "Finished: FAILURE"} {
		if !strings.Contains(outsideText, want) {
			t.Fatalf("outside-stage log missing %q:\n%s", want, outsideText)
		}
	}
	if strings.Contains(outsideText, "+ unit-test-1") || strings.Contains(outsideText, "+ integration-test-1") {
		t.Fatalf("outside-stage log leaked in-stage output:\n%s", outsideText)
	}
}

func TestIndexPlainConsoleLogFallbackMapsSequentialStagesAndOutside(t *testing.T) {
	content := strings.Join([]string{
		"Started by user admin",
		"[Pipeline] Start of Pipeline",
		"[Pipeline] stage",
		"[Pipeline] { (Checkout)",
		"[Pipeline] echo",
		"Checking out...",
		"[Pipeline] }",
		"[Pipeline] // stage",
		"[Pipeline] stage",
		"[Pipeline] { (Build)",
		"Compiling...",
		"[Pipeline] }",
		"[Pipeline] // stage",
		"[Pipeline] End of Pipeline",
		"Finished: SUCCESS",
	}, "\n") + "\n"

	path := filepath.Join(t.TempDir(), "plain.log")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	stages := []gojenkins.PipelineNode{
		{ID: "6", Name: "Checkout"},
		{ID: "12", Name: "Build"},
	}

	idx, err := indexPlainConsoleLog(path, stages)
	if err != nil {
		t.Fatalf("indexPlainConsoleLog error = %v", err)
	}
	if line, ok := idx.JumpLineForStage("6"); !ok || line != 4 {
		t.Fatalf("Checkout start line = (%d, %v), want (4, true)", line, ok)
	}
	if line, ok := idx.JumpLineForStage("12"); !ok || line != 10 {
		t.Fatalf("Build start line = (%d, %v), want (10, true)", line, ok)
	}
	if len(idx.PreStage) == 0 || len(idx.PostStage) == 0 {
		t.Fatalf("expected non-empty pre/post stage ranges, got pre=%v post=%v", idx.PreStage, idx.PostStage)
	}
}
