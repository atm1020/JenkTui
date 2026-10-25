package buildparams

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

type fakeResolver struct {
	parameters []gojenkins.FormParameter
	results    map[string][]gojenkins.Choice
	errors     map[string]error
	mu         sync.Mutex
	calls      []string
}

func (f *fakeResolver) ParametersList() []gojenkins.FormParameter {
	return append([]gojenkins.FormParameter(nil), f.parameters...)
}

func (f *fakeResolver) Resolve(_ context.Context, name string, _ map[string]string) ([]gojenkins.Choice, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
	return f.results[name], f.errors[name]
}

func (f *fakeResolver) Dependents(name string) []string {
	var result []string
	for _, p := range f.parameters {
		for _, parent := range p.ReferencedParameters {
			if parent == name {
				result = append(result, p.Name)
			}
		}
	}
	return result
}

func TestInitLoadsDynamicFormAndResolvesFirstCascade(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "CLOUD", Type: "ChoiceParameter"},
		{Name: "REGION", Type: "CascadeChoiceParameter"},
	})
	resolver := &fakeResolver{
		parameters: []gojenkins.FormParameter{
			{Name: "CLOUD", Type: "ChoiceParameter", Choices: choices("aws", "gcp")},
			{Name: "REGION", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"CLOUD"}},
		},
		results: map[string][]gojenkins.Choice{"REGION": choices("eu", "us")},
		errors:  map[string]error{},
	}
	dialog.formLoader = func(context.Context) (formResolver, error) { return resolver, nil }
	if dialog.Init() == nil {
		t.Fatal("Init() returned no command")
	}

	cmd := dialog.handleFormLoaded(FormLoadedMsg{Gen: dialog.formGen, Form: resolver})
	if _, ok := dialog.params[0].field.(*choiceField); !ok {
		t.Fatalf("CLOUD field = %T, want choiceField", dialog.params[0].field)
	}
	messages := executeCommands(cmd)
	if len(messages) != 1 {
		t.Fatalf("resolve messages = %d, want 1", len(messages))
	}
	loaded := messages[0].(ChoicesLoadedMsg)
	if loaded.Param != "REGION" {
		t.Fatalf("resolved %q, want REGION", loaded.Param)
	}
	dialog.handleChoicesLoaded(loaded)
	if got := dialog.params[1].field.Value(); got != "eu" {
		t.Fatalf("REGION value = %q, want eu", got)
	}
}

func TestParentChangeResolvesOnlyDirectDependents(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "A", Type: "ChoiceParameter"}, {Name: "B", Type: "CascadeChoiceParameter"}, {Name: "C", Type: "CascadeChoiceParameter"}})
	resolver := &fakeResolver{
		parameters: []gojenkins.FormParameter{
			{Name: "A", Type: "ChoiceParameter", Choices: choices("a")},
			{Name: "B", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"A"}},
			{Name: "C", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"B"}},
		},
		results: map[string][]gojenkins.Choice{"B": choices("b"), "C": choices("c")},
		errors:  map[string]error{},
	}
	dialog.form = resolver
	dialog.param("A").status = resolveDone
	dialog.param("B").status = resolveDone
	dialog.param("C").status = resolveDone
	dialog.params[0].field = newChoiceField(choices("a"), false)
	dialog.params[1].field = newChoiceField(choices("b"), false)
	dialog.params[2].field = newChoiceField(choices("c"), false)

	messages := resolveMessages(executeCommands(dialog.propagateChange("A")))
	if len(messages) != 1 || messages[0].Param != "B" {
		t.Fatalf("messages = %#v, want exactly B", messages)
	}
}

func TestStaleChoiceResultIsIgnored(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "REGION", Type: "CascadeChoiceParameter"}})
	dialog.params[0].field = newChoiceField(choices("old"), false)
	dialog.param("REGION").gen = 2
	dialog.handleChoicesLoaded(ChoicesLoadedMsg{Param: "REGION", Gen: 1, Choices: choices("new")})
	if got := dialog.params[0].field.Value(); got != "old" {
		t.Fatalf("stale result changed value to %q", got)
	}
}

func TestSelfReferenceDegradesWithoutResolving(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "A", Type: "CascadeChoiceParameter"}})
	resolver := &fakeResolver{parameters: []gojenkins.FormParameter{{Name: "A", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"A"}}}, results: map[string][]gojenkins.Choice{}, errors: map[string]error{}}
	cmd := dialog.handleFormLoaded(FormLoadedMsg{Form: resolver})
	if cmd == nil || len(executeCommands(cmd)) == 0 {
		t.Fatal("self-reference did not restart the focused fallback cursor")
	}
	if dialog.param("A").status != resolveFailed {
		t.Fatalf("state = %v, want failed", dialog.param("A").status)
	}
}

