package buildparams

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
)

type choiceItem struct {
	index  int
	choice gojenkins.Choice
	picked bool
}

func (i choiceItem) FilterValue() string { return i.choice.Label }

type choiceDelegate struct{}

func (choiceDelegate) Height() int                         { return 1 }
func (choiceDelegate) Spacing() int                        { return 0 }
func (choiceDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }
func (choiceDelegate) ShortHelp() []key.Binding            { return nil }
func (choiceDelegate) FullHelp() [][]key.Binding           { return nil }
func (choiceDelegate) Render(w io.Writer, model list.Model, index int, raw list.Item) {
	item := raw.(choiceItem)
	t := styles.GetTheme()
	marker := "  "
	if item.picked {
		marker = "✓ "
	}
	style := t.S().Base
	if item.choice.Disabled {
		style = style.Foreground(t.Grey)
	}
	if index == model.Index() {
		style = style.Foreground(t.HighlightForeground).Background(t.Primary)
	}
	_, _ = fmt.Fprint(w, style.Render(marker+item.choice.Label))
}

type choicePicker struct {
	list       list.Model
	fieldIndex int
	multiple   bool
	hint       string
	// toggled records a space toggle in a multi-select. Until then enter picks
	// the highlighted choice, as it does in a single-select.
	toggled bool
}

func newChoicePicker(fieldIndex int, parameterName string, field *choiceField, maxWidth, maxHeight int, startsFiltering bool, keymap KeyMap) *choicePicker {
	items := make([]list.Item, 0, len(field.choices))
	contentWidth := lipgloss.Width(parameterName) + 4
	for i, choice := range field.choices {
		items = append(items, choiceItem{index: i, choice: choice, picked: field.selected[i]})
		contentWidth = max(contentWidth, lipgloss.Width(choice.Label)+4)
	}

	hint := pickerHint(field.multiple, keymap)
	contentWidth = max(contentWidth, lipgloss.Width(hint)+2)
	if maxWidth <= 0 {
		maxWidth = 48
	}
	width := min(max(contentWidth, 22), maxWidth)
	if maxHeight <= 0 {
		maxHeight = 14
	}
	// List height includes title/filter, item count, and pagination rows.
	// The custom key hint is rendered outside the list below this height.
	height := min(max(len(items)+4, 7), maxHeight)

	model := list.New(items, choiceDelegate{}, width, height)
	model.Title = parameterName
	model.SetShowHelp(false)
	model.SetShowPagination(true)
	model.SetShowStatusBar(true)
	model.SetFilteringEnabled(true)
	model.KeyMap = pickerListKeyMap(keymap)
	model.Filter = substringFilter
	model.SetStatusBarItemName("choice", "choices")
	model.Paginator.Type = paginator.Arabic
	model.DisableQuitKeybindings()

	// SetFilterState alone leaves Bubbles' filtered-item cache empty. Seed it
	// with the complete list first, and reset on every open so a previous
	// picker's search never determines this picker's initial contents.
	model.SetFilterText("")
	if startsFiltering {
		model.SetFilterState(list.Filtering)
	} else {
		model.ResetFilter()
	}

	t := styles.GetTheme()
	model.Styles.TitleBar = t.S().Base.Padding(0, 1)
	model.Styles.Title = t.S().Base.Foreground(t.Primary).Bold(true)
	model.Styles.NoItems = t.S().Base.Foreground(t.Grey).Padding(0, 1)
	model.Styles.StatusBar = t.S().Base.Foreground(t.Grey).PaddingLeft(1)
	model.Styles.StatusEmpty = t.S().Base.Foreground(t.Grey)
	model.Styles.StatusBarActiveFilter = t.S().Base.Foreground(t.Primary)
	model.Styles.StatusBarFilterCount = t.S().Base.Foreground(t.Grey)
	model.Styles.PaginationStyle = t.S().Base.Foreground(t.Grey).PaddingLeft(1)
	model.Styles.ArabicPagination = t.S().Base.Foreground(t.Grey)
	model.Styles.DefaultFilterCharacterMatch = t.S().Base.Foreground(t.Primary).Underline(true)
	// Recalculate page capacity after replacing the default status styles;
	// Bubbles' defaults reserve an extra bottom row.
	model.SetSize(width, height)

	return &choicePicker{
		list:       model,
		fieldIndex: fieldIndex,
		multiple:   field.multiple,
		hint:       hint,
	}
}

// pickerListKeyMap maps the configured dialog bindings onto the Bubbles list,
// and switches off the list's help and quit bindings, so every key the
// picker reacts to comes from config.json.
func pickerListKeyMap(k KeyMap) list.KeyMap {
	unbound := key.NewBinding()
	return list.KeyMap{
		CursorUp:             k.Up,
		CursorDown:           k.Down,
		NextPage:             k.NextPage,
		PrevPage:             k.PrevPage,
		GoToStart:            k.GoToStart,
		GoToEnd:              k.GoToEnd,
		Filter:               k.Filter,
		ClearFilter:          k.Close,
		CancelWhileFiltering: k.Close,
		AcceptWhileFiltering: key.NewBinding(key.WithKeys(append(k.OpenPicker.Keys(), k.Confirm.Keys()...)...)),
		ShowFullHelp:         unbound,
		CloseFullHelp:        unbound,
		Quit:                 unbound,
		ForceQuit:            unbound,
	}
}

