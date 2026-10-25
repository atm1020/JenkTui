package logviewer

// FileReadyMsg is sent when file is ready and line count is known
type FileReadyMsg struct {
	FilePath   string
	LineCount  int
	generation uint64
}

type ScrollType int

const (
	None ScrollType = iota
	Up
	Down
)

type chunkCursorTarget struct {
	line         int // one-based file line, matching SetCursorMsg
	col          int
	ensureColumn bool
}

type chunkLoadRequest struct {
	generation uint64
	session    uint64
	from       int
	to         int
	scrollType ScrollType
	cursor     *chunkCursorTarget
}

// ChunkLoadedMsg is sent when a chunk of lines has been loaded.
type ChunkLoadedMsg struct {
	Lines      []string
	ScrollType ScrollType
	request    *chunkLoadRequest
}

// SearchCompletedMsg is sent when search is complete
type SearchCompletedMsg struct {
	Matches []MatchPosition
	// generation is internal; zero keeps hand-authored test messages and
	// legacy callers valid while asynchronous searches remain stale-safe.
	generation uint64
	session    uint64
}

// loadFailedMsg keeps the viewer's loading animation lifecycle local before
// the app shell turns the failure into its standard error notification.
type loadFailedMsg struct {
	Error            error
	Title            string
	chunkGeneration  uint64
	searchGeneration uint64
	session          uint64
}

// SetCursorMsg requests cursor to jump to specific line. Col is the column
// to bring into view horizontally (0 if unknown/irrelevant).
type SetCursorMsg struct {
	Line int
	Col  int
	Init bool
}

// YankCompletedMsg is sent when the visually-selected line range has been
// read from disk and is ready to copy to the clipboard.
type YankCompletedMsg struct {
	Lines []string
	Err   error
}