func TestResolveFailureDegradesOnlyOneField(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "A", Type: "CascadeChoiceParameter"}, {Name: "B", Type: "ChoiceParameter"}})
	dialog.params[0].field = newChoiceField(nil, false)
	dialog.params[1].field = newChoiceField(choices("ok"), false)
	dialog.param("A").gen = 1
	dialog.param("A").status = resolveLoading
	dialog.param("B").status = resolveDone
	dialog.handleChoicesLoaded(ChoicesLoadedMsg{Param: "A", Gen: 1, Err: errors.New("HTTP 500")})
	if _, ok := dialog.params[0].field.(*textInputField); !ok {
		t.Fatalf("failed field = %T, want text input", dialog.params[0].field)
	}
	if _, ok := dialog.params[1].field.(*choiceField); !ok {
		t.Fatalf("sibling field = %T, want choice field", dialog.params[1].field)
	}
	if dialog.hasPendingResolution() {
		fatal := "build remained disabled after a per-field failure"
		t.Fatal(fatal)
	}
}

func TestFormLoadFailureAllowsTypedFallback(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "A", Type: "ChoiceParameter"}})
	dialog.handleFormLoaded(FormLoadedMsg{Err: errors.New("broken form")})
	if _, ok := dialog.params[0].field.(*textInputField); !ok {
		t.Fatalf("field = %T, want text input", dialog.params[0].field)
	}
	if dialog.hasPendingResolution() {
		t.Fatal("build remained disabled after form-load failure")
	}
}

func TestMultiSelectJoinsValuesWithComma(t *testing.T) {
	field := newChoiceField([]gojenkins.Choice{{Value: "plain", Label: "Plain", Selected: true}, {Value: "with,comma", Label: "Comma", Selected: true}}, true)
	if got := field.Value(); got != "plain,with,comma" {
		t.Fatalf("Value() = %q", got)
	}
}

func TestChoicePickerIsCompactAndShowsAllSmallListItems(t *testing.T) {
	field := newChoiceField(choices("dev", "staging", "production"), false)
	picker := newChoicePicker(0, "ENV", field, 60, 20, false, testKeyMap(nil))

	if !picker.list.ShowPagination() || !picker.list.ShowStatusBar() {
		t.Fatal("picker hides its pagination or item count")
	}
	if got := len(picker.list.VisibleItems()); got != 3 {
		t.Fatalf("visible items = %d, want 3", got)
	}
	if got := picker.list.Paginator.TotalPages; got != 1 {
		t.Fatalf("total pages = %d, want every item on one page", got)
	}
	if got := picker.list.Height(); got > 7 {
		t.Fatalf("list height = %d, want a compact picker", got)
	}
}

func TestChoicePickerShowsCountAndPageForLongLists(t *testing.T) {
	values := make([]string, 30)
	for i := range values {
		values[i] = fmt.Sprintf("node-%02d", i+1)
	}
	field := newChoiceField(choices(values...), false)
	picker := newChoicePicker(0, "NODE", field, 60, 14, false, testKeyMap(nil))

	if picker.list.Paginator.TotalPages < 2 {
		t.Fatalf("total pages = %d, want more than one", picker.list.Paginator.TotalPages)
	}
	if !picker.list.ShowPagination() || !picker.list.ShowStatusBar() {
		t.Fatal("picker hides its page indicator or item count")
	}
	if got, page := len(picker.list.Items()), picker.list.Paginator.Page; got != 30 || page != 0 {
		t.Fatalf("items = %d on page %d, want 30 starting on the first page", got, page)
	}
}

func TestPickerFilterMatchesLabelsBySubstring(t *testing.T) {
	field := newChoiceField([]gojenkins.Choice{{Value: "prod-eu", Label: "EU Production"}, {Value: "stage", Label: "Staging"}}, false)
	picker := newChoicePicker(0, "TARGET", field, 60, 20, false, testKeyMap(nil))
	targets := []string{picker.list.Items()[0].FilterValue(), picker.list.Items()[1].FilterValue()}

	if got := substringFilter("production", targets); len(got) != 1 || got[0].Index != 0 {
		t.Fatalf("label filter result = %#v, want EU Production", got)
	}
	if got := substringFilter("prod-eu", targets); len(got) != 0 {
		t.Fatalf("hidden value unexpectedly matched: %#v", got)
	}
}

func TestFilterablePickerOpensWithFilterFocused(t *testing.T) {
	field := newChoiceField(choices("dev", "staging"), false)
	focused := newChoicePicker(0, "ENV", field, 60, 20, true, testKeyMap(nil))
	browsing := newChoicePicker(0, "ENV", field, 60, 20, false, testKeyMap(nil))

	if focused.list.FilterState() != list.Filtering {
		t.Fatalf("filterable state = %v, want filtering", focused.list.FilterState())
	}
	if browsing.list.FilterState() != list.Unfiltered {
		t.Fatalf("ordinary state = %v, want unfiltered", browsing.list.FilterState())
	}
}

