package build

import (
	"bufio"
	stdctx "context"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/atm1020/gojenkins"
)

// LineRange is an inclusive 1-based line interval [Start, End] in the plain-text
// full console log file.
type LineRange struct {
	Start int
	End   int
}

// LogIndex maps Jenkins wfapi stage IDs, step (flow node) IDs, and outside-of-stage
// regions (pre-stage setup and post-stage errors/summary) to exact 1-based line
// ranges inside the full plain-text console log.
//
// Built in a single streaming pass over Jenkins' core `/logText/progressiveHtml`
// endpoint (which annotates every pipeline line with `nodeId`, `enclosingId`,
// `startId`, and `pipeline-node-<id>` spans — even across interleaved parallel
// branches), with an automatic fallback to plain `/consoleText` `[Pipeline]`
// marker scanning when HTML annotations are unavailable.
type LogIndex struct {
	TotalLines  int
	HasNodeTags bool

	// StageRanges maps a wfapi stage ID to all 1-based line ranges belonging to
	// that stage (including all nested flow nodes, cleanly de-interleaved across
	// concurrent parallel branches).
	StageRanges map[string][]LineRange
	// StageStartLine records the 1-based line where a stage opens (`{ (Stage)`).
	StageStartLine map[string]int

	// StepRanges maps a wfapi flow-node (step) ID to its 1-based line ranges.
	StepRanges map[string][]LineRange
	// StepOutputStart records the first 1-based output line of a step (skipping
	// the `[Pipeline] sh` header when output exists), falling back to its header.
	StepOutputStart map[string]int

	// Outside-of-stage regions: lines whose owning node is not enclosed by any
	// stage (e.g. `Started by user`, `Running on Jenkins`, `End of Pipeline`,
	// uncaught Groovy stacktraces, and `Finished: ...`).
	PreStage  []LineRange
	PostStage []LineRange
	MidOut    []LineRange
}

func newLogIndex() *LogIndex {
	return &LogIndex{
		StageRanges:     make(map[string][]LineRange),
		StageStartLine:  make(map[string]int),
		StepRanges:      make(map[string][]LineRange),
		StepOutputStart: make(map[string]int),
	}
}

// JumpLineForStep returns the best 1-based line in the full console log for a
// step, preferring its actual output over the `[Pipeline]` header line.
func (idx *LogIndex) JumpLineForStep(stepID string) (int, bool) {
	if idx == nil || stepID == "" {
		return 0, false
	}
	if line, ok := idx.StepOutputStart[stepID]; ok && line > 0 {
		return line, true
	}
	if ranges := idx.StepRanges[stepID]; len(ranges) > 0 {
		return ranges[0].Start, true
	}
	return 0, false
}

// JumpLineForStage returns the 1-based line where a stage starts in the full log.
func (idx *LogIndex) JumpLineForStage(stageID string) (int, bool) {
	if idx == nil || stageID == "" {
		return 0, false
	}
	if line, ok := idx.StageStartLine[stageID]; ok && line > 0 {
		return line, true
	}
	if ranges := idx.StageRanges[stageID]; len(ranges) > 0 {
		return ranges[0].Start, true
	}
	return 0, false
}

// JumpLineForPostStage returns the first 1-based line after the last stage
// (useful for jumping directly to post-pipeline errors/stacktraces).
func (idx *LogIndex) JumpLineForPostStage() (int, bool) {
	if idx == nil || len(idx.PostStage) == 0 {
		return 0, false
	}
	return idx.PostStage[0].Start, true
}

// OutsideLineCount reports how many lines sit outside any pipeline stage.
func (idx *LogIndex) OutsideLineCount() int {
	if idx == nil {
		return 0
	}
	return countRanges(idx.PreStage) + countRanges(idx.MidOut) + countRanges(idx.PostStage)
}

func countRanges(ranges []LineRange) int {
	total := 0
	for _, r := range ranges {
		if r.End >= r.Start {
			total += r.End - r.Start + 1
		}
	}
	return total
}

func appendRange(ranges []LineRange, line int) []LineRange {
	if n := len(ranges); n > 0 && ranges[n-1].End+1 == line {
		ranges[n-1].End = line
		return ranges
	}
	return append(ranges, LineRange{Start: line, End: line})
}

type htmlNodeMeta struct {
	enclosingID string
	startID     string
	label       string
}

type spanFrame struct {
	nodeID   string
	isOutput bool
}

