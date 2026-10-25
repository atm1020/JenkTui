package buildparams

import (
	stdctx "context"
	"strings"

	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

// paramField abstracts over textinput and textarea.
type paramField interface {
	Focus() tea.Cmd
	Blur()
	View() string
	Value() string
	SetValue(s string)
	Update(msg tea.Msg) (paramField, tea.Cmd)
}

// textInputField wraps textinput.Model.
type textInputField struct {
	model textinput.Model
}

func (f *textInputField) Focus() tea.Cmd    { return f.model.Focus() }
func (f *textInputField) Blur()             { f.model.Blur() }
func (f *textInputField) View() string      { return f.model.View() }
func (f *textInputField) Value() string     { return f.model.Value() }
func (f *textInputField) SetValue(s string) { f.model.SetValue(s) }

func (f *textInputField) Update(msg tea.Msg) (paramField, tea.Cmd) {
	var cmd tea.Cmd
	f.model, cmd = f.model.Update(msg)
	return f, cmd
}

// textAreaField wraps textarea.Model.
type textAreaField struct {
	model textarea.Model
}

func (f *textAreaField) Focus() tea.Cmd    { return f.model.Focus() }
func (f *textAreaField) Blur()             { f.model.Blur() }
func (f *textAreaField) View() string      { return "\n" + f.model.View() }
func (f *textAreaField) Value() string     { return f.model.Value() }
func (f *textAreaField) SetValue(s string) { f.model.SetValue(s) }

func (f *textAreaField) Update(msg tea.Msg) (paramField, tea.Cmd) {
	var cmd tea.Cmd
	f.model, cmd = f.model.Update(msg)
	return f, cmd
}

// choiceField stores display labels separately from submitted values.
type choiceField struct {
	choices  []gojenkins.Choice
	selected map[int]bool
	multiple bool
	focused  bool
	loading  bool
	// want is the last value asked for. A cascading field has no choices
	// until it resolves, so SetChoices applies want once they arrive.
	want string
}

func newChoiceField(choices []gojenkins.Choice, multiple bool) *choiceField {
	f := &choiceField{multiple: multiple, selected: make(map[int]bool)}
	f.SetChoices(choices)
	return f
}

func (f *choiceField) Focus() tea.Cmd { f.focused = true; return nil }
func (f *choiceField) Blur()          { f.focused = false }
func (f *choiceField) Value() string {
	var values []string
	for i, choice := range f.choices {
		if f.selected[i] {
			values = append(values, choice.Value)
		}
	}
	return strings.Join(values, ",")
}

// SetValue selects the choices whose values match. Only a multi-select value
// is comma-split; a single-select value may itself contain a comma. When
// nothing matches, the default pick applies.
func (f *choiceField) SetValue(value string) {
	f.want = value
	wanted := map[string]bool{value: true}
	if f.multiple {
		wanted = make(map[string]bool)
		for _, item := range strings.Split(value, ",") {
			wanted[item] = true
		}
	}
	clear(f.selected)
	for i, choice := range f.choices {
		if wanted[choice.Value] && !choice.Disabled {
			f.selected[i] = true
			if !f.multiple {
				break
			}
		}
	}
	if len(f.selected) == 0 {
		f.selectDefault()
	}
}

// selectDefault applies the spec's default pick: the first :selected choice
// (every one, for a multi-select), else the first enabled choice, else none.
func (f *choiceField) selectDefault() {
	for i, choice := range f.choices {
		if choice.Selected && !choice.Disabled {
			f.selected[i] = true
			if !f.multiple {
				return
			}
		}
	}
	if len(f.selected) != 0 {
		return
	}
	for i, choice := range f.choices {
		if !choice.Disabled {
			f.selected[i] = true
			return
		}
	}
}

func (f *choiceField) Update(tea.Msg) (paramField, tea.Cmd) { return f, nil }
func (f *choiceField) View() string {
	if f.loading {
		return "loading choices…"
	}
	if f.noChoices() {
		return "no choices"
	}
	labels := f.selectedLabels()
	if len(labels) == 0 {
		return "(no choice selected)"
	}
	return strings.Join(labels, ", ")
}

// noChoices reports a resolved field whose script returned an empty list.
func (f *choiceField) noChoices() bool {
	return !f.loading && len(f.choices) == 0
}

func (f *choiceField) selectedLabels() []string {
	var labels []string
	for i, choice := range f.choices {
		if f.selected[i] {
			labels = append(labels, choice.Label)
		}
	}
	return labels
}

// SetChoices replaces the options, keeping the current (or last wanted)
// value where the new options still contain it.
func (f *choiceField) SetChoices(choices []gojenkins.Choice) {
	old := f.Value()
	if old == "" {
		old = f.want
	}
	f.choices = append([]gojenkins.Choice(nil), choices...)
	f.SetValue(old)
}

// warnAmbiguousCommas logs each selected multi-select value that contains a
// comma. The plugin's wire format joins multi-select values with commas and
// cannot escape them, so the value is kept as-is and the ambiguity surfaced.
func (f *choiceField) warnAmbiguousCommas(parameter string) {
	if !f.multiple {
		return
	}
	for i, choice := range f.choices {
		if f.selected[i] && strings.Contains(choice.Value, ",") {
			log.Warn("ambiguous comma in Active Choices multi-select value", "parameter", parameter, "value", choice.Value)
		}
	}
}

type formResolver interface {
	ParametersList() []gojenkins.FormParameter
	Resolve(stdctx.Context, string, map[string]string) ([]gojenkins.Choice, error)
	Dependents(string) []string
}

// buildFormResolver adapts *gojenkins.BuildForm, whose parameters are a field
// rather than a method, to formResolver.
type buildFormResolver struct {
	*gojenkins.BuildForm
}

func (r buildFormResolver) ParametersList() []gojenkins.FormParameter {
	return r.Parameters
}

// paramState is everything the dialog tracks for one build parameter.
type paramState struct {
	def   gojenkins.ParameterDefinition
	field paramField
	// boolValue backs a BooleanParameterDefinition toggle.
	boolValue bool
	status    resolveState
	// gen is bumped per resolve request; a ChoicesLoadedMsg carrying an older
	// gen is stale and dropped.
	gen uint64
	// reason is the short warning shown under a degraded field.
	reason string
	// filterable opens the picker with its filter focused (the plugin's
	// filterable option); search is available either way.
	filterable bool
	// omitValue marks a reference parameter Jenkins renders with
	// omitValueField: it is display-only and left out of the build request.
	omitValue bool
}

func (p *paramState) name() string { return p.def.Name }

type BuildParamsDialog struct {
	width  int
	height int
	id     uint64

	params           []*paramState
	focused          int
	keymap           KeyMap
	help             help.Model
	onBuild          func(params map[string]string) tea.Cmd
	onCancel         tea.Cmd
	jobName          string
	hasDynamicParams bool
	program          *context.ProgramContext
	form             formResolver
	formLoader       func(stdctx.Context) (formResolver, error)
	// formGen counts form loads; a FormLoadedMsg from an earlier one is stale.
	formGen uint64
	picker  *choicePicker
	spinner spinner.Model
	// spinning is true while a tick chain is live, so a restart never runs a
	// second chain alongside it.
	spinning bool
}
