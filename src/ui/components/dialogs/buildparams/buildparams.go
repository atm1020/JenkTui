package buildparams

import (
	stdctx "context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
)

var dialogSequence atomic.Uint64

const (
	labelWidth        = 20
	textareaHeight    = 4
	textareaMaxHeight = 6
)

// Jenkins parameter classes, as ParameterDefinition.Type and
// FormParameter.Type report them. Active Choices classes carry no
// ParameterDefinition suffix.
const (
	classBoolean          = "BooleanParameterDefinition"
	classText             = "TextParameterDefinition"
	classPassword         = "PasswordParameterDefinition"
	classChoice           = "ChoiceParameterDefinition"
	classActiveChoice     = "ChoiceParameter"
	classCascadeChoice    = "CascadeChoiceParameter"
	classDynamicReference = "DynamicReferenceParameter"
)

func NewBuildParamsDialog(
	program *context.ProgramContext,
	job *gojenkins.Job,
	jobName string,
	defs []gojenkins.ParameterDefinition,
	windowWidth int,
	onBuild func(params map[string]string) tea.Cmd,
	onCancel tea.Cmd,
	keymapConf context.BuildParamsDialogKeyMapConfig,
) *BuildParamsDialog {
	var loader func(stdctx.Context) (formResolver, error)
	if job != nil {
		loader = func(ctx stdctx.Context) (formResolver, error) {
			form, err := job.GetBuildForm(ctx)
			if err != nil {
				return nil, err
			}
			return buildFormResolver{form}, nil
		}
	}
	return newDialog(program, loader, jobName, defs, windowWidth, onBuild, onCancel, keymapConf)
}

// newDialog builds the dialog around an injectable form loader. With no
// loader, dynamic parameters degrade to text inputs straight away.
func newDialog(
	program *context.ProgramContext,
	formLoader func(stdctx.Context) (formResolver, error),
	jobName string,
	defs []gojenkins.ParameterDefinition,
	windowWidth int,
	onBuild func(params map[string]string) tea.Cmd,
	onCancel tea.Cmd,
	keymapConf context.BuildParamsDialogKeyMapConfig,
) *BuildParamsDialog {
	inputWidth := dialogWidth(windowWidth) - labelWidth - 8
	params := make([]*paramState, len(defs))
	hasDynamic := false
	for i, def := range defs {
		p := &paramState{def: def}
		switch {
		case def.Type == classChoice:
			p.field = newStaticChoiceField(def)
			p.status = resolveDone
		case isActiveChoiceParam(def.Type):
			hasDynamic = true
			field := newChoiceField(nil, false)
			field.loading = true
			p.field = field
			p.status = resolveLoading
		case def.Type == classText:
			p.field = newTextAreaField(def, inputWidth)
		default:
			p.field = newTextInputField(def, inputWidth)
		}
		if def.Type == classBoolean {
			p.boolValue = fmt.Sprintf("%v", def.DefaultParameterValue.Value) == "true"
			p.field.SetValue(fmt.Sprintf("%v", p.boolValue))
		}
		if i == 0 {
			p.field.Focus()
		}
		params[i] = p
	}

	h := help.New()
	h.SetWidth(dialogWidth(windowWidth))

	dialog := &BuildParamsDialog{
		width:            windowWidth,
		id:               dialogSequence.Add(1),
		program:          program,
		params:           params,
		focused:          0,
		keymap:           UserKeyMap(keymapConf),
		help:             h,
		onBuild:          onBuild,
		onCancel:         onCancel,
		jobName:          jobName,
		hasDynamicParams: hasDynamic,
		formLoader:       formLoader,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(styles.GetTheme().S().Spinner),
		),
	}
	if hasDynamic && formLoader == nil {
		dialog.degradeDynamicParams()
	}
	return dialog
}

func (d *BuildParamsDialog) Init() tea.Cmd {
	if d.hasDynamicParams && d.formLoader != nil {
		return tea.Batch(textinput.Blink, d.loadForm(), d.startSpinner())
	}
	return textinput.Blink
}

func (d *BuildParamsDialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
	case tea.KeyPressMsg:
		return d, d.handleKeyPress(msg)
	case FormLoadedMsg:
		return d, d.handleFormLoaded(msg)
	case ChoicesLoadedMsg:
		return d, d.handleChoicesLoaded(msg)
	case spinner.TickMsg:
		return d, d.handleSpinnerTick(msg)
	}
	if d.picker != nil {
		return d, d.updatePicker(msg)
	}
	return d, d.updateFocusedField(msg)
}

