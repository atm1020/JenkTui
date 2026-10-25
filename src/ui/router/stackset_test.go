package router

import (
	"slices"
	"testing"
)

func TestNewStackSet(t *testing.T) {
	s := newStackSet()
	if s.len() != 0 {
		t.Fatalf("expected len 0, got %d", s.len())
	}
	if s.set == nil {
		t.Fatal("set map not initialized")
	}
}

func TestNewStackSetFrom(t *testing.T) {
	s := newStackSetFrom("a", "b", "c")
	if s.len() != 3 {
		t.Fatalf("expected len 3, got %d", s.len())
	}
	if !slices.Equal(s.items(), []string{"a", "b", "c"}) {
		t.Fatalf("expected [a b c], got %v", s.items())
	}
	for _, id := range []string{"a", "b", "c"} {
		if !s.contains(id) {
			t.Fatalf("expected contains(%q) = true", id)
		}
	}
}

func TestNewStackSetFromDuplicates(t *testing.T) {
	s := newStackSetFrom("a", "b", "a")
	if s.len() != 3 {
		t.Fatalf("expected len 3, got %d", s.len())
	}
	if s.set["a"] != 2 {
		t.Fatalf("expected refcount 2 for 'a', got %d", s.set["a"])
	}
}

func TestNewStackSetFromEmpty(t *testing.T) {
	s := newStackSetFrom()
	if s.len() != 0 {
		t.Fatalf("expected len 0, got %d", s.len())
	}
}

func TestPush(t *testing.T) {
	s := newStackSet()
	s.push("a")
	s.push("b")

	if s.len() != 2 {
		t.Fatalf("expected len 2, got %d", s.len())
	}
	if !slices.Equal(s.items(), []string{"a", "b"}) {
		t.Fatalf("expected [a b], got %v", s.items())
	}
	if !s.contains("a") || !s.contains("b") {
		t.Fatal("contains should return true for pushed items")
	}
}

func TestPushDuplicate(t *testing.T) {
	s := newStackSet()
	s.push("a")
	s.push("a")

	if s.len() != 2 {
		t.Fatalf("expected len 2, got %d", s.len())
	}
	if s.set["a"] != 2 {
		t.Fatalf("expected refcount 2 for 'a', got %d", s.set["a"])
	}
}

func TestPop(t *testing.T) {
	s := newStackSetFrom("a", "b", "c")
	got := s.pop()

	if got != "c" {
		t.Fatalf("expected pop 'c', got %q", got)
	}
	if s.len() != 2 {
		t.Fatalf("expected len 2, got %d", s.len())
	}
	if s.contains("c") {
		t.Fatal("contains('c') should be false after pop")
	}
	if !slices.Equal(s.items(), []string{"a", "b"}) {
		t.Fatalf("expected [a b], got %v", s.items())
	}
}

func TestPopEmpty(t *testing.T) {
	s := newStackSet()
	got := s.pop()
	if got != "" {
		t.Fatalf("expected empty string from pop on empty stack, got %q", got)
	}
}

func TestPopDuplicateDecrementsRefcount(t *testing.T) {
	s := newStackSetFrom("a", "b", "a")

	got := s.pop() // pops top "a"
	if got != "a" {
		t.Fatalf("expected 'a', got %q", got)
	}
	// "a" still present at bottom, so contains should still be true
	if !s.contains("a") {
		t.Fatal("contains('a') should still be true after popping one duplicate")
	}
	if s.set["a"] != 1 {
		t.Fatalf("expected refcount 1 for 'a', got %d", s.set["a"])
	}

	// Pop "b", then pop last "a"
	s.pop()
	s.pop()
	if s.contains("a") {
		t.Fatal("contains('a') should be false after popping all instances")
	}
}

func TestEvict(t *testing.T) {
	s := newStackSetFrom("a", "b", "c")
	got := s.evict()

	if got != "a" {
		t.Fatalf("expected evict 'a', got %q", got)
	}
	if s.len() != 2 {
		t.Fatalf("expected len 2, got %d", s.len())
	}
	if s.contains("a") {
		t.Fatal("contains('a') should be false after evict")
	}
	if !slices.Equal(s.items(), []string{"b", "c"}) {
		t.Fatalf("expected [b c], got %v", s.items())
	}
}

