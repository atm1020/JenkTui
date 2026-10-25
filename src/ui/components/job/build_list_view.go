package job

import (
	"fmt"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

type BuildListItem struct {
	BuildId string
	Url     string
}

func (jli BuildListItem) FilterValue() string {
	return jli.BuildId
}

func (jli BuildListItem) Title() string {
	return jli.BuildId
}

func (jli BuildListItem) Description() string {
	return jli.Url
}

type BuildListModel struct {
	wWithh, wHeight int
	list            *list.Model
	keyMap          BuildListViewKeyMap
	// renderRevision changes when the visible list may have changed, so the
	// job page can reuse a framed panel across animation frames.
	renderRevision uint64
}

func NewJobListModel(keyMap BuildListViewKeyMap) *BuildListModel {
	builds := buildListModel()
	builds.KeyMap.Filter = keyMap.Search
	return &BuildListModel{
		list:   builds,
		keyMap: keyMap,
	}
}

func (jl *BuildListModel) Init() tea.Cmd {
	return nil
}

func (jl *BuildListModel) Update(msg tea.Msg) (*BuildListModel, tea.Cmd) {
	jl.renderRevision++
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		jl.list.SetWidth(msg.Width)
		jl.list.SetHeight(msg.Height)
	}

	log.Debug("Msg received in BuildListModel Update:", msg)
	list, cmd := jl.list.Update(msg)
	jl.list = &list
	return jl, cmd
}

func (jl *BuildListModel) View(width, height int) string {
	if len(jl.list.Items()) != 0 {
		jl.list.SetHeight(height)
		jl.list.SetWidth(width)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, jl.list.View())
}

// IsFiltering reports whether the build list's filter is capturing a query.
func (jl *BuildListModel) IsFiltering() bool {
	return jl.list != nil && jl.list.FilterState() == list.Filtering
}

func (jl *BuildListModel) selectedItem() (BuildListItem, bool) {
	if jl.list == nil {
		return BuildListItem{}, false
	}
	item, ok := jl.list.SelectedItem().(BuildListItem)
	return item, ok
}

func (jl *BuildListModel) setItems(items []gojenkins.JobBuild) {
	jl.renderRevision++
	listItems := make([]list.Item, 0, len(items))
	for _, job := range items {
		listItems = append(listItems, BuildListItem{
			BuildId: fmt.Sprintf("#%d", job.Number),
			Url:     job.URL,
		})
	}
	jl.list.SetItems(listItems)
}

func buildListModel() *list.Model {
	jobList := list.New([]list.Item{}, BuildListViewDelegate{}, 0, 0)
	jobList.SetShowStatusBar(false)
	jobList.Title = "Builds"
	jobList.Styles.Title = styles.GetTheme().S().Title.Bold(true).Underline(true).Padding(0, 1)
	jobList.SetShowHelp(false)
	jobList.SetFilteringEnabled(true)
	return &jobList
}
