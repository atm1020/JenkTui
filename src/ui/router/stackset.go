package router

// stackSet is an ordered stack with O(1) membership lookup.
// It supports duplicate IDs via reference counting.
type stackSet struct {
	stack []string
	set   map[string]int // id -> count
}

func newStackSet() stackSet {
	return stackSet{set: make(map[string]int)}
}

// newStackSetFrom creates a stackSet pre-populated with the given ids (bottom to top).
func newStackSetFrom(ids ...string) stackSet {
	s := stackSet{
		stack: make([]string, len(ids)),
		set:   make(map[string]int, len(ids)),
	}
	copy(s.stack, ids)
	for _, id := range ids {
		s.set[id]++
	}
	return s
}

// push appends an id to the top of the stack.
func (s *stackSet) push(id string) {
	s.stack = append(s.stack, id)
	s.set[id]++
}

// pop removes and returns the top (most recent) entry.
func (s *stackSet) pop() string {
	if len(s.stack) == 0 {
		return ""
	}
	last := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	s.set[last]--
	if s.set[last] <= 0 {
		delete(s.set, last)
	}
	return last
}

// evict removes and returns the bottom (oldest) entry.
func (s *stackSet) evict() string {
	if len(s.stack) == 0 {
		return ""
	}
	first := s.stack[0]
	s.stack = s.stack[1:]
	s.set[first]--
	if s.set[first] <= 0 {
		delete(s.set, first)
	}
	return first
}

// len returns the number of entries in the stack.
func (s *stackSet) len() int {
	return len(s.stack)
}

// contains reports whether the id is anywhere in the stack.
func (s *stackSet) contains(id string) bool {
	return s.set[id] > 0
}

// clear removes all entries.
func (s *stackSet) clear() {
	s.stack = nil
	s.set = make(map[string]int)
}

// items returns the underlying slice for iteration/logging.
func (s *stackSet) items() []string {
	return s.stack
}

// peek returns the top element without removing it.
func (s *stackSet) peek() string {
	if len(s.stack) == 0 {
		return ""
	}
	return s.stack[len(s.stack)-1]
}
