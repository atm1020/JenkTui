package block

// Memo keeps rendered Blocks by the key they were rendered with. A key holds
// everything that can change the section, including the animation frame it
// shows, so a hit is always the current output.
//
// Keep is how many keys stay cached; zero keeps one. A section that shows a
// spinner sets it to the spinner's frame count, so after one cycle every frame
// is a hit, the way Crush pre-renders its animation frames (animCacheMap in
// internal/ui/anim/anim.go). Without it, the build page re-rendered its
// header and running step box through lipgloss on every frame, about three
// quarters of a 240 µs frame. When a new key
// would exceed Keep, every entry is dropped: after a state change the old
// keys never match again.
type Memo[K comparable] struct {
	Keep    int
	entries map[K]Block
	renders int
}

func (m *Memo[K]) Get(key K, render func() Block) Block {
	if b, ok := m.entries[key]; ok {
		return b
	}
	if m.entries == nil || len(m.entries) >= max(1, m.Keep) {
		m.entries = make(map[K]Block, max(1, m.Keep))
	}
	b := render()
	m.entries[key] = b
	m.renders++
	return b
}

func (m *Memo[K]) Reset() {
	m.entries = nil
}

// Renders counts cache misses, for tests and profiling.
func (m *Memo[K]) Renders() int {
	return m.renders
}