func TestEvictEmpty(t *testing.T) {
	s := newStackSet()
	got := s.evict()
	if got != "" {
		t.Fatalf("expected empty string from evict on empty stack, got %q", got)
	}
}

func TestEvictDuplicateDecrementsRefcount(t *testing.T) {
	s := newStackSetFrom("a", "b", "a")

	got := s.evict() // evicts bottom "a"
	if got != "a" {
		t.Fatalf("expected 'a', got %q", got)
	}
	if !s.contains("a") {
		t.Fatal("contains('a') should still be true after evicting one duplicate")
	}
	if s.set["a"] != 1 {
		t.Fatalf("expected refcount 1 for 'a', got %d", s.set["a"])
	}
}

func TestContains(t *testing.T) {
	s := newStackSetFrom("x", "y")
	if !s.contains("x") {
		t.Fatal("expected contains('x') = true")
	}
	if s.contains("z") {
		t.Fatal("expected contains('z') = false")
	}
}

func TestClear(t *testing.T) {
	s := newStackSetFrom("a", "b", "c")
	s.clear()

	if s.len() != 0 {
		t.Fatalf("expected len 0 after clear, got %d", s.len())
	}
	if s.contains("a") || s.contains("b") || s.contains("c") {
		t.Fatal("contains should return false for all items after clear")
	}
	if len(s.set) != 0 {
		t.Fatalf("expected empty set map after clear, got %d entries", len(s.set))
	}
}

func TestPeek(t *testing.T) {
	s := newStackSetFrom("a", "b", "c")
	got := s.peek()

	if got != "c" {
		t.Fatalf("expected peek 'c', got %q", got)
	}
	// peek should not modify the stack
	if s.len() != 3 {
		t.Fatalf("peek should not change len, expected 3, got %d", s.len())
	}
}

func TestPeekEmpty(t *testing.T) {
	s := newStackSet()
	got := s.peek()
	if got != "" {
		t.Fatalf("expected empty string from peek on empty stack, got %q", got)
	}
}

func TestPeekAfterPop(t *testing.T) {
	s := newStackSetFrom("a", "b")
	s.pop()
	got := s.peek()
	if got != "a" {
		t.Fatalf("expected peek 'a' after pop, got %q", got)
	}
}

func TestItems(t *testing.T) {
	s := newStackSetFrom("x", "y", "z")
	items := s.items()
	if !slices.Equal(items, []string{"x", "y", "z"}) {
		t.Fatalf("expected [x y z], got %v", items)
	}
}

func TestItemsEmpty(t *testing.T) {
	s := newStackSet()
	if len(s.items()) != 0 {
		t.Fatalf("expected empty items, got %v", s.items())
	}
}

func TestPushPopRoundTrip(t *testing.T) {
	s := newStackSet()
	s.push("a")
	s.push("b")
	s.push("c")

	if s.pop() != "c" || s.pop() != "b" || s.pop() != "a" {
		t.Fatal("pop should return items in LIFO order")
	}
	if s.len() != 0 {
		t.Fatalf("expected empty stack after popping all, got len %d", s.len())
	}
	if len(s.set) != 0 {
		t.Fatalf("expected empty set after popping all, got %d entries", len(s.set))
	}
}

func TestEvictThenPop(t *testing.T) {
	s := newStackSetFrom("a", "b", "c")

	evicted := s.evict()
	if evicted != "a" {
		t.Fatalf("expected evict 'a', got %q", evicted)
	}

	popped := s.pop()
	if popped != "c" {
		t.Fatalf("expected pop 'c', got %q", popped)
	}

	if s.len() != 1 {
		t.Fatalf("expected len 1, got %d", s.len())
	}
	if !slices.Equal(s.items(), []string{"b"}) {
		t.Fatalf("expected [b], got %v", s.items())
	}
}
