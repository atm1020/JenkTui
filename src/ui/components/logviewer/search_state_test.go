// =============================================================================
// GO TESTING FRAMEWORK — HOW IT WORKS
// =============================================================================
//
// RULES GO ENFORCES AUTOMATICALLY:
//   1. File must end in _test.go          → go test only compiles these files
//   2. Package must match the code file   → "package logviewer" here
//   3. Test functions must start with "Test" and accept *testing.T
//   4. No main() needed — go test is the entry point
//
// HOW TO RUN TESTS (from project root):
//
//   go test ./...                              run all tests in the project
//   go test ./src/ui/components/logviewer/... run tests in this package only
//   go test ./... -v                           verbose: print each test name + PASS/FAIL
//   go test ./... -run TestNextMatch           run only tests matching that name (regex)
//   go test ./... -run TestNextMatch/advance   run a specific sub-test by name
//   go test ./... -count=1                     disable test result cache (force re-run)
//   go test ./... -race                        enable race condition detector
//
// WHAT *testing.T GIVES YOU:
//
//   t.Errorf("msg %d", val)   report failure, keep running the test
//   t.Fatalf("msg %d", val)   report failure, stop the test immediately
//   t.Logf("msg %d", val)     print a message (only shown when -v or test fails)
//   t.Skip("reason")          skip this test (shown as 'S' in output)
//   t.Helper()                mark caller as helper so error line points to caller
//
// TABLE-DRIVEN TESTS (the Go standard pattern):
//
//   Instead of one function per scenario, define a slice of structs (the "table"),
//   loop over them, and use t.Run() for each row. Benefits:
//     - Add new cases by adding one struct literal — no new function needed
//     - Each case gets its own name in the output: TestFoo/my_case_name
//     - A failure in one case does NOT stop the others
//
// =============================================================================

package logviewer

import "testing"

// linesOf builds one MatchPosition per given line number, all at Col 0 — a
// shorthand for tests that don't care about the column.
func linesOf(lines ...int) []MatchPosition {
	matches := make([]MatchPosition, len(lines))
	for i, line := range lines {
		matches[i] = MatchPosition{Line: line, Col: 0}
	}
	return matches
}

// TestNewSearchState verifies that the constructor sets the correct initial state.
//
// Pattern: each struct in the slice is one test case ("table row").
// Fields starting with "want" are the expected values we assert against.
func TestNewSearchState(t *testing.T) {
	// Define the table — a slice of anonymous structs.
	// Each struct holds inputs and the expected outputs for one scenario.
	tests := []struct {
		name            string          // shown in output as TestNewSearchState/<name>
		matches         []MatchPosition // input to NewSearchState
		wantTotal       int             // expected TotalMatches()
		wantCurrentIdx  int             // expected CurrentIndex()
		wantCurrentLine int             // expected CurrentLine()
	}{
		{
			// Edge case: empty input. CurrentLine() should return 0, not panic.
			name:            "no matches",
			matches:         linesOf(),
			wantTotal:       0,
			wantCurrentIdx:  0,
			wantCurrentLine: 0,
		},
		{
			// Happy path: single item. Index starts at 0, so CurrentLine = matches[0].Line.
			name:            "single match",
			matches:         linesOf(42),
			wantTotal:       1,
			wantCurrentIdx:  0,
			wantCurrentLine: 42,
		},
		{
			// Happy path: multiple items. Index starts at 0, so CurrentLine = first element.
			name:            "multiple matches",
			matches:         linesOf(5, 10, 20),
			wantTotal:       3,
			wantCurrentIdx:  0,
			wantCurrentLine: 5,
		},
	}

	// Loop over every row in the table.
	// tt is the current test case (convention: "tt" = "table test").
	for _, tt := range tests {
		// t.Run creates a sub-test with its own pass/fail tracking.
		// The second argument is a closure; the inner `t` shadows the outer one.
		t.Run(tt.name, func(t *testing.T) {
			// ARRANGE: set up the object under test using the input from this row.
			s := NewSearchState(tt.matches)

			// ASSERT: compare actual vs expected.
			// t.Errorf marks the test as failed but continues — all three checks run.
			if s.TotalMatches() != tt.wantTotal {
				t.Errorf("TotalMatches() = %d, want %d", s.TotalMatches(), tt.wantTotal)
			}
			if s.CurrentIndex() != tt.wantCurrentIdx {
				t.Errorf("CurrentIndex() = %d, want %d", s.CurrentIndex(), tt.wantCurrentIdx)
			}
			if s.CurrentLine() != tt.wantCurrentLine {
				t.Errorf("CurrentLine() = %d, want %d", s.CurrentLine(), tt.wantCurrentLine)
			}
		})
	}
}