func TestPickerSpaceAndEscapeRespectFilterPrecedence(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "TARGETS", Type: "ChoiceParameter"}})
	field := newChoiceField(choices("one", "two"), true)
	dialog.params[0].field = field
	dialog.picker = newChoicePicker(0, "TARGETS", field, 60, 20, true, dialog.keymap)
	original := field.Value()

	dialog.handlePickerKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := dialog.picker.list.FilterValue(); got != " " {
		t.Fatalf("filter value = %q, want a space", got)
	}
	if got := field.Value(); got != original {
		t.Fatalf("space toggled selection while filtering: %q -> %q", original, got)
	}

	dialog.handlePickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if dialog.picker == nil {
		t.Fatal("first escape closed picker instead of clearing filter")
	}
	if got := dialog.picker.list.FilterValue(); got != "" {
		t.Fatalf("filter value after escape = %q, want empty", got)
	}
	dialog.handlePickerKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if dialog.picker != nil {
		t.Fatal("second escape did not close picker")
	}
	messages := executeCommands(dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape}))
	if len(messages) != 1 {
		t.Fatalf("third escape messages = %#v, want dialog close", messages)
	}
	if _, ok := messages[0].(util.CloseDialogMsg); !ok {
		t.Fatalf("third escape message = %T, want CloseDialogMsg", messages[0])
	}
}

func TestPickerSpaceTogglesWhenFilterNotFocused(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "TARGETS", Type: "ChoiceParameter"}})
	field := newChoiceField(choices("one", "two"), true)
	dialog.params[0].field = field
	dialog.picker = newChoicePicker(0, "TARGETS", field, 60, 20, false, dialog.keymap)
	if got := field.Value(); got != "one" {
		t.Fatalf("initial value = %q, want one", got)
	}

	dialog.handlePickerKey(tea.KeyPressMsg{Code: tea.KeyDown})
	dialog.handlePickerKey(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := field.Value(); got != "one,two" {
		t.Fatalf("value after space = %q, want one,two", got)
	}
	if dialog.picker == nil {
		t.Fatal("space closed the multi-select picker")
	}
}

func TestPickerSelectionsSurviveFilterChange(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "SERVICES", Type: "ChoiceParameter"}})
	field := newChoiceField(choices("api", "billing", "cache", "db"), true)
	dialog.params[0].field = field
	dialog.picker = newChoicePicker(0, "SERVICES", field, 60, 20, false, dialog.keymap)
	space := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	press := func(msg tea.KeyPressMsg) { updateUntilIdle(dialog, msg) }

	press(tea.KeyPressMsg{Code: tea.KeyDown})
	press(space) // api, billing
	press(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "db" {
		press(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	press(tea.KeyPressMsg{Code: tea.KeyEnter}) // leave the filter input
	press(space)                               // db
	press(tea.KeyPressMsg{Code: tea.KeyEscape})

	if got := field.Value(); got != "api,billing,db" {
		t.Fatalf("value = %q, want api,billing,db", got)
	}
	if dialog.picker == nil || dialog.picker.list.IsFiltered() {
		t.Fatal("escape should clear the filter and keep the picker open")
	}
}

func TestConfirmBlockedUntilChoicesResolve(t *testing.T) {
	var submitted map[string]string
	conf := config.GetDefaultConfig()
	dialog := newDialog(nil, pendingFormLoader, "demo",
		[]gojenkins.ParameterDefinition{{Name: "CLOUD", Type: "ChoiceParameter"}}, 100,
		func(params map[string]string) tea.Cmd { submitted = params; return nil },
		nil, conf.KeyMapConfig.Dialog.BuildParams)
	ctrlS := tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}

	if cmd := dialog.handleKeyPress(ctrlS); cmd != nil || submitted != nil {
		t.Fatal("build submitted before the form loaded")
	}
	resolver := &fakeResolver{parameters: []gojenkins.FormParameter{{Name: "CLOUD", Type: "ChoiceParameter", Choices: choices("aws", "gcp")}}}
	dialog.handleFormLoaded(FormLoadedMsg{Form: resolver})
	executeCommands(dialog.handleKeyPress(ctrlS))
	if submitted["CLOUD"] != "aws" {
		t.Fatalf("submitted = %#v, want CLOUD=aws", submitted)
	}
}

func TestLoadingSpinnerAnimatesUntilChoicesResolve(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "CLOUD", Type: "ChoiceParameter"},
		{Name: "REGION", Type: "CascadeChoiceParameter"},
	})
	resolver := &fakeResolver{
		parameters: []gojenkins.FormParameter{
			{Name: "CLOUD", Type: "ChoiceParameter", Choices: choices("aws", "gcp")},
			{Name: "REGION", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"CLOUD"}},
		},
		results: map[string][]gojenkins.Choice{"REGION": choices("eu")},
	}
	dialog.formLoader = func(context.Context) (formResolver, error) { return resolver, nil }

	tick := findTick(t, dialog.Init())
	_, cmd := dialog.Update(tick)
	if cmd == nil {
		t.Fatal("spinner stopped ticking while the form was loading")
	}

	regionLoaded := resolveMessages(executeCommands(dialog.handleFormLoaded(FormLoadedMsg{Gen: dialog.formGen, Form: resolver})))
	_, cmd = dialog.Update(findTick(t, cmd))
	if cmd == nil {
		t.Fatal("spinner stopped ticking while REGION was resolving")
	}
	nextTick := findTick(t, cmd)

	dialog.handleChoicesLoaded(regionLoaded[0])
	if _, cmd = dialog.Update(nextTick); cmd != nil {
		t.Fatal("spinner kept ticking after every field resolved")
	}
}

