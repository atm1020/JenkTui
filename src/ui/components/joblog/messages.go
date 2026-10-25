package joblog

// DownloadCompleteMsg is sent when log download is complete
type DownloadCompleteMsg struct {
	Generation int
	FilePath   string
	LineCount  int
	Error      error
}