func (d *BuildParamsDialog) View() string {
	t := styles.GetTheme()
	dw := dialogWidth(d.width)

	title := t.S().Title.Render(fmt.Sprintf("Build %s", d.jobName))

	rows := []string{title, ""}

	nameStyle := t.S().Base.Bold(true).Width(labelWidth)
	descStyle := t.S().Base.Foreground(t.Grey).Italic(true)
	separator := t.S().Base.Foreground(t.Grey).Render(strings.Repeat("─", dw-6))
	focusIndicator := t.S().Base.Foreground(t.Primary).Render("▶ ")
	emptyIndicator := "  "

	for i, p := range d.params {
		focused := i == d.focused
		indicator := emptyIndicator
		label := nameStyle.Render(p.name())
		if focused {
			indicator = focusIndicator
			label = nameStyle.Foreground(t.Primary).Render(p.name())
		}
		paramType := renderParamType(t, p.def.Type)

		var inputView string
		if p.def.Type == classBoolean {
			inputView = renderBoolToggle(t, p.boolValue, focused)
		} else if field, ok := p.field.(*choiceField); ok {
			inputView = d.renderChoiceField(t, field, focused)
		} else {
			inputView = p.field.View()
		}

		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, indicator, label, paramType))
		if p.def.Description != "" {
			rows = append(rows, "  "+descStyle.Render(util.TurncateString(p.def.Description, dw-8)))
		}
		rows = append(rows, "  "+inputView)
		if p.reason != "" {
			rows = append(rows, "  "+t.S().Base.Foreground(t.Warning).Render(p.reason))
		}
		if i < len(d.params)-1 {
			rows = append(rows, separator)
		}
	}

	if classes := d.unsupportedClasses(); len(classes) > 0 {
		infoStyle := t.S().Base.Foreground(t.Warning).Italic(true)
		banner := "Unsupported, entered as text: " + strings.Join(classes, ", ")
		rows = append(rows, "", infoStyle.Render(util.TurncateString(banner, dw-6)))
	}

	rows = append(rows, "", d.help.View(d.keymap))

	// Paint every row to the full inner width so no unstyled cells leak the
	// terminal's default background, then let the fixed-width container frame it.
	border := t.S().DialogBorder
	innerWidth := dw - border.GetHorizontalFrameSize()
	for i, r := range rows {
		rows[i] = util.PadWidth(r, innerWidth)
	}
	content := strings.Join(rows, "\n")
	view := border.Width(dw).Render(content)
	if d.picker != nil {
		picker := d.pickerView()
		col := max(0, (lipgloss.Width(view)-lipgloss.Width(picker))/2)
		view = lipgloss.NewCompositor(
			lipgloss.NewLayer(view),
			lipgloss.NewLayer(picker).X(col).Y(2),
		).Render()
	}
	return view
}

func (d *BuildParamsDialog) UpdateFooter() tea.Cmd {
	return nil
}

// CleanUp releases the session-bound form when the dialog is dismissed.
func (d *BuildParamsDialog) CleanUp() tea.Cmd {
	d.form = nil
	d.picker = nil
	return nil
}

func (d *BuildParamsDialog) Dimensions() (int, int) {
	v := d.View()
	return lipgloss.Width(v), lipgloss.Height(v)
}

func (d *BuildParamsDialog) KeyMap() help.KeyMap {
	return d.keymap
}

func (d *BuildParamsDialog) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	if d.picker != nil {
		return d.handlePickerKey(msg)
	}
	focused := d.params[d.focused]
	switch {
	case key.Matches(msg, d.keymap.HelpToggle):
		d.help.ShowAll = !d.help.ShowAll
	case key.Matches(msg, d.keymap.Close):
		return d.handleClose()
	case key.Matches(msg, d.keymap.Confirm):
		return d.handleConfirm()
	case key.Matches(msg, d.keymap.OpenPicker):
		d.handleOpenPicker()
	case key.Matches(msg, d.keymap.Reload) && d.hasDynamicParams && d.formLoader != nil:
		return d.handleReload()
	case key.Matches(msg, d.keymap.Down, d.keymap.Tab):
		return d.focusNext()
	case key.Matches(msg, d.keymap.Up):
		return d.focusPrev()
	case focused.def.Type == classBoolean && key.Matches(msg, d.keymap.Toggle):
		focused.boolValue = !focused.boolValue
		focused.field.SetValue(fmt.Sprintf("%v", focused.boolValue))
	case focused.def.Type == classBoolean:
		// Block typing for boolean parameters.
	default:
		return d.updateFocusedField(msg)
	}
	return nil
}

