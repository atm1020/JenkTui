package themeselector

import (
	"testing"

	colortheme "github.com/atm1020/jenktui/src/theme"
	"github.com/atm1020/jenktui/src/ui/styles"

	tea "charm.land/bubbletea/v2"
)

func TestThemeFilterAcceptsConfiguredNavigationLetters(t *testing.T) {
	listModel := initList(40, 48, 12, styles.Github)
	dialog := &ThemeSelectorDialog{
		list:   listModel,
		keyMap: DefaultThemeSelectorKeyMap(),
	}

	_, _ = dialog.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !dialog.list.SettingFilter() {
		t.Fatal("slash did not start list filtering")
	}
	_, _ = dialog.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	if got := dialog.list.FilterValue(); got != "k" {
		t.Fatalf("filter value = %q, want k", got)
	}
}

func TestInitListIncludesCatalogAndSelectsCurrentTheme(t *testing.T) {
	styles.SetThemeDefinitions(colortheme.LoadAll(""))
	styles.SwitchTheme(styles.Github)
	t.Cleanup(func() {
		styles.SetThemeDefinitions(colortheme.LoadAll(""))
		styles.SwitchTheme(styles.Github)
	})

	listModel := initList(40, 48, 12, styles.TokyoNight)
	if got := len(listModel.Items()); got != 11 {
		t.Fatalf("theme item count = %d, want 11", got)
	}
	selected, ok := listModel.SelectedItem().(ThemeItem)
	if !ok {
		t.Fatalf("selected item type = %T, want ThemeItem", listModel.SelectedItem())
	}
	if selected.themeType != styles.TokyoNight {
		t.Fatalf("selected theme = %q, want %q", selected.themeType, styles.TokyoNight)
	}
}