func TestLoadingSpinnerRestartsWhenParentChanges(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "CLOUD", Type: "ChoiceParameter"},
		{Name: "REGION", Type: "CascadeChoiceParameter"},
	})
	resolver := &fakeResolver{
		parameters: []gojenkins.FormParameter{
			{Name: "CLOUD", Type: "ChoiceParameter", Choices: choices("aws", "gcp")},
			{Name: "REGION", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"CLOUD"}},
		},
		results: map[string][]gojenkins.Choice{"REGION": choices("eu")},
	}
	dialog.form = resolver
	dialog.param("CLOUD").status = resolveDone
	dialog.param("REGION").status = resolveDone
	dialog.params[0].field = newChoiceField(choices("aws", "gcp"), false)
	dialog.params[1].field = newChoiceField(choices("eu"), false)

	findTick(t, dialog.propagateChange("CLOUD"))
}

func resolveMessages(messages []tea.Msg) []ChoicesLoadedMsg {
	var result []ChoicesLoadedMsg
	for _, msg := range messages {
		if loaded, ok := msg.(ChoicesLoadedMsg); ok {
			result = append(result, loaded)
		}
	}
	return result
}

// findTick runs cmd and returns the spinner tick it schedules, failing when
// there is none. Ticks sleep for one frame, so only call it on short chains.
func findTick(t *testing.T, cmd tea.Cmd) spinner.TickMsg {
	t.Helper()
	for _, msg := range executeCommands(cmd) {
		if tick, ok := msg.(spinner.TickMsg); ok {
			return tick
		}
	}
	t.Fatal("no spinner tick scheduled")
	return spinner.TickMsg{}
}

func TestParamTypeLabelsAreShortAndReadable(t *testing.T) {
	cases := map[string]string{
		"StringParameterDefinition":  "string",
		"BooleanParameterDefinition": "bool",
		"ChoiceParameterDefinition":  "choice",
		"ChoiceParameter":            "choice",
		"CascadeChoiceParameter":     "cascade",
		"DynamicReferenceParameter":  "reference",
		"RunParameterDefinition":     "run",
	}
	for jenkinsType, want := range cases {
		if got := paramTypeLabel(jenkinsType); got != want {
			t.Errorf("paramTypeLabel(%q) = %q, want %q", jenkinsType, got, want)
		}
	}
}

func TestChoiceHintFollowsFocusAndKeymap(t *testing.T) {
	conf := config.GetDefaultConfig().KeyMapConfig.Dialog.BuildParams
	conf.OpenPicker = []string{"o"}
	dialog := NewBuildParamsDialog(nil, nil, "demo", []gojenkins.ParameterDefinition{
		{Name: "CLOUD", Type: "ChoiceParameterDefinition", Choices: []string{"aws"}},
		{Name: "REGION", Type: "ChoiceParameterDefinition", Choices: []string{"eu"}},
	}, 100, func(map[string]string) tea.Cmd { return nil }, nil, conf)
	dialog.params[0].field.Focus()

	if got := strings.Count(dialog.View(), "o to choose"); got != 1 {
		t.Fatalf("hint count = %d, want 1 (focused field only, configured key)", got)
	}
}

func TestMultiSelectEnterWithoutTogglingSelectsHighlighted(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "NAMESPACES", Type: "CascadeChoiceParameter"}})
	field := newChoiceField(choices("default", "canary", "payments"), true)
	dialog.params[0].field = field
	dialog.param("NAMESPACES").status = resolveDone
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if dialog.picker == nil {
		t.Fatal("enter did not open the picker")
	}

	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if dialog.picker != nil {
		t.Fatal("enter did not close the picker")
	}
	if got := field.Value(); got != "payments" {
		t.Fatalf("value = %q, want payments", got)
	}
}

func TestMultiSelectEnterAfterTogglingKeepsToggledSet(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "NAMESPACES", Type: "CascadeChoiceParameter"}})
	field := newChoiceField(choices("default", "canary", "payments"), true)
	dialog.params[0].field = field
	dialog.param("NAMESPACES").status = resolveDone
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := field.Value(); got != "default,canary" {
		t.Fatalf("value = %q, want default,canary", got)
	}
}

