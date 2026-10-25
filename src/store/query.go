package store

import (
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
)

// RunSortField selects which column QueryRuns orders by. Values are matched
// against the allow-list in orderByClauses — never interpolated into SQL.
type RunSortField string

const (
	RunSortDate     RunSortField = "date"
	RunSortDuration RunSortField = "duration"
	RunSortStatus   RunSortField = "status"
)

// SortDirection selects ascending or descending order for QueryRuns.
type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

// RunQuery describes a filtered, ordered view of the runs table. An empty
// JobName, BuildID, or Status means "no filter" for that field.
type RunQuery struct {
	JobName   string
	BuildID   string
	Status    buildstatus.PipelineStatus
	SortBy    RunSortField
	Direction SortDirection
}

// defaultRunQuery is applied when SortBy/Direction don't match an entry in
// orderByClauses (including the zero value of RunQuery).
var defaultOrderBy = orderByClauses[RunSortDate][SortDescending]

// orderByClauses is the fixed allow-list of ORDER BY fragments selectable by
// (SortBy, Direction). SQL identifiers and direction can't be bound as query
// parameters, so callers must select from here rather than building a clause
// from user input.
//
// start_time and duration use 0 as an "unknown" sentinel (queued/running
// builds), not SQL NULL, so a leading `(col = 0) ASC` boolean key pushes
// those rows to the end regardless of direction. created_at DESC, id ASC is
// appended everywhere for deterministic tie-breaking.
var orderByClauses = map[RunSortField]map[SortDirection]string{
	RunSortDate: {
		SortAscending:  `(start_time = 0) ASC, start_time ASC, created_at DESC, id ASC`,
		SortDescending: `(start_time = 0) ASC, start_time DESC, created_at DESC, id ASC`,
	},
	RunSortDuration: {
		SortAscending:  `(duration = 0) ASC, duration ASC, created_at DESC, id ASC`,
		SortDescending: `(duration = 0) ASC, duration DESC, created_at DESC, id ASC`,
	},
	RunSortStatus: {
		SortAscending:  `status ASC, created_at DESC, id ASC`,
		SortDescending: `status DESC, created_at DESC, id ASC`,
	},
}

// orderByClause returns the allow-listed ORDER BY fragment for sortBy and
// direction, falling back to date-descending for unknown or empty values.
func orderByClause(sortBy RunSortField, direction SortDirection) string {
	if dirs, ok := orderByClauses[sortBy]; ok {
		if clause, ok := dirs[direction]; ok {
			return clause
		}
	}
	return defaultOrderBy
}

// likeEscaper escapes LIKE metacharacters so filter input is matched as
// literal text. Replacements are simultaneous (strings.Replacer scans the
// input once), so escaping backslash alongside % and _ can't double-escape.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// likePattern turns literal user input into a substring-match LIKE pattern.
// An empty value becomes "%%", which matches every row.
func likePattern(value string) string {
	return "%" + likeEscaper.Replace(value) + "%"
}

// normalizeBuildIDFilter trims whitespace and strips one optional leading
// '#' so both "42" and "#42" match the same build.
func normalizeBuildIDFilter(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "#")
}
