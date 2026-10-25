package globalsearch

import (
	"testing"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func testKeymapConf() context.GlobalsearchDialogKeyMapConfig {
	return context.GlobalsearchDialogKeyMapConfig{
		Help:        context.KeyBinding{"ctrl+g"},
		Quit:        context.KeyBinding{"ctrl+c"},
		ToggleFocus: context.KeyBinding{"tab"},
		FocusUp:     context.KeyBinding{"ctrl+k"},
		FocusDown:   context.KeyBinding{"ctrl+j"},
		Search:      context.KeyBinding{"enter"},
		ClearInput:  context.KeyBinding{"ctrl+r"},
		Down:        context.KeyBinding{"j"},
		Up:          context.KeyBinding{"k"},
		NextPage:    context.KeyBinding{"l"},
		PrevPage:    context.KeyBinding{"h"},
		Select:      context.KeyBinding{"enter"},
		Filter:      context.KeyBinding{"/"},
	}
}

func newResultModelWithItems(t *testing.T, names ...string) *SearchResultModel {
	t.Helper()
	m := NewSearcResultModel(nil, 40, 12, testKeymapConf())
	items := make([]list.Item, 0, len(names))
	for _, n := range names {
		items = append(items, NewSearchResultItem(&gojenkins.Suggestion{Name: n}))
	}
	m.list.SetItems(items)
	return m
}

func TestFilterKeyStartsFiltering(t *testing.T) {
	m := newResultModelWithItems(t, "alpha", "beta", "gamma")

	if m.IsFiltering() {
		t.Fatal("model should not be filtering before the filter key is pressed")
	}

	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})

	if !m.IsFiltering() {
		t.Fatal("filter key did not put the results list into filtering state")
	}
}

// TestFilterReceivesTypedQuery verifies the routing fix: once filtering starts,
// keystrokes reach the list's filter input (bubbles then does the async matching
// itself). Asserting the filter input value is synchronous, unlike VisibleItems
// which only updates after the list's FilterMatchesMsg command runs.
func TestFilterReceivesTypedQuery(t *testing.T) {
	m := newResultModelWithItems(t, "alpha", "beta", "gamma")

	m, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "alp" {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	if !m.IsFiltering() {
		t.Fatal("model stopped filtering while the query was being typed")
	}
	if got := m.list.FilterInput.Value(); got != "alp" {
		t.Fatalf("filter input did not receive typed query: got %q, want %q", got, "alp")
	}
}

func TestSelect_NonJobURLStillEmitsTarget(t *testing.T) {
	tests := []struct {
		name       string
		resultName string
		url        string
	}{
		{name: "node", resultName: "Built-In Node", url: "/computer/(built-in)/"},
		{name: "view", resultName: "All", url: "/"},
		{name: "user", resultName: "admin", url: "/user/admin"},
		{name: "administration_page", resultName: "Manage Jenkins", url: "/manage"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSearcResultModel(
				&context.ProgramContext{Jenkins: gojenkins.CreateJenkins(nil, "http://jenkins")},
				40, 12, testKeymapConf(),
			)
			m.list.SetItems([]list.Item{NewSearchResultItem(&gojenkins.Suggestion{
				Name: tt.resultName,
				URL:  tt.url,
			})})

			cmd := m.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("selecting a result produced no command")
			}
			msg, ok := cmd().(SearchResultItemSelectedMsg)
			if !ok {
				t.Fatalf("got %T, want SearchResultItemSelectedMsg", cmd())
			}
			if msg.Target.Name != tt.resultName {
				t.Errorf("Name = %q, want %q", msg.Target.Name, tt.resultName)
			}
			if want := "http://jenkins" + tt.url; msg.Target.URL != want {
				t.Errorf("URL = %q, want %q", msg.Target.URL, want)
			}
		})
	}
}

func TestResultSelectedMsg_EmptyNameStillEmitsTarget(t *testing.T) {
	m := NewSearcResultModel(
		&context.ProgramContext{Jenkins: gojenkins.CreateJenkins(nil, "http://jenkins")},
		40, 12, testKeymapConf(),
	)

	msg := m.resultSelectedMsg("", "/computer/(built-in)/", jobref.KindUnknown)
	got, ok := msg.(SearchResultItemSelectedMsg)
	if !ok {
		t.Fatalf("got %T, want SearchResultItemSelectedMsg", msg)
	}
	if got.Target.Name != "" {
		t.Errorf("Name = %q, want empty", got.Target.Name)
	}
	if want := "http://jenkins/computer/(built-in)/"; got.Target.URL != want {
		t.Errorf("URL = %q, want %q", got.Target.URL, want)
	}
}

// TestSelect_PreservesSuggestionClassification pins how a search hit becomes
// a navigation target. When Jenkins reports a class in "type", a searched-for
// folder can open straight into its container page. Anything that isn't a class
// JenkTui recognises stays KindUnknown and takes the discovery path.
func TestSelect_PreservesSuggestionClassification(t *testing.T) {
	tests := []struct {
		name           string
		suggestionType string
		wantKind       jobref.Kind
	}{
		{"folder_class", "com.cloudbees.hudson.plugins.folder.Folder", jobref.KindFolder},
		{"multibranch_class", "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", jobref.KindMultiBranch},
		{"pipeline_class", "org.jenkinsci.plugins.workflow.job.WorkflowJob", jobref.KindPipeline},
		{"unrecognised_type_falls_back_to_discovery", "symbol-folder", jobref.KindUnknown},
		{"absent_type_falls_back_to_discovery", "", jobref.KindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewSearcResultModel(
				&context.ProgramContext{Jenkins: gojenkins.CreateJenkins(nil, "http://jenkins")},
				40, 12, testKeymapConf(),
			)
			m.list.SetItems([]list.Item{NewSearchResultItem(&gojenkins.Suggestion{
				Name: "target",
				Type: tt.suggestionType,
				URL:  "/job/team/job/target/",
			})})

			cmd := m.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("selecting a result produced no command")
			}
			msg, ok := cmd().(SearchResultItemSelectedMsg)
			if !ok {
				t.Fatalf("got %T, want SearchResultItemSelectedMsg", cmd())
			}
			if msg.Target.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", msg.Target.Kind, tt.wantKind)
			}
			// A relative suggestion URL is made absolute here, where the
			// server is known.
			if want := "http://jenkins/job/team/job/target/"; msg.Target.URL != want {
				t.Errorf("URL = %q, want %q", msg.Target.URL, want)
			}
		})
	}
}