func TestResolveErrorsExplainTheCause(t *testing.T) {
	cases := map[error]string{
		fmt.Errorf("resolve: %w", gojenkins.ErrNoChoices):      "no choices",
		fmt.Errorf("resolve: %w", gojenkins.ErrStaleBuildForm): "form expired; reload",
		errors.New("HTTP 500"):                                 "HTTP 500",
	}
	for err, want := range cases {
		if got := shortResolveError(err); got != want {
			t.Errorf("shortResolveError(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestChoiceWithoutMatchingDefaultSelectsFirstOption(t *testing.T) {
	for _, def := range []string{"", "not-an-option"} {
		param := gojenkins.ParameterDefinition{Name: "ENV", Type: "ChoiceParameterDefinition", Choices: []string{"dev", "prod"}}
		param.DefaultParameterValue.Value = def
		dialog := testDialog([]gojenkins.ParameterDefinition{param})
		if got := dialog.params[0].field.Value(); got != "dev" {
			t.Errorf("default %q: value = %q, want dev", def, got)
		}
	}
}

func TestChoiceDefaultPrefersSelectedMarker(t *testing.T) {
	field := newChoiceField([]gojenkins.Choice{{Value: "a"}, {Value: "b", Selected: true}}, false)
	field.SetValue("missing")
	if got := field.Value(); got != "b" {
		t.Fatalf("value = %q, want the :selected choice b", got)
	}
}

func TestSingleSelectValueWithCommaRoundTrips(t *testing.T) {
	field := newChoiceField(choices("a,b", "c"), false)
	field.SetValue("a,b")
	if got := field.Value(); got != "a,b" {
		t.Fatalf("value = %q, want a,b", got)
	}
}

func TestCommaBearingMultiSelectValueWarnsOnce(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Default()
	log.SetDefault(log.New(&buf))
	defer log.SetDefault(previous)

	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "TARGETS", Type: "ChoiceParameter"}})
	dialog.params[0].field = newChoiceField([]gojenkins.Choice{{Value: "plain", Selected: true}, {Value: "with,comma", Selected: true}}, true)
	dialog.params[0].status = resolveDone
	executeCommands(dialog.handleKeyPress(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}))

	if got := strings.Count(buf.String(), "ambiguous comma"); got != 1 {
		t.Fatalf("warnings = %d, want 1; log:\n%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "with,comma") {
		t.Fatalf("warning does not name the value; log:\n%s", buf.String())
	}
}

func TestEmptyResolutionShowsNoChoices(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "REGION", Type: "CascadeChoiceParameter"}})
	dialog.params[0].gen = 1
	dialog.params[0].status = resolveLoading
	dialog.handleChoicesLoaded(ChoicesLoadedMsg{Param: "REGION", Gen: 1, Choices: []gojenkins.Choice{}})

	field, ok := dialog.params[0].field.(*choiceField)
	if !ok || !field.noChoices() {
		t.Fatalf("field = %#v, want the no-choices state", dialog.params[0].field)
	}
	if dialog.hasPendingResolution() {
		t.Fatal("an empty resolution blocked Build")
	}
}

func TestPickerFilterWithNoResultsIsNoMatch(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "ENV", Type: "ChoiceParameter"}})
	field := newChoiceField(choices("dev", "prod"), false)
	dialog.params[0].field = field
	dialog.picker = newChoicePicker(0, "ENV", field, 60, 20, true, dialog.keymap)
	if dialog.picker.noMatch() {
		t.Fatal("an empty filter reported no match")
	}

	for _, r := range "zzz" {
		updateUntilIdle(dialog, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !dialog.picker.noMatch() {
		t.Fatal("a filter matching nothing did not report no match")
	}
}

func TestBannerNamesUnsupportedParameterClass(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "SUMMARY", Type: "DynamicReferenceParameter"},
		{Name: "CLOUD", Type: "ChoiceParameter"},
	})
	resolver := &fakeResolver{parameters: []gojenkins.FormParameter{
		{Name: "SUMMARY", Type: "DynamicReferenceParameter"},
		{Name: "CLOUD", Type: "ChoiceParameter", Choices: choices("aws")},
	}}
	dialog.handleFormLoaded(FormLoadedMsg{Form: resolver})

	if got := dialog.unsupportedClasses(); len(got) != 1 || got[0] != "DynamicReferenceParameter" {
		t.Fatalf("unsupported classes = %v, want [DynamicReferenceParameter]", got)
	}
}

func TestCleanUpReleasesFormAndIsIdempotent(t *testing.T) {
	dialog := loadedCascadeDialog(t, cascadeResolver())
	dialog.handleOpenPicker()

	for range 2 {
		if cmd := dialog.CleanUp(); cmd != nil {
			t.Fatal("CleanUp returned a command")
		}
	}
	if dialog.form != nil || dialog.picker != nil {
		t.Fatal("CleanUp kept the form session or picker")
	}
}

