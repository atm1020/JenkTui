package router

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func BenchmarkRouter_NavigateTo(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		r := NewRouter()
		a := newMock("a")
		target := newMock("b")
		r.SetInitialView(a)
		r.RegisterView(target)
		b.StartTimer()

		r.NavigateTo("b")
	}
}

func BenchmarkRouter_Back(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		r := NewRouter()
		a := newMock("a")
		bm := newMock("b")
		r.SetInitialView(a)
		r.RegisterView(bm)
		r.NavigateTo("b")
		b.StartTimer()

		r.Back()
	}
}

func BenchmarkRouter_Forward(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		r := NewRouter()
		a := newMock("a")
		bm := newMock("b")
		r.SetInitialView(a)
		r.RegisterView(bm)
		r.NavigateTo("b")
		r.Back()
		b.StartTimer()

		r.Forward()
	}
}

func BenchmarkRouter_NavigateWithEviction(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		r := NewRouter()
		r.SetInitialView(newMock("a"))
		r.RegisterView(newMock("b"))
		r.RegisterView(newMock("c"))
		r.RegisterView(newMock("d"))
		r.NavigateTo("b")
		r.NavigateTo("c")
		r.NavigateTo("d")
		// backStack is now full (3 entries: a, b, c)
		r.RegisterView(newMock("e"))
		b.StartTimer()

		r.NavigateTo("e") // triggers eviction of "a"
	}
}

func BenchmarkRouter_Update(b *testing.B) {
	r := NewRouter()
	m := newMock("a")
	r.SetInitialView(m)
	msg := tea.WindowSizeMsg{Width: 100, Height: 50}

	b.ReportAllocs()
	for b.Loop() {
		r.Update(msg)
	}
}

func BenchmarkRouter_RegisterView(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		r := NewRouter()
		m := newMock("a")
		b.StartTimer()

		r.RegisterView(m)
	}
}