func (d *BuildParamsDialog) handleClose() tea.Cmd {
	cmds := []tea.Cmd{util.CmdHandler(util.CloseDialogMsg{})}
	if d.onCancel != nil {
		// onCancel is a tea.Cmd (func() tea.Msg). Returning it here is what
		// fires it; Bubble Tea runs it after Update. Do not call onCancel().
		cmds = append(cmds, d.onCancel)
	}
	return tea.Batch(cmds...)
}

func (d *BuildParamsDialog) handleConfirm() tea.Cmd {
	if d.hasPendingResolution() {
		return nil
	}
	values := make(map[string]string, len(d.params))
	for _, p := range d.params {
		if p.omitValue {
			continue
		}
		values[p.name()] = p.field.Value()
		if field, ok := p.field.(*choiceField); ok {
			field.warnAmbiguousCommas(p.name())
		}
	}
	// onBuild is the only callback invoked with (); it returns the trigger
	// tea.Cmd, which the runtime then executes (same as onCancel above).
	return tea.Batch(
		util.CmdHandler(util.CloseDialogMsg{}),
		d.onBuild(values),
	)
}

func (d *BuildParamsDialog) handleOpenPicker() {
	p := d.params[d.focused]
	field, ok := p.field.(*choiceField)
	if !ok || field.loading {
		return
	}
	d.picker = newChoicePicker(
		d.focused,
		p.name(),
		field,
		dialogWidth(d.width)-8,
		d.height-6,
		p.filterable,
		d.keymap,
	)
}

// handleReload renders the form again. Bumping every dynamic parameter's gen
// drops any choices still in flight from the previous form.
func (d *BuildParamsDialog) handleReload() tea.Cmd {
	for _, p := range d.params {
		if !isActiveChoiceParam(p.def.Type) {
			continue
		}
		p.gen++
		p.status = resolveLoading
		if field, ok := p.field.(*choiceField); ok {
			field.loading = true
		}
	}
	return tea.Batch(d.loadForm(), d.startSpinner())
}

func (d *BuildParamsDialog) updateFocusedField(msg tea.Msg) tea.Cmd {
	if d.focused < 0 || d.focused >= len(d.params) {
		return nil
	}
	p := d.params[d.focused]
	if p.def.Type == classBoolean || p.status == resolveLoading {
		return nil
	}
	var cmd tea.Cmd
	p.field, cmd = p.field.Update(msg)
	return cmd
}

func (d *BuildParamsDialog) focusNext() tea.Cmd {
	d.params[d.focused].field.Blur()
	d.focused = (d.focused + 1) % len(d.params)
	return d.params[d.focused].field.Focus()
}

func (d *BuildParamsDialog) focusPrev() tea.Cmd {
	d.params[d.focused].field.Blur()
	d.focused--
	if d.focused < 0 {
		d.focused = len(d.params) - 1
	}
	return d.params[d.focused].field.Focus()
}

// param returns the named parameter's state, or nil when the dialog has none.
func (d *BuildParamsDialog) param(name string) *paramState {
	for _, p := range d.params {
		if p.name() == name {
			return p
		}
	}
	return nil
}

// degrade marks p failed with a short reason and swaps in a text input seeded
// with the Jenkins default, so the user can still type a value they know.
func (d *BuildParamsDialog) degrade(p *paramState, reason string) tea.Cmd {
	p.status = resolveFailed
	p.reason = reason
	p.field = newTextInputField(p.def, d.inputWidth())
	if d.params[d.focused] == p {
		return p.field.Focus()
	}
	return nil
}

// degradeDynamicParams falls back to text inputs for every Active Choices
// parameter, as the dialog behaved before dynamic parameters were supported.
func (d *BuildParamsDialog) degradeDynamicParams() tea.Cmd {
	var cmds []tea.Cmd
	for _, p := range d.params {
		if isActiveChoiceParam(p.def.Type) {
			cmds = append(cmds, d.degrade(p, "dynamic choices unavailable; enter a value"))
		}
	}
	return tea.Batch(cmds...)
}

func (d *BuildParamsDialog) inputWidth() int {
	return dialogWidth(d.width) - labelWidth - 8
}

func dialogWidth(windowWidth int) int {
	// Delegates to the shared clamped sizing so the params dialog never overflows a
	// narrow window (the old max(windowWidth/2, 50) floor exceeded small terminals)
	// nor sprawls on a wide one.
	return styles.DialogWidth(windowWidth)
}