func TestOmitValueFieldReferenceParamsAreNotSubmitted(t *testing.T) {
	var submitted map[string]string
	conf := config.GetDefaultConfig()
	dialog := newDialog(nil, pendingFormLoader, "demo", []gojenkins.ParameterDefinition{
		{Name: "PLAN_SUMMARY", Type: "DynamicReferenceParameter"},
		{Name: "OPERATOR_NOTE", Type: "DynamicReferenceParameter"},
	}, 100, func(params map[string]string) tea.Cmd { submitted = params; return nil }, nil, conf.KeyMapConfig.Dialog.BuildParams)
	dialog.handleFormLoaded(FormLoadedMsg{Form: &fakeResolver{parameters: []gojenkins.FormParameter{
		{Name: "PLAN_SUMMARY", Type: "DynamicReferenceParameter", OmitValueField: true},
		{Name: "OPERATOR_NOTE", Type: "DynamicReferenceParameter"},
	}}})
	dialog.params[1].field.SetValue("typed note")

	executeCommands(dialog.handleKeyPress(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}))

	if _, ok := submitted["PLAN_SUMMARY"]; ok {
		t.Fatalf("omitValueField parameter was submitted: %#v", submitted)
	}
	if submitted["OPERATOR_NOTE"] != "typed note" {
		t.Fatalf("submitted = %#v, want OPERATOR_NOTE=typed note", submitted)
	}
}

func TestFormChoiceTypeMakesMultiSelectFields(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "CLUSTER", Type: "ChoiceParameter"},
		{Name: "NAMESPACES", Type: "CascadeChoiceParameter"},
	})
	resolver := &fakeResolver{
		parameters: []gojenkins.FormParameter{
			{Name: "CLUSTER", Type: "ChoiceParameter", Choices: choices("k8s-01")},
			{Name: "NAMESPACES", Type: "CascadeChoiceParameter", Multiple: true, ReferencedParameters: []string{"CLUSTER"}},
		},
		results: map[string][]gojenkins.Choice{"NAMESPACES": {
			{Value: "default", Label: "default", Selected: true},
			{Value: "canary", Label: "canary", Selected: true},
			{Value: "payments", Label: "payments"},
		}},
	}
	for _, msg := range resolveMessages(executeCommands(dialog.handleFormLoaded(FormLoadedMsg{Form: resolver}))) {
		dialog.handleChoicesLoaded(msg)
	}

	field := dialog.params[1].field.(*choiceField)
	if !field.multiple {
		t.Fatal("NAMESPACES is not multi-select")
	}
	if got := field.Value(); got != "default,canary" {
		t.Fatalf("value = %q, want every :selected choice", got)
	}
	if dialog.params[0].field.(*choiceField).multiple {
		t.Fatal("a single-select parameter became multi-select")
	}
}

func TestFilterableFormParameterOpensPickerFiltering(t *testing.T) {
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "DATACENTER", Type: "ChoiceParameter"},
		{Name: "ENVIRONMENT", Type: "ChoiceParameter"},
	})
	dialog.handleFormLoaded(FormLoadedMsg{Form: &fakeResolver{parameters: []gojenkins.FormParameter{
		{Name: "DATACENTER", Type: "ChoiceParameter", Filterable: true, Choices: choices("ams1", "fra1")},
		{Name: "ENVIRONMENT", Type: "ChoiceParameter", Choices: choices("dev01", "dev02")},
	}}})

	dialog.handleOpenPicker()
	if dialog.picker == nil || dialog.picker.list.FilterState() != list.Filtering {
		t.Fatal("filterable DATACENTER did not open with the filter focused")
	}
	dialog.picker = nil
	dialog.focusNext()
	dialog.handleOpenPicker()
	if dialog.picker == nil || dialog.picker.list.FilterState() != list.Unfiltered {
		t.Fatal("ENVIRONMENT opened filtering although it is not filterable")
	}
}

// testKeyMap builds the dialog keymap from the default config after edit
// adjusts it, so tests can rebind keys the way a user's config.json would.
func testKeyMap(edit func(*appcontext.BuildParamsDialogKeyMapConfig)) KeyMap {
	conf := config.GetDefaultConfig().KeyMapConfig.Dialog.BuildParams
	if edit != nil {
		edit(&conf)
	}
	return UserKeyMap(conf)
}

func reboundPickerDialog(t *testing.T, filtering bool) *BuildParamsDialog {
	t.Helper()
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "ENV", Type: "ChoiceParameter"}})
	dialog.keymap = testKeyMap(func(c *appcontext.BuildParamsDialogKeyMapConfig) {
		c.Up = appcontext.KeyBinding{"ctrl+p"}
		c.Down = appcontext.KeyBinding{"ctrl+n"}
		c.OpenPicker = appcontext.KeyBinding{"ctrl+o"}
		c.Close = appcontext.KeyBinding{"ctrl+q"}
	})
	field := newChoiceField(choices("dev", "stage", "prod"), false)
	dialog.params[0].field = field
	dialog.params[0].status = resolveDone
	dialog.picker = newChoicePicker(0, "ENV", field, 60, 20, filtering, dialog.keymap)
	return dialog
}

