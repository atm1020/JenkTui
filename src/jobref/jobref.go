// Package jobref centralizes Jenkins job URL helpers used across the TUI
// and the poll loop. It is a leaf package with no project-internal imports
// so it can be consumed from any layer (notably src/poll, which cannot
// import src/ui/util because of the poll → ui/util → context → poll cycle).
package jobref

import (
	"net/url"
	"regexp"
	"strings"
)

// Jenkins job _class suffixes used to tell container jobs (browsable, no
// runs of their own) apart from buildable pipeline jobs.
const (
	classFolder      = "Folder"
	classMultiBranch = "WorkflowMultiBranchProject"
	classWorkflowJob = "WorkflowJob"
)

// IsFolder reports whether class is a plain Jenkins Folder.
func IsFolder(class string) bool {
	return strings.HasSuffix(class, classFolder)
}

// IsMultiBranch reports whether class is a MultiBranch Pipeline project.
func IsMultiBranch(class string) bool {
	return strings.HasSuffix(class, classMultiBranch)
}

// IsPipeline reports whether class is a buildable Jenkins Pipeline job
// (including an individual branch of a MultiBranch Pipeline, which is an
// ordinary WorkflowJob under the hood).
func IsPipeline(class string) bool {
	return strings.HasSuffix(class, classWorkflowJob)
}

// Kind is how the TUI routes a job: which page opens, and whether opening it
// needs a discovery request first. It is deliberately coarser than the raw
// Jenkins _class — everything the router cares about is "can I browse it,
// can I run it, or do I not know yet".
type Kind int

const (
	// KindUnknown means the classification is not established (e.g. a URL
	// from global search, or a job kind jenktui has no page for). Opening
	// one requires asking Jenkins what it is first.
	KindUnknown Kind = iota
	KindPipeline
	KindFolder
	KindMultiBranch
)

// KindFromClass translates a raw Jenkins _class into a routing kind. This is
// the single seam where class strings turn into routing decisions; nothing
// downstream should re-inspect the class to decide where to navigate.
func KindFromClass(class string) Kind {
	switch {
	case IsMultiBranch(class):
		return KindMultiBranch
	case IsFolder(class):
		return KindFolder
	case IsPipeline(class):
		return KindPipeline
	default:
		return KindUnknown
	}
}

// IsContainer reports whether k opens as a browsable listing of children
// (a Folder, an Organization Folder, or a MultiBranch Pipeline) rather than
// as a job detail page.
func (k Kind) IsContainer() bool {
	return k == KindFolder || k == KindMultiBranch
}

// FavoriteKind renders k as the value stored in the favorites "kind" column.
// Pipelines and unclassified jobs share the "job" bucket, which is all the
// favorites view distinguishes.
func (k Kind) FavoriteKind() string {
	switch k {
	case KindMultiBranch:
		return "multibranch"
	case KindFolder:
		return "folder"
	default:
		return "job"
	}
}

// KindFromFavoriteKind is the inverse of FavoriteKind, so reopening a
// favorited container skips the discovery request. "job" maps back to
// KindUnknown rather than KindPipeline: that bucket also holds freestyle
// and other non-pipeline jobs, and guessing wrong would route them to a
// page that cannot render them.
func KindFromFavoriteKind(kind string) Kind {
	switch kind {
	case "multibranch":
		return KindMultiBranch
	case "folder":
		return KindFolder
	default:
		return KindUnknown
	}
}

// Target is a navigation destination: where to go, what to call it before
// anything is loaded, and what it is (when that is already known). Passing
// this around instead of a bare URL is what lets a known container open
// straight into its page instead of being re-discovered.
type Target struct {
	URL  string
	Name string
	Kind Kind
}

// jobNameSep separates folder segments in a human-readable job name.
// U+EAB6 is the Nerd Font glyph that renders as a chevron in the TUI.
const jobNameSep = "  "

// ToAbsolute returns url unchanged if it already carries an http(s)://
// scheme, otherwise prepends server. server is expected to be the value
// of *gojenkins.Jenkins.Server, which gojenkins.CreateJenkins stores
// without a trailing slash. Used at every ingress point that may receive
// a relative URL — primarily gojenkins.Suggestion.URL from global search.
func ToAbsolute(server, url string) string {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}
	if server == "" {
		return url
	}
	return server + url
}

// BuildURL returns the page URL for buildID below jobURL. Jenkins job URLs
// are observed both with and without a trailing slash, so the join is
// normalized here rather than left to individual views.
func BuildURL(jobURL, buildID string) string {
	return strings.TrimRight(jobURL, "/") + "/" + strings.TrimLeft(buildID, "/")
}

// ToDisplayName returns a human-readable label for a Jenkins job URL,
// e.g. "http://jenkins/job/folder/job/my-pipeline/" → "folder  my-pipeline".
func ToDisplayName(jobURL string) string {
	jobURL = strings.TrimPrefix(jobURL, "http://")
	jobURL = strings.TrimPrefix(jobURL, "https://")
	jobURL = strings.TrimSuffix(jobURL, "/")
	split := strings.SplitN(jobURL, "/", 2)
	if len(split) <= 1 {
		return jobURL
	}
	var parts []string
	for p := range strings.SplitSeq(split[1], "/") {
		if p == "" || p == "job" {
			continue
		}
		parts = append(parts, DecodeName(p))
	}
	return strings.Join(parts, jobNameSep)
}

// DecodeName undoes Jenkins' percent-encoding of characters that would
// otherwise collide with the URL path separator -- most commonly a branch
// name containing "/" (e.g. a multibranch pipeline's "merge-queue/PR-48"),
// which Jenkins reports as the literal job name "merge-queue%2FPR-48" so it
// stays a single path segment. Falls back to the raw name if it isn't valid
// percent-encoding, so an unrelated literal "%" in a name still displays.
func DecodeName(name string) string {
	decoded, err := url.PathUnescape(name)
	if err != nil {
		return name
	}
	return decoded
}

var jobURLPattern = regexp.MustCompile(`(^|/)job/[^/]+(/job/[^/]+)*/?$`)

// IsJobURL reports whether rawURL is the canonical page URL of a Jenkins Item
// (a job or folder). Requiring the entire suffix to be /job/name pairs avoids
// treating an action below a job, such as /job/name/configure, as the job itself.
func IsJobURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && jobURLPattern.MatchString(parsed.EscapedPath())
}

// ToIDAndParents parses a Jenkins job URL or path into the job ID and its
// parent folder IDs, suitable for gojenkins.GetJob(ctx, id, parentIDs...).
// Robust to both absolute ("http://…/job/X/job/Y/") and relative
// ("/job/X/job/Y/") forms by skipping everything before the first "/job/"
// segment so scheme + host can't leak into the parent list.
func ToIDAndParents(url string) (string, []string) {
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	if i := strings.Index(url, "/job/"); i >= 0 {
		url = url[i:]
	}

	var parts []string
	for v := range strings.SplitSeq(url, "/") {
		if v == "" || v == "job" {
			continue
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "", nil
	}
	var parents []string
	if len(parts) > 1 {
		parents = parts[:len(parts)-1]
	}
	return parts[len(parts)-1], parents
}