// parseProgressiveHTML streams Jenkins' `/logText/progressiveHtml` body, writes
// clean plain-text lines to out, and returns a LogIndex joined against stageIDs.
func parseProgressiveHTML(r io.Reader, out io.Writer, stageIDs map[string]struct{}) (*LogIndex, error) {
	idx := newLogIndex()
	nodes := make(map[string]htmlNodeMeta)
	// Per-line ownership recorded during the pass; resolved to stages once the
	// pass completes so even closing nodes referencing startId resolve cleanly.
	type lineTag struct {
		nodeID   string
		isOutput bool
	}
	var tags []lineTag

	var stack []spanFrame
	activeSpan := func() (string, bool) {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].nodeID != "" {
				return stack[i].nodeID, stack[i].isOutput
			}
		}
		return "", false
	}

	br := bufio.NewReaderSize(r, 64*1024)
	bw := bufio.NewWriterSize(out, 64*1024)
	var lineBuf strings.Builder
	var tagBuf strings.Builder
	inTag := false
	lineNodeID := ""
	lineIsOutput := false
	lineHasOwner := false

	flushLine := func() error {
		plain := html.UnescapeString(lineBuf.String())
		plain = strings.TrimSuffix(plain, "\r")
		if _, err := bw.WriteString(plain); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
		idx.TotalLines++
		owner := lineNodeID
		isOut := lineIsOutput
		if !lineHasOwner {
			owner, isOut = activeSpan()
		}
		tags = append(tags, lineTag{nodeID: owner, isOutput: isOut})

		lineBuf.Reset()
		lineNodeID = ""
		lineIsOutput = false
		lineHasOwner = false
		return nil
	}

	processTag := func(raw string) {
		trimmed := strings.TrimSpace(raw)
		if len(trimmed) < 2 {
			return
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "<span"):
			frame := parseSpanOpenTag(trimmed, nodes)
			if frame.nodeID != "" {
				idx.HasNodeTags = true
			}
			if !strings.HasSuffix(trimmed, "/>") {
				stack = append(stack, frame)
			}
			if frame.nodeID != "" {
				lineNodeID = frame.nodeID
				lineIsOutput = frame.isOutput
				lineHasOwner = true
			}
		case strings.HasPrefix(lower, "</span"):
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				if lineBuf.Len() > 0 {
					if ferr := flushLine(); ferr != nil {
						return nil, ferr
					}
				}
				break
			}
			return nil, err
		}

		if inTag {
			tagBuf.WriteByte(b)
			if b == '>' {
				inTag = false
				processTag(tagBuf.String())
				tagBuf.Reset()
			}
			continue
		}

		if b == '<' {
			inTag = true
			tagBuf.Reset()
			tagBuf.WriteByte(b)
			continue
		}

		if b == '\n' {
			if ferr := flushLine(); ferr != nil {
				return nil, ferr
			}
			continue
		}

		if !lineHasOwner {
			if nid, isOut := activeSpan(); nid != "" {
				lineNodeID = nid
				lineIsOutput = isOut
				lineHasOwner = true
			}
		}
		lineBuf.WriteByte(b)
	}

	if err := bw.Flush(); err != nil {
		return nil, err
	}

	// Build transitive node -> stage mapping using wfapi stage IDs (and labelled
	// stage nodes if stageIDs was empty, e.g. while metadata is still loading).
	stageForNode := make(map[string]string, len(nodes))
	resolveStage := func(nodeID string) string {
		if nodeID == "" {
			return ""
		}
		if cached, ok := stageForNode[nodeID]; ok {
			return cached
		}
		cur := nodeID
		visited := make(map[string]struct{}, 8)
		found := ""
		for cur != "" {
			if _, loop := visited[cur]; loop {
				break
			}
			visited[cur] = struct{}{}
			if _, ok := stageIDs[cur]; ok {
				found = cur
				break
			}
			meta, exists := nodes[cur]
			if !exists {
				break
			}
			if len(stageIDs) == 0 && meta.label != "" && !strings.HasPrefix(meta.label, "Branch: ") {
				found = cur
				break
			}
			// Closing braces like `[Pipeline] }` have startId=<stageID> and no enclosingId.
			if meta.startID != "" && meta.startID != cur {
				if _, ok := stageIDs[meta.startID]; ok {
					found = meta.startID
					break
				}
				if meta.enclosingID == "" {
					cur = meta.startID
					continue
				}
			}
			cur = meta.enclosingID
		}
		stageForNode[nodeID] = found
		return found
	}

	firstStageLine := 0
	lastStageLine := 0
	lineStage := make([]string, len(tags))
	for i, tag := range tags {
		lineNum := i + 1
		st := resolveStage(tag.nodeID)
		lineStage[i] = st
		if st != "" {
			if firstStageLine == 0 {
				firstStageLine = lineNum
			}
			lastStageLine = lineNum
			idx.StageRanges[st] = appendRange(idx.StageRanges[st], lineNum)
			if _, ok := idx.StageStartLine[st]; !ok {
				idx.StageStartLine[st] = lineNum
			}
		}
		if tag.nodeID != "" {
			idx.StepRanges[tag.nodeID] = appendRange(idx.StepRanges[tag.nodeID], lineNum)
			if tag.isOutput {
				if _, ok := idx.StepOutputStart[tag.nodeID]; !ok {
					idx.StepOutputStart[tag.nodeID] = lineNum
				}
			}
		}
	}

	for i, st := range lineStage {
		if st != "" {
			continue
		}
		lineNum := i + 1
		switch {
		case firstStageLine == 0 || lineNum < firstStageLine:
			idx.PreStage = appendRange(idx.PreStage, lineNum)
		case lineNum > lastStageLine:
			idx.PostStage = appendRange(idx.PostStage, lineNum)
		default:
			idx.MidOut = appendRange(idx.MidOut, lineNum)
		}
	}

	return idx, nil
}