func TestPickerWhileFilteringUsesConfiguredKeys(t *testing.T) {
	dialog := reboundPickerDialog(t, true)

	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := dialog.picker.list.Index(); got != 0 {
		t.Fatalf("unbound down moved the cursor to %d", got)
	}
	dialog.handleKeyPress(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl})
	dialog.handleKeyPress(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if got := dialog.picker.list.Index(); got != 1 {
		t.Fatalf("cursor = %d, want 1 after ctrl+n, ctrl+n, ctrl+p", got)
	}

	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter})
	if dialog.picker == nil {
		t.Fatal("unbound enter selected while filtering")
	}
	dialog.handleKeyPress(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if dialog.picker != nil || dialog.params[0].field.Value() != "stage" {
		t.Fatalf("ctrl+o while filtering: picker open=%v value=%q, want closed with stage", dialog.picker != nil, dialog.params[0].field.Value())
	}
}

func TestPickerCloseKeyIsConfigured(t *testing.T) {
	dialog := reboundPickerDialog(t, false)

	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})
	if dialog.picker == nil {
		t.Fatal("unbound esc closed the picker")
	}
	dialog.handleKeyPress(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if dialog.picker != nil {
		t.Fatal("configured ctrl+q did not close the picker")
	}
}

func TestPickerPagingKeysComeFromConfig(t *testing.T) {
	values := make([]string, 30)
	for i := range values {
		values[i] = fmt.Sprintf("node-%02d", i+1)
	}
	dialog := testDialog([]gojenkins.ParameterDefinition{{Name: "NODE", Type: "ChoiceParameter"}})
	dialog.keymap = testKeyMap(func(c *appcontext.BuildParamsDialogKeyMapConfig) {
		c.NextPage = appcontext.KeyBinding{"ctrl+f"}
	})
	field := newChoiceField(choices(values...), false)
	dialog.params[0].field = field
	dialog.picker = newChoicePicker(0, "NODE", field, 60, 14, false, dialog.keymap)

	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if got := dialog.picker.list.Paginator.Page; got != 0 {
		t.Fatalf("unbound pgdown changed the page to %d", got)
	}
	dialog.handleKeyPress(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if got := dialog.picker.list.Paginator.Page; got != 1 {
		t.Fatalf("page = %d after configured ctrl+f, want 1", got)
	}
	dialog.handleKeyPress(tea.KeyPressMsg{Code: tea.KeyEnd})
	if got, last := dialog.picker.list.Index(), len(values)-1; got != last {
		t.Fatalf("cursor = %d after default end, want %d", got, last)
	}
}

func TestPickerHintComesFromConfiguredBindings(t *testing.T) {
	keymap := testKeyMap(func(c *appcontext.BuildParamsDialogKeyMapConfig) {
		c.ToggleChoice = appcontext.KeyBinding{"x"}
		c.OpenPicker = appcontext.KeyBinding{"ctrl+o"}
		c.Close = appcontext.KeyBinding{"ctrl+q"}
		c.Filter = appcontext.KeyBinding{"f"}
	})
	hint := pickerHint(true, keymap)
	for _, want := range []string{"x toggle", "ctrl+o select", "ctrl+q close", "f filter"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint %q lacks %q", hint, want)
		}
	}
	if strings.Contains(hint, "enter") || strings.Contains(hint, "esc") || strings.Contains(hint, "/") {
		t.Errorf("hint %q still names a default key", hint)
	}

	unbound := testKeyMap(func(c *appcontext.BuildParamsDialogKeyMapConfig) { c.Filter = nil })
	if hint := pickerHint(false, unbound); strings.Contains(hint, "filter") {
		t.Errorf("hint %q offers a filter key that is not bound", hint)
	}
}

// testDialog builds a dialog whose form loader is present but never used
// unless a test calls Init; tests deliver FormLoadedMsg themselves.
func testDialog(params []gojenkins.ParameterDefinition) *BuildParamsDialog {
	conf := config.GetDefaultConfig()
	return newDialog(nil, pendingFormLoader, "demo", params, 100, func(map[string]string) tea.Cmd { return nil }, nil, conf.KeyMapConfig.Dialog.BuildParams)
}

func pendingFormLoader(context.Context) (formResolver, error) {
	return nil, errors.New("test dialog has no form")
}

func TestNoFormLoaderDegradesDynamicParamsToTextInputs(t *testing.T) {
	var submitted map[string]string
	conf := config.GetDefaultConfig()
	cloud := gojenkins.ParameterDefinition{Name: "CLOUD", Type: "ChoiceParameter"}
	cloud.DefaultParameterValue.Value = "aws"
	dialog := NewBuildParamsDialog(nil, nil, "demo", []gojenkins.ParameterDefinition{cloud}, 100,
		func(params map[string]string) tea.Cmd { submitted = params; return nil },
		nil, conf.KeyMapConfig.Dialog.BuildParams)

	if _, ok := dialog.params[0].field.(*textInputField); !ok {
		t.Fatalf("field = %T, want text input", dialog.params[0].field)
	}
	executeCommands(dialog.handleKeyPress(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}))
	if submitted["CLOUD"] != "aws" {
		t.Fatalf("submitted = %#v, want CLOUD=aws", submitted)
	}
}

