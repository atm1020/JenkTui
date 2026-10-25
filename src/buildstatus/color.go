package buildstatus

import "strings"

// JobColor is the classic Jenkins job color returned by job/folder APIs.
type JobColor string

const (
	JobColorBlue     JobColor = "blue"
	JobColorRed      JobColor = "red"
	JobColorYellow   JobColor = "yellow"
	JobColorAborted  JobColor = "aborted"
	JobColorNotBuilt JobColor = "notbuilt"
	JobColorDisabled JobColor = "disabled"
	JobColorGrey     JobColor = "grey"
)

// Jenkins appends "_anime" to a job's base color to mark it as currently building.
const animatedColorSuffix = "_anime"

type jobColorMeaning struct {
	label   string
	outcome Outcome
}

var jobColorMeanings = map[JobColor]jobColorMeaning{
	JobColorBlue:     {label: "Success", outcome: OutcomeSuccess},
	JobColorRed:      {label: "Failed", outcome: OutcomeFailure},
	JobColorYellow:   {label: "Unstable", outcome: OutcomeUnstable},
	JobColorAborted:  {label: "Aborted", outcome: OutcomeAborted},
	JobColorNotBuilt: {label: "Not built", outcome: OutcomeNotBuilt},
	JobColorDisabled: {label: "Disabled", outcome: OutcomeUnknown},
	JobColorGrey:     {label: "Unknown", outcome: OutcomeUnknown},
}

func (c JobColor) parts() (base JobColor, animated bool) {
	normalized := strings.ToLower(strings.TrimSpace(string(c)))
	baseValue, animated := strings.CutSuffix(normalized, animatedColorSuffix)
	return JobColor(baseValue), animated
}

// Activity returns the current job activity encoded by the classic color.
// Animated colors are running regardless of their last-known outcome.
func (c JobColor) Activity() Activity {
	base, animated := c.parts()
	if animated {
		return ActivityRunning
	}
	if base == JobColorDisabled {
		return ActivityDisabled
	}
	if _, ok := jobColorMeanings[base]; ok {
		return ActivityIdle
	}
	return ActivityUnknown
}

// IsRunning reports whether the classic color carries Jenkins' animation
// suffix.
func (c JobColor) IsRunning() bool {
	return c.Activity() == ActivityRunning
}

// Outcome returns the result encoded by the base color, independently of an
// animation suffix.
func (c JobColor) Outcome() Outcome {
	base, _ := c.parts()
	return jobColorMeanings[base].outcome
}

// Label returns user-facing text for the color. While a job is animated its
// current activity is more useful than its previous/partial outcome.
func (c JobColor) Label() string {
	base, animated := c.parts()
	if animated {
		return "Running"
	}
	if meaning, ok := jobColorMeanings[base]; ok {
		return meaning.label
	}
	return "Unknown"
}
