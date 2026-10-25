package logviewer

// SearchState manages search result navigation
type SearchState struct {
	matches    []MatchPosition
	currentIdx int
}

// NewSearchState creates a new search state with match occurrences
func NewSearchState(matches []MatchPosition) *SearchState {
	return &SearchState{
		matches:    matches,
		currentIdx: 0,
	}
}

// CurrentIndex returns the current match index
func (s *SearchState) CurrentIndex() int {
	return s.currentIdx
}

// CurrentMatch returns the current match occurrence (the zero value if
// there are no matches).
func (s *SearchState) CurrentMatch() MatchPosition {
	if len(s.matches) == 0 {
		return MatchPosition{}
	}
	return s.matches[s.currentIdx]
}

// CurrentLine returns the line number of current match
func (s *SearchState) CurrentLine() int {
	return s.CurrentMatch().Line
}

// NextMatch moves to next match and returns it
func (s *SearchState) NextMatch() MatchPosition {
	if s.currentIdx < len(s.matches)-1 {
		s.currentIdx++
	}
	return s.CurrentMatch()
}

// PrevMatch moves to previous match and returns it
func (s *SearchState) PrevMatch() MatchPosition {
	if s.currentIdx > 0 {
		s.currentIdx--
	}
	return s.CurrentMatch()
}

// TotalMatches returns total number of matches
func (s *SearchState) TotalMatches() int {
	return len(s.matches)
}