func cascadeResolver() *fakeResolver {
	return &fakeResolver{
		parameters: []gojenkins.FormParameter{
			{Name: "CLOUD", Type: "ChoiceParameter", Choices: choices("aws", "gcp")},
			{Name: "REGION", Type: "CascadeChoiceParameter", ReferencedParameters: []string{"CLOUD"}},
		},
		results: map[string][]gojenkins.Choice{"REGION": choices("eu", "us")},
	}
}

func loadedCascadeDialog(t *testing.T, resolver *fakeResolver) *BuildParamsDialog {
	t.Helper()
	dialog := testDialog([]gojenkins.ParameterDefinition{
		{Name: "CLOUD", Type: "ChoiceParameter"},
		{Name: "REGION", Type: "CascadeChoiceParameter"},
	})
	dialog.formLoader = func(context.Context) (formResolver, error) { return resolver, nil }
	for _, msg := range resolveMessages(executeCommands(dialog.handleFormLoaded(FormLoadedMsg{Form: resolver}))) {
		dialog.handleChoicesLoaded(msg)
	}
	return dialog
}

func TestReloadDropsChoicesRequestedBeforeIt(t *testing.T) {
	resolver := cascadeResolver()
	dialog := loadedCascadeDialog(t, resolver)
	dialog.params[0].field.SetValue("gcp")
	inFlight := resolveMessages(executeCommands(dialog.propagateChange("CLOUD")))
	if len(inFlight) != 1 {
		t.Fatalf("in-flight resolves = %d, want 1", len(inFlight))
	}

	dialog.handleReload()
	stale := inFlight[0]
	stale.Choices = choices("stale")
	dialog.handleChoicesLoaded(stale)

	for _, choice := range dialog.params[1].field.(*choiceField).choices {
		if choice.Value == "stale" {
			t.Fatal("a result requested before the reload was applied")
		}
	}
}

func TestFormLoadedBeforeTheLatestReloadIsIgnored(t *testing.T) {
	older, newer := cascadeResolver(), cascadeResolver()
	newer.parameters[0].Choices = choices("azure")
	dialog := loadedCascadeDialog(t, older)

	first := FormLoadedMsg{DialogID: dialog.id, Gen: dialog.formGen + 1, Form: older}
	dialog.handleReload()
	second := FormLoadedMsg{DialogID: dialog.id, Gen: dialog.formGen + 1, Form: newer}
	dialog.handleReload()
	second.Gen = dialog.formGen

	dialog.handleFormLoaded(second)
	dialog.handleFormLoaded(first)

	if got := dialog.params[0].field.Value(); got != "azure" {
		t.Fatalf("CLOUD = %q, want azure from the latest form", got)
	}
}

func TestReloadKeepsSelectionsThatStillExist(t *testing.T) {
	resolver := cascadeResolver()
	dialog := loadedCascadeDialog(t, resolver)
	dialog.params[0].field.SetValue("gcp")
	dialog.params[1].field.SetValue("us")

	var reloaded FormLoadedMsg
	for _, msg := range executeCommands(dialog.handleReload()) {
		if loaded, ok := msg.(FormLoadedMsg); ok {
			reloaded = loaded
		}
	}
	for _, msg := range resolveMessages(executeCommands(dialog.handleFormLoaded(reloaded))) {
		dialog.handleChoicesLoaded(msg)
	}

	if got := dialog.params[0].field.Value(); got != "gcp" {
		t.Fatalf("CLOUD after reload = %q, want gcp", got)
	}
	if got := dialog.params[1].field.Value(); got != "us" {
		t.Fatalf("REGION after reload = %q, want us", got)
	}
}

func choices(values ...string) []gojenkins.Choice {
	result := make([]gojenkins.Choice, 0, len(values))
	for _, value := range values {
		result = append(result, gojenkins.Choice{Value: value, Label: value})
	}
	return result
}

// updateUntilIdle feeds msg and every message its commands produce back into
// the dialog, as the Bubble Tea runtime would. Bubbles' list filters through
// a command, so filtered results only exist after this round trip.
func updateUntilIdle(dialog *BuildParamsDialog, msg tea.Msg) {
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		_, cmd := dialog.Update(next)
		for _, produced := range executeCommands(cmd) {
			if _, ok := produced.(list.FilterMatchesMsg); ok {
				queue = append(queue, produced)
			}
		}
	}
}

func executeCommands(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var messages []tea.Msg
	for _, child := range batch {
		messages = append(messages, executeCommands(child)...)
	}
	return messages
}
