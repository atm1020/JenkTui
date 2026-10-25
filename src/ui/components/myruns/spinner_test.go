package myruns

import (
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/store"
)

func TestRefreshPreservesSpinnerCadence(t *testing.T) {
	for _, queried := range []bool{false, true} {
		name := "loaded"
		if queried {
			name = "queried"
		}
		t.Run(name, func(t *testing.T) {
			m := &Model{wWidth: 120, wHeight: 40, spinnerModel: spinner.New()}
			refresh := func(status buildstatus.PipelineStatus) []spinner.TickMsg {
				runs := []store.RunRecord{{ID: "job#1", JobName: "job", BuildID: "1", Status: status}}
				if queried {
					return immediateSpinnerTicks(m.handleRunsQueried(RunsQueriedMsg{Runs: runs}))
				}
				return immediateSpinnerTicks(m.handleRunsLoaded(RunsLoadedMsg{Runs: runs}))
			}
			first := refresh("IN_PROGRESS")
			if len(first) != 1 {
				t.Fatalf("initial load: got %d ticks, want 1", len(first))
			}
			pending := m.handleSpinnerTick(first[0])
			if pending == nil {
				t.Fatal("initial tick did not schedule next frame")
			}
			frame := m.spinnerModel.View()
			for range 3 {
				if ticks := refresh("IN_PROGRESS"); len(ticks) != 0 {
					t.Fatalf("refresh injected %d immediate frames into running chain", len(ticks))
				}
			}
			if m.spinnerModel.View() != frame {
				t.Fatal("refresh advanced spinner")
			}
			if next := m.handleSpinnerTick(pending().(spinner.TickMsg)); next == nil {
				t.Fatal("refresh invalidated pending frame")
			}
			if ticks := refresh("SUCCESS"); len(ticks) != 0 {
				t.Fatal("terminal run started animation")
			}
			if m.handleSpinnerTick(m.spinnerModel.Tick().(spinner.TickMsg)) != nil {
				t.Fatal("terminal run kept animation alive")
			}
			if ticks := refresh("IN_PROGRESS"); len(ticks) != 1 {
				t.Fatal("animation did not restart when active runs returned")
			}
			m.CleanUp()
			if ticks := refresh("IN_PROGRESS"); len(ticks) != 1 {
				t.Fatal("animation did not restart after re-entry")
			}
		})
	}
}

// Only expand commands from a data refresh, never a tick's continuing timer.
func immediateSpinnerTicks(cmd tea.Cmd) []spinner.TickMsg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case spinner.TickMsg:
		return []spinner.TickMsg{msg}
	case tea.BatchMsg:
		var ticks []spinner.TickMsg
		for _, child := range msg {
			ticks = append(ticks, immediateSpinnerTicks(child)...)
		}
		return ticks
	default:
		return nil
	}
}
