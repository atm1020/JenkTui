package styles

import (
	"strings"
	"testing"
)

const githubBgANSI = "48;2;13;17;23m"

// The app surface comes from OSC 11 (tea.View.BackgroundColor), so
// base-derived styles must NOT paint BgBase themselves — in terminals without
// OSC 11 support that renders as mismatched background patches.
func TestSurfaceStylesDoNotPaintThemeBackground(t *testing.T) {
	SwitchTheme(Github)
	s := GetTheme().S()

	tests := []struct {
		name   string
		render string
	}{
		{name: "base", render: s.Base.Render("base")},
		{name: "text", render: s.Text.Render("text")},
		{name: "title", render: s.Title.Render("title")},
		{name: "subtitle", render: s.Subtitle.Render("subtitle")},
		{name: "list title", render: s.ListDelegate.NormalTitle.Render("job")},
		{name: "list description", render: s.ListDelegate.NormalDesc.Render("desc")},
		{name: "folder icon", render: s.StyledIcons.Folder},
		{name: "attachment icon", render: s.StyledIcons.Attachment},
		{name: "dialog border", render: s.DialogBorder.Render("dialog")},
		{name: "stage box success", render: s.StageBox.Success.Render("Prepare")},
		{name: "stage box pending", render: s.StageBox.Pending.Render("Deploy")},
		{name: "gutter line number", render: s.Gutter.LineNumber.Render("  1")},
		{name: "gutter separator", render: s.Gutter.Separator.Render("│")},
		{name: "spinner", render: s.Spinner.Render("x")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No background SGR of any kind: the old stage fills were
			// alpha-tinted BgBase variants, so checking only for the exact
			// BgBase sequence would miss them.
			if strings.Contains(tt.render, "[48;") || strings.Contains(tt.render, githubBgANSI) {
				t.Fatalf("surface style paints a background; rely on OSC 11 instead: %q", tt.render)
			}
		})
	}
}