func (p *choicePicker) View() string {
	t := styles.GetTheme()
	muted := t.S().Base.Foreground(t.Grey)
	hint := muted.Render(p.hint)
	body := p.list.View()
	if p.noMatch() {
		// Replace Bubbles' "Nothing matched" status so an empty filter reads
		// differently from a field whose script returned no choices.
		list := p.list
		list.SetShowStatusBar(false)
		body = lipgloss.JoinVertical(lipgloss.Left, list.View(), muted.PaddingLeft(1).Render("no match"))
	}
	content := lipgloss.JoinVertical(lipgloss.Left, body, hint)
	return t.S().DialogBorder.Padding(0, 1).Render(content)
}

// noMatch reports a filter being typed that matches none of the choices.
// Accepting such a filter resets it, so this state only exists while typing.
func (p *choicePicker) noMatch() bool {
	return p.list.SettingFilter() && p.list.FilterValue() != "" && len(p.list.VisibleItems()) == 0
}

func substringFilter(term string, targets []string) []list.Rank {
	term = strings.ToLower(term)
	ranks := make([]list.Rank, 0, len(targets))
	for i, target := range targets {
		if strings.Contains(strings.ToLower(target), term) {
			ranks = append(ranks, list.Rank{Index: i})
		}
	}
	return ranks
}

// pickerHint lists the picker's keys as configured; an unbound action is left
// out rather than advertised under a key that would not work.
func pickerHint(multiple bool, k KeyMap) string {
	var parts []string
	add := func(binding key.Binding, action string) {
		if keys := binding.Help().Key; keys != "" {
			parts = append(parts, keys+" "+action)
		}
	}
	if multiple {
		add(k.ToggleChoice, "toggle")
	}
	add(k.OpenPicker, "select")
	add(k.Close, "close")
	add(k.Filter, "filter")
	return strings.Join(parts, "  •  ")
}

func (d *BuildParamsDialog) handlePickerKey(msg tea.KeyPressMsg) tea.Cmd {
	picker := d.picker
	field, ok := d.params[picker.fieldIndex].field.(*choiceField)
	if !ok {
		d.picker = nil
		return nil
	}
	if picker.list.SettingFilter() {
		return d.handleFilteringKey(msg, field)
	}
	if key.Matches(msg, d.keymap.Close) && picker.list.IsFiltered() {
		picker.list.ResetFilter()
		return nil
	}
	switch {
	case key.Matches(msg, d.keymap.Close):
		d.picker = nil
	case key.Matches(msg, d.keymap.Up):
		picker.list.CursorUp()
	case key.Matches(msg, d.keymap.Down):
		picker.list.CursorDown()
	case key.Matches(msg, d.keymap.ToggleChoice) && field.multiple:
		picker.toggled = true
		return d.applyPickerChoice(field, false, false)
	case key.Matches(msg, d.keymap.Confirm, d.keymap.OpenPicker):
		if picker.toggled {
			d.picker = nil
			return nil
		}
		return d.applyPickerChoice(field, true, true)
	default:
		var cmd tea.Cmd
		picker.list, cmd = picker.list.Update(msg)
		return cmd
	}
	return nil
}

// handleFilteringKey routes a key while the filter input is focused. Printable
// keys are filter text even when bound (j, k, space), so only non-text keys
// navigate or select.
func (d *BuildParamsDialog) handleFilteringKey(msg tea.KeyPressMsg, field *choiceField) tea.Cmd {
	picker := d.picker
	switch {
	case isCommandKey(msg, d.keymap.Up):
		picker.list.CursorUp()
	case isCommandKey(msg, d.keymap.Down):
		picker.list.CursorDown()
	case !field.multiple && isCommandKey(msg, d.keymap.Confirm, d.keymap.OpenPicker):
		return d.applyPickerChoice(field, true, true)
	default:
		var cmd tea.Cmd
		picker.list, cmd = picker.list.Update(msg)
		return cmd
	}
	return nil
}

// isCommandKey matches a binding only for keys that type no text.
func isCommandKey(msg tea.KeyPressMsg, bindings ...key.Binding) bool {
	return msg.Text == "" && key.Matches(msg, bindings...)
}

func (d *BuildParamsDialog) updatePicker(msg tea.Msg) tea.Cmd {
	if d.picker == nil {
		return nil
	}
	var cmd tea.Cmd
	d.picker.list, cmd = d.picker.list.Update(msg)
	return cmd
}

// applyPickerChoice selects the highlighted choice. replace makes it the only
// selection; otherwise a multi-select toggles it alongside the others.
func (d *BuildParamsDialog) applyPickerChoice(field *choiceField, replace, closePicker bool) tea.Cmd {
	parameterName := d.params[d.picker.fieldIndex].name()
	item, ok := d.picker.list.SelectedItem().(choiceItem)
	if !ok || item.choice.Disabled {
		return nil
	}
	old := field.Value()
	if field.multiple && !replace {
		field.selected[item.index] = !field.selected[item.index]
	} else {
		clear(field.selected)
		field.selected[item.index] = true
	}
	for i, raw := range d.picker.list.Items() {
		updated := raw.(choiceItem)
		updated.picked = field.selected[updated.index]
		_ = d.picker.list.SetItem(i, updated)
	}
	if closePicker {
		d.picker = nil
	}
	if old != field.Value() {
		field.warnAmbiguousCommas(parameterName)
		return d.propagateChange(parameterName)
	}
	return nil
}

func (d *BuildParamsDialog) pickerView() string {
	if d.picker == nil {
		return ""
	}
	return d.picker.View()
}
