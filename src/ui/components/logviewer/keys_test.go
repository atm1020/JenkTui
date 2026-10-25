package logviewer

import (
	"testing"

	"github.com/atm1020/jenktui/src/config"

	"charm.land/bubbles/v2/key"
)

// FullHelp must surface every navigation key so the expanded help view (?)
// doesn't omit keys that ShortHelp already advertises in the footer.
func TestKeyMap_FullHelp_IncludesPageUpAndPageDown(t *testing.T) {
	km := UserKeyMap(config.GetDefaultConfig().KeyMapConfig.Logviewer)

	var found key.Binding
	hasPageUp, hasPageDown := false, false
	for _, row := range km.FullHelp() {
		for _, binding := range row {
			if binding.Help().Desc == km.PageUp.Help().Desc {
				hasPageUp = true
				found = binding
			}
			if binding.Help().Desc == km.PageDown.Help().Desc {
				hasPageDown = true
			}
		}
	}

	if !hasPageUp {
		t.Error("expected FullHelp() to include PageUp")
	}
	if !hasPageDown {
		t.Error("expected FullHelp() to include PageDown")
	}
	if hasPageUp && len(found.Keys()) == 0 {
		t.Error("expected PageUp binding in FullHelp() to carry its configured keys")
	}
}

// Only doubled runes are treated as multi-key sequences; named keys must stay
// whole-string matches, or half a key name would swallow the next press.
func TestIsSequencePrefix(t *testing.T) {
	b := key.NewBinding(key.WithKeys("gg", "home"))

	tests := []struct {
		typed string
		want  bool
	}{
		{"g", true},
		{"gg", false}, // complete, not a prefix
		{"h", false},  // "home" is a named key, not a sequence
		{"ho", false},
		{"j", false},
	}

	for _, tt := range tests {
		if got := isSequencePrefix(tt.typed, b); got != tt.want {
			t.Errorf("isSequencePrefix(%q) = %v, want %v", tt.typed, got, tt.want)
		}
	}

	if isSequencePrefix("g", key.NewBinding(key.WithKeys("gg"), key.WithDisabled())) {
		t.Error("expected a disabled binding to have no sequence prefixes")
	}
}

func TestMatchesKey(t *testing.T) {
	b := key.NewBinding(key.WithKeys("gg", "home"))

	if !matchesKey("gg", b) {
		t.Error("expected 'gg' to match")
	}
	if !matchesKey("home", b) {
		t.Error("expected 'home' to match")
	}
	if matchesKey("g", b) {
		t.Error("expected a half-typed 'g' not to match")
	}
}
