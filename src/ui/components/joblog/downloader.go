package joblog

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/atm1020/jenktui/src/ui/components/logviewer"

	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

// Downloader handles downloading Jenkins logs
type Downloader struct {
	pipelineRun *gojenkins.PipelineRun
	cacheDir    string
}

// NewDownloader creates a new downloader
func NewDownloader(run *gojenkins.PipelineRun, cacheDir string) *Downloader {
	return &Downloader{
		pipelineRun: run,
		cacheDir:    cacheDir,
	}
}

// GenerateFilePath generates the cache file path for this log
func (d *Downloader) GenerateFilePath() string {
	sanitized := strings.ReplaceAll(d.pipelineRun.Base, "/", "_")
	return fmt.Sprintf("%s/%s.log", d.cacheDir, sanitized)
}

// Download downloads the log file and returns the path and line count.
func (d *Downloader) Download(ctx context.Context) (filePath string, lineCount int, err error) {
	filePath = d.GenerateFilePath()

	log.Info("Downloading log", "cacheDir", d.cacheDir, "fileName", filePath)

	file, err := d.pipelineRun.SaveTextLog(ctx, filePath)
	if err != nil {
		return "", 0, fmt.Errorf("download failed: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", 0, fmt.Errorf("failed to close downloaded file: %w", err)
	}
	if err := os.Chmod(filePath, 0o600); err != nil {
		return "", 0, fmt.Errorf("failed to secure downloaded file: %w", err)
	}

	count, err := logviewer.NewLoader(filePath).ScanLines()
	if err != nil {
		return "", 0, fmt.Errorf("failed to scan log file: %w", err)
	}

	log.Info("Download complete", "lines", count, "path", filePath)
	return filePath, count, nil
}