// TestNextMatch verifies forward navigation through search results.
//
// The "calls" field lets us simulate pressing 'n' multiple times without
// duplicating the loop logic in every test case.
func TestNextMatch(t *testing.T) {
	tests := []struct {
		name     string
		matches  []MatchPosition
		calls    int // how many times to call NextMatch
		wantLine int
		wantIdx  int
	}{
		{
			// Edge case: calling Next on an empty state should be a no-op.
			name:     "no matches does nothing",
			matches:  linesOf(),
			calls:    1,
			wantLine: 0,
			wantIdx:  0,
		},
		{
			// Happy path: one step forward moves from index 0 → 1.
			name:     "advance once",
			matches:  linesOf(5, 10, 20),
			calls:    1,
			wantLine: 10,
			wantIdx:  1,
		},
		{
			// Happy path: two steps forward reaches the last element.
			name:     "advance to last",
			matches:  linesOf(5, 10, 20),
			calls:    2,
			wantLine: 20,
			wantIdx:  2,
		},
		{
			// Boundary: extra calls past the end should clamp, not panic or wrap.
			name:     "clamped at last match",
			matches:  linesOf(5, 10, 20),
			calls:    10,
			wantLine: 20,
			wantIdx:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSearchState(tt.matches)

			// ACT: call NextMatch the requested number of times.
			// We capture only the last return value to assert on.
			var current MatchPosition
			for i := 0; i < tt.calls; i++ {
				current = s.NextMatch()
			}

			if current.Line != tt.wantLine {
				t.Errorf("NextMatch().Line = %d, want %d", current.Line, tt.wantLine)
			}
			if s.CurrentIndex() != tt.wantIdx {
				t.Errorf("CurrentIndex() = %d, want %d", s.CurrentIndex(), tt.wantIdx)
			}
		})
	}
}

// TestPrevMatch verifies backward navigation through search results.
//
// "startIdx" advances the state forward first so we have somewhere to go back from.
// This avoids depending on test execution order — each sub-test is self-contained.
func TestPrevMatch(t *testing.T) {
	tests := []struct {
		name     string
		matches  []MatchPosition
		startIdx int // advance forward this many times before testing PrevMatch
		calls    int // how many times to call PrevMatch
		wantLine int
		wantIdx  int
	}{
		{
			// Edge case: calling Prev on an empty state should be a no-op.
			name:     "no matches does nothing",
			matches:  linesOf(),
			startIdx: 0,
			calls:    1,
			wantLine: 0,
			wantIdx:  0,
		},
		{
			// Happy path: start at index 2, go back one → index 1.
			name:     "go back one",
			matches:  linesOf(5, 10, 20),
			startIdx: 2,
			calls:    1,
			wantLine: 10,
			wantIdx:  1,
		},
		{
			// Boundary: extra calls past the start should clamp at index 0, not go negative.
			name:     "clamped at first match",
			matches:  linesOf(5, 10, 20),
			startIdx: 2,
			calls:    10,
			wantLine: 5,
			wantIdx:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSearchState(tt.matches)

			// ARRANGE: fast-forward to a non-zero position so PrevMatch has room to move.
			for i := 0; i < tt.startIdx; i++ {
				s.NextMatch()
			}

			// ACT: call PrevMatch the requested number of times.
			var current MatchPosition
			for i := 0; i < tt.calls; i++ {
				current = s.PrevMatch()
			}

			if current.Line != tt.wantLine {
				t.Errorf("PrevMatch().Line = %d, want %d", current.Line, tt.wantLine)
			}
			if s.CurrentIndex() != tt.wantIdx {
				t.Errorf("CurrentIndex() = %d, want %d", s.CurrentIndex(), tt.wantIdx)
			}
		})
	}
}