func parseSpanOpenTag(tag string, nodes map[string]htmlNodeMeta) spanFrame {
	classVal := extractHTMLAttr(tag, "class")
	if classVal == "" {
		return spanFrame{}
	}
	if strings.Contains(classVal, "pipeline-new-node") {
		nodeID := extractHTMLAttr(tag, "nodeId")
		if nodeID != "" {
			nodes[nodeID] = htmlNodeMeta{
				enclosingID: extractHTMLAttr(tag, "enclosingId"),
				startID:     extractHTMLAttr(tag, "startId"),
				label:       extractHTMLAttr(tag, "label"),
			}
		}
		return spanFrame{nodeID: nodeID, isOutput: false}
	}
	for _, cls := range strings.Fields(classVal) {
		if strings.HasPrefix(cls, "pipeline-node-") {
			nodeID := strings.TrimPrefix(cls, "pipeline-node-")
			if nodeID != "" {
				return spanFrame{nodeID: nodeID, isOutput: true}
			}
		}
	}
	return spanFrame{}
}

func extractHTMLAttr(tag, name string) string {
	lowerTag := strings.ToLower(tag)
	needle := strings.ToLower(name) + "="
	pos := 0
	for {
		i := strings.Index(lowerTag[pos:], needle)
		if i < 0 {
			return ""
		}
		abs := pos + i
		if abs > 0 {
			prev := lowerTag[abs-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' {
				pos = abs + len(needle)
				continue
			}
		}
		valStart := abs + len(needle)
		if valStart >= len(tag) {
			return ""
		}
		quote := tag[valStart]
		if quote == '"' || quote == '\'' {
			end := strings.IndexByte(tag[valStart+1:], quote)
			if end < 0 {
				return ""
			}
			return html.UnescapeString(tag[valStart+1 : valStart+1+end])
		}
		end := strings.IndexAny(tag[valStart:], " \t\n\r>")
		if end < 0 {
			return html.UnescapeString(tag[valStart:])
		}
		return html.UnescapeString(tag[valStart : valStart+end])
	}
}

