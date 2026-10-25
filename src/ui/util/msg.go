package util

import (
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/atm1020/gojenkins"
)

type OpenDialogMsg struct {
	Dialog JenkTuiDialogModel
}

type CloseDialogMsg struct{}

// OpenJobFolderMsg opens a container page (Folder, Organization Folder, or
// MultiBranch Pipeline). Target carries what to show before anything loads;
// Job is a reference the page issues its own child query through, and its
// raw response is empty when the container was opened from a known target.
type OpenJobFolderMsg struct {
	Target jobref.Target
	Job    *gojenkins.Job
}

type OpenJobComponentMsg struct {
	Job *gojenkins.Job
}

// OpenJobTargetMsg asks the app to open a navigation target. Emitting this
// rather than resolving locally keeps routing decisions in one place — see
// HandleJobTarget.
type OpenJobTargetMsg struct {
	Target jobref.Target
}

type BrowserOpenMsg struct {
	Url string
}

type ErrorMsg struct {
	Error error
	Title string
}

// NotiType represents the type of notification
type NotiType int

const (
	NotiInfo    NotiType = iota // Default - blue/info background
	NotiWarning                 // Orange/warning background
	NotiError                   // Red/error background
	NotiSuccess                 // Green/success background
	NotiSkipped                 // Grey/skipped background
)

type FooterNotiMsg struct {
	Message string
	Type    NotiType // Default is NotiInfo (0)
}

type FooterUrlMsg struct {
	Url string
}

type ThemeChangedMsg struct {
	ThemeType styles.ThemeType
}

// ThemePreviewMsg is sent when the user previews a theme (up/down in theme selector)
// without confirming. Caches should be cleared but the dialog stays open.
type ThemePreviewMsg struct{}

// RunTriggeredMsg is emitted when the user triggers a build (e.g., via replay).
type RunTriggeredMsg struct {
	Job      *gojenkins.Job
	IntentID string
	// QueueID is the Jenkins queue item. nil means the trigger had none (replay).
	QueueID *int64
	Tracked bool
	// SourceViewID is set when the originating view wants to handle the trigger
	// result itself. A blank value leaves the existing notification-only flow.
	SourceViewID string
}