func isActiveChoiceParam(paramType string) bool {
	switch paramType {
	case classActiveChoice, classCascadeChoice, classDynamicReference:
		return true
	}
	return false
}

func newTextInputField(def gojenkins.ParameterDefinition, width int) paramField {
	ti := textinput.New()
	ti.Placeholder = def.Name
	ti.CharLimit = 256
	ti.SetWidth(width)
	if def.DefaultParameterValue.Value != nil {
		ti.SetValue(fmt.Sprintf("%v", def.DefaultParameterValue.Value))
	}
	if def.Type == classPassword {
		ti.EchoMode = textinput.EchoPassword
	}
	return &textInputField{model: ti}
}

func newTextAreaField(def gojenkins.ParameterDefinition, width int) paramField {
	ta := textarea.New()
	ta.Placeholder = def.Name
	ta.SetWidth(width)
	ta.SetHeight(textareaHeight)
	ta.MaxHeight = textareaMaxHeight
	if def.DefaultParameterValue.Value != nil {
		ta.SetValue(fmt.Sprintf("%v", def.DefaultParameterValue.Value))
	}
	return &textAreaField{model: ta}
}

// newStaticChoiceField builds a core ChoiceParameterDefinition, whose options
// the REST API already carries.
func newStaticChoiceField(def gojenkins.ParameterDefinition) *choiceField {
	choices := make([]gojenkins.Choice, 0, len(def.Choices))
	for _, value := range def.Choices {
		choices = append(choices, gojenkins.Choice{Value: value, Label: value})
	}
	field := newChoiceField(choices, false)
	if def.DefaultParameterValue.Value != nil {
		field.SetValue(fmt.Sprintf("%v", def.DefaultParameterValue.Value))
	}
	return field
}

func renderParamType(t *styles.Theme, paramType string) string {
	return t.S().Base.Foreground(t.Secondary).PaddingLeft(1).Render(paramTypeLabel(paramType))
}

// paramTypeLabel maps a Jenkins parameter class to a short tag. Active Choices
// classes lack the ParameterDefinition suffix, so they are named explicitly.
func paramTypeLabel(paramType string) string {
	switch paramType {
	case classBoolean:
		return "bool"
	case classActiveChoice:
		return "choice"
	case classCascadeChoice:
		return "cascade"
	case classDynamicReference:
		return "reference"
	}
	return strings.ToLower(strings.TrimSuffix(paramType, "ParameterDefinition"))
}

// renderChoiceField draws a choice as a select control. The key hint appears
// only on the focused field, so an unfocused value reads on its own.
func (d *BuildParamsDialog) renderChoiceField(t *styles.Theme, field *choiceField, focused bool) string {
	muted := t.S().Base.Foreground(t.Grey)
	if field.loading {
		return d.spinner.View() + muted.Render(field.View())
	}
	if field.noChoices() {
		return muted.Italic(true).Render(field.View())
	}
	labels := field.selectedLabels()
	if len(labels) == 0 {
		return muted.Italic(true).Render("▾ none selected")
	}
	value := t.S().Base.Bold(true)
	if focused {
		value = value.Foreground(t.Primary)
	}
	view := muted.Render("▾ ") + value.Render(strings.Join(labels, ", "))
	if focused {
		view += muted.Render(fmt.Sprintf("  %s to choose", d.keymap.OpenPicker.Help().Key))
	}
	return view
}

// unsupportedClasses names the parameter classes the dialog could only offer
// as text inputs. Other degraded fields explain themselves in their reason.
func (d *BuildParamsDialog) unsupportedClasses() []string {
	var classes []string
	for _, p := range d.params {
		if p.def.Type == classDynamicReference && p.status == resolveFailed && !slices.Contains(classes, p.def.Type) {
			classes = append(classes, p.def.Type)
		}
	}
	return classes
}

func renderBoolToggle(t *styles.Theme, value bool, focused bool) string {
	trueStyle := t.S().Base
	falseStyle := t.S().Base

	if value {
		trueStyle = trueStyle.Foreground(t.HighlightForeground).Background(t.Primary)
	} else {
		falseStyle = falseStyle.Foreground(t.HighlightForeground).Background(t.Primary)
	}

	trueBtn := trueStyle.Padding(0, 1).Render("true")
	falseBtn := falseStyle.Padding(0, 1).Render("false")

	toggle := lipgloss.JoinHorizontal(lipgloss.Center, trueBtn, " ", falseBtn)
	if focused {
		toggle += t.S().Base.Foreground(t.Grey).Render("  (space to toggle)")
	}
	return toggle
}