// indexPlainConsoleLog builds a best-effort LogIndex from plain `/consoleText`
// using `[Pipeline] { (<StageName>)` and `[Pipeline] // stage` markers. Used
// as a graceful fallback when `/logText/progressiveHtml` is unavailable.
func indexPlainConsoleLog(filePath string, stages []gojenkins.PipelineNode) (*LogIndex, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	nameToIDs := make(map[string][]string, len(stages))
	for _, st := range stages {
		nameToIDs[st.Name] = append(nameToIDs[st.Name], st.ID)
	}
	nameCursor := make(map[string]int, len(stages))

	idx := newLogIndex()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var stageStack []string
	var lineStages []string
	firstStageLine := 0
	lastStageLine := 0

	for scanner.Scan() {
		idx.TotalLines++
		lineNum := idx.TotalLines
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[Pipeline] { (") && strings.HasSuffix(line, ")") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "[Pipeline] { ("), ")")
			if !strings.HasPrefix(name, "Branch: ") {
				if ids := nameToIDs[name]; len(ids) > 0 {
					c := nameCursor[name]
					if c >= len(ids) {
						c = len(ids) - 1
					}
					stageID := ids[c]
					nameCursor[name] = c + 1
					stageStack = append(stageStack, stageID)
					if _, ok := idx.StageStartLine[stageID]; !ok {
						idx.StageStartLine[stageID] = lineNum
					}
				}
			}
		}

		curStage := ""
		if len(stageStack) > 0 {
			curStage = stageStack[len(stageStack)-1]
			if firstStageLine == 0 {
				firstStageLine = lineNum
			}
			lastStageLine = lineNum
			idx.StageRanges[curStage] = appendRange(idx.StageRanges[curStage], lineNum)
		}
		lineStages = append(lineStages, curStage)

		if line == "[Pipeline] // stage" && len(stageStack) > 0 {
			stageStack = stageStack[:len(stageStack)-1]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for i, st := range lineStages {
		if st != "" {
			continue
		}
		lineNum := i + 1
		switch {
		case firstStageLine == 0 || lineNum < firstStageLine:
			idx.PreStage = appendRange(idx.PreStage, lineNum)
		case lineNum > lastStageLine:
			idx.PostStage = appendRange(idx.PostStage, lineNum)
		default:
			idx.MidOut = appendRange(idx.MidOut, lineNum)
		}
	}

	return idx, nil
}

// downloadAndIndexFullLog fetches `/logText/progressiveHtml?start=0` (yielding
// both the plain-text full log file and exact per-node/per-stage/outside line
// index in one round-trip), falling back to `/consoleText` if unavailable.
func downloadAndIndexFullLog(
	ctx stdctx.Context,
	run *gojenkins.PipelineRun,
	cacheDir string,
) (string, *LogIndex, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", nil, fmt.Errorf("failed to prepare log cache directory: %w", err)
	}
	sanitized := strings.ReplaceAll(run.Base, "/", "_")
	if sanitized == "" {
		sanitized = "build_" + safePathComponent(run.ID)
	}
	filePath := filepath.Join(cacheDir, sanitized+".log")

	stageIDs := make(map[string]struct{}, len(run.Stages))
	for _, st := range run.Stages {
		if st.ID != "" {
			stageIDs[st.ID] = struct{}{}
		}
	}

	if idx, ok, err := tryFetchProgressiveHTML(ctx, run, filePath, stageIDs); ok {
		return filePath, idx, nil
	} else if err != nil && ctx.Err() != nil {
		return "", nil, ctx.Err()
	}

	// Fallback: classic plain consoleText + [Pipeline] marker indexing.
	plainPath, err := downloadFullConsoleLog(ctx, run, cacheDir)
	if err != nil {
		return "", nil, err
	}
	idx, err := indexPlainConsoleLog(plainPath, run.Stages)
	if err != nil {
		return plainPath, nil, nil
	}
	return plainPath, idx, nil
}

func tryFetchProgressiveHTML(
	ctx stdctx.Context,
	run *gojenkins.PipelineRun,
	filePath string,
	stageIDs map[string]struct{},
) (*LogIndex, bool, error) {
	if run == nil || run.Job == nil || run.Job.Jenkins == nil || run.Job.Jenkins.Requester == nil {
		return nil, false, nil
	}
	jenkins := run.Job.Jenkins
	buildBase := strings.TrimSpace(run.Base)
	if buildBase == "" && run.Job.Base != "" && run.ID != "" {
		buildBase = strings.TrimRight(run.Job.Base, "/") + "/" + run.ID
	}
	server := strings.TrimRight(strings.TrimSpace(jenkins.Server), "/")
	if server != "" && strings.HasPrefix(buildBase, server) {
		buildBase = strings.TrimPrefix(buildBase, server)
	}
	buildBase = strings.TrimRight(buildBase, "/")
	if buildBase == "" {
		return nil, false, nil
	}
	if !strings.HasPrefix(buildBase, "/") {
		buildBase = "/" + buildBase
	}

	ar := gojenkins.NewAPIRequest(http.MethodGet, buildBase+"/logText", nil)
	ar.Suffix = "progressiveHtml"
	resp, err := jenkins.Requester.Do(ctx, ar, nil, map[string]string{"start": "0"})
	if err != nil {
		return nil, false, err
	}
	if resp == nil {
		return nil, false, fmt.Errorf("empty progressiveHtml response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, false, fmt.Errorf("progressiveHtml status %d", resp.StatusCode)
	}

	tmpPath := filePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, false, err
	}
	idx, parseErr := parseProgressiveHTML(resp.Body, f, stageIDs)
	closeErr := f.Close()
	if parseErr != nil {
		_ = os.Remove(tmpPath)
		return nil, false, parseErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, false, closeErr
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return nil, false, err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, false, err
	}
	return idx, true, nil
}

// extractLogRanges copies only the lines falling inside ranges (in ascending
// order) from fullLogPath into outPath. Used to build instant, zero-HTTP scoped
// views for a single stage (de-interleaved across parallel branches) or step.
func extractLogRanges(fullLogPath string, ranges []LineRange, outPath string) (int, error) {
	if len(ranges) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return 0, err
	}
	src, err := os.Open(fullLogPath)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	dst, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(dst, 32*1024)

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	rIdx := 0
	lineNum := 0
	written := 0
	for scanner.Scan() && rIdx < len(ranges) {
		lineNum++
		for rIdx < len(ranges) && lineNum > ranges[rIdx].End {
			rIdx++
		}
		if rIdx >= len(ranges) {
			break
		}
		if lineNum >= ranges[rIdx].Start && lineNum <= ranges[rIdx].End {
			if _, err := bw.WriteString(scanner.Text()); err != nil {
				_ = dst.Close()
				return 0, err
			}
			if err := bw.WriteByte('\n'); err != nil {
				_ = dst.Close()
				return 0, err
			}
			written++
		}
	}
	if err := scanner.Err(); err != nil {
		_ = dst.Close()
		return 0, err
	}
	if err := bw.Flush(); err != nil {
		_ = dst.Close()
		return 0, err
	}
	if err := dst.Close(); err != nil {
		return 0, err
	}
	if err := os.Chmod(outPath, 0o600); err != nil {
		return 0, err
	}
	return written, nil
}

// extractOutsideStageLog writes Pre-stage, between-stage, and Post-stage lines
// from fullLogPath into outPath with clear section headers referencing their
// original line numbers in the full console log.
func extractOutsideStageLog(fullLogPath string, idx *LogIndex, outPath string) (int, error) {
	if idx == nil || idx.OutsideLineCount() == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return 0, err
	}
	src, err := os.Open(fullLogPath)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	type section struct {
		label  string
		ranges []LineRange
	}
	var sections []section
	if len(idx.PreStage) > 0 {
		sections = append(sections, section{label: "Pre-stage", ranges: idx.PreStage})
	}
	if len(idx.MidOut) > 0 {
		sections = append(sections, section{label: "Between stages", ranges: idx.MidOut})
	}
	if len(idx.PostStage) > 0 {
		sections = append(sections, section{label: "Post-stage", ranges: idx.PostStage})
	}

	// Flatten into ordered range entries with section headers.
	type taggedRange struct {
		LineRange
		header string
	}
	var ordered []taggedRange
	for _, sec := range sections {
		first := sec.ranges[0].Start
		last := sec.ranges[len(sec.ranges)-1].End
		header := fmt.Sprintf("── %s (lines %d-%d) ──────────────────────────────────", sec.label, first, last)
		for i, r := range sec.ranges {
			h := ""
			if i == 0 {
				h = header
			}
			ordered = append(ordered, taggedRange{LineRange: r, header: h})
		}
	}

	dst, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	bw := bufio.NewWriterSize(dst, 32*1024)

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	rIdx := 0
	lineNum := 0
	written := 0
	headerPrinted := false
	for scanner.Scan() && rIdx < len(ordered) {
		lineNum++
		for rIdx < len(ordered) && lineNum > ordered[rIdx].End {
			rIdx++
			headerPrinted = false
		}
		if rIdx >= len(ordered) {
			break
		}
		if lineNum >= ordered[rIdx].Start && lineNum <= ordered[rIdx].End {
			if ordered[rIdx].header != "" && !headerPrinted {
				if written > 0 {
					_ = bw.WriteByte('\n')
					written++
				}
				_, _ = bw.WriteString(ordered[rIdx].header + "\n")
				written++
				headerPrinted = true
			}
			if _, err := bw.WriteString(scanner.Text()); err != nil {
				_ = dst.Close()
				return 0, err
			}
			if err := bw.WriteByte('\n'); err != nil {
				_ = dst.Close()
				return 0, err
			}
			written++
		}
	}
	if err := scanner.Err(); err != nil {
		_ = dst.Close()
		return 0, err
	}
	if err := bw.Flush(); err != nil {
		_ = dst.Close()
		return 0, err
	}
	if err := dst.Close(); err != nil {
		return 0, err
	}
	if err := os.Chmod(outPath, 0o600); err != nil {
		return 0, err
	}
	return written, nil
}
