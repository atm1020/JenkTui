package styles

import (
	"cmp"
	"image/color"
	"math"
	"slices"
	"strings"
	"unicode"

	colortheme "github.com/atm1020/jenktui/src/theme"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

type Theme struct {
	PrimaryBorder      color.Color
	SecondaryBorder    color.Color
	SelectedBackground color.Color
	FaintBorder        color.Color

	Primary   color.Color
	Secondary color.Color
	FgBase    color.Color

	BgBase color.Color

	HighlightForeground color.Color
	HighlightBackground color.Color

	Success color.Color
	Error   color.Color
	Warning color.Color
	Info    color.Color
	Link    color.Color
	Grey    color.Color
	Brown   color.Color
	Yellow  color.Color

	JenkinsRed  color.Color
	JenkinsBlue color.Color

	// Stage box colors
	StagePending     color.Color
	StageRunning     color.Color
	StageSuccess     color.Color
	StageFailed      color.Color
	StageAborted     color.Color
	StageNotExecuted color.Color

	// Stage box borders
	StagePendingBorder     color.Color
	StageRunningBorder     color.Color
	StageSuccessBorder     color.Color
	StageFailedBorder      color.Color
	StageAbortedBorder     color.Color
	StageNotExecutedBorder color.Color

	// Focused stage
	StageFocusedBorder color.Color

	// Gutter (log viewer line numbers)
	GutterLineNumber  color.Color
	GutterCurrentLine color.Color
	GutterCurrentBg   color.Color
	GutterVisualLine  color.Color
	GutterVisualBg    color.Color
	GutterSeparator   color.Color

	// Spinners
	SpinnerColor color.Color

	// Build info
	BuildNumberBg color.Color
	BuildNumberFg color.Color
	BuildDateBg   color.Color
	BuildDateFg   color.Color

	// Footer
	FooterBackground      color.Color
	FooterForeground      color.Color
	FooterPageBg          color.Color
	FooterPageFg          color.Color
	FooterServerBg        color.Color
	FooterServerFg        color.Color
	FooterDivider         color.Color
	FooterServerConnected color.Color

	HeaderBackground color.Color
	HeaderForeground color.Color

	SearchHighlightBackground        color.Color
	SearchHighlightForeground        color.Color
	SearchCurrentHighlightBackground color.Color
	SearchCurrentHighlightForeground color.Color

	Background color.Color
	Foreground color.Color
	AccentBlue color.Color
	Highlight  color.Color
	Unstable   color.Color
	Failure    color.Color
	Aborted    color.Color
	Running    color.Color
	Border     color.Color
	SubtleText color.Color

	styles *Styles
}

type StyledIcons struct {
	Success      string
	Failure      string
	Running      string
	Pending      string
	NotBuilt     string
	Unknown      string
	NotAvailable string

	Sunny           string
	ThunderStorm    string
	Cloudy          string
	OneCloud        string
	Folder          string
	Branch          string
	Thunder         string
	ConnectedServer string
	BookMark        string
	Attachment      string
	WorkflowJobIcon string
	Node            string
	Item            string
	View            string
}

type RawIcon struct {
	Node   string
	Item   string
	View   string
	Folder string
}

type ListDelegateStyles struct {
	NormalTitle   lipgloss.Style
	NormalDesc    lipgloss.Style
	SelectedTitle lipgloss.Style
	SelectedDesc  lipgloss.Style
	Icon          lipgloss.Style
	Metadata      lipgloss.Style
}

type StageBoxStyles struct {
	Base        lipgloss.Style // Common base for all boxes
	Pending     lipgloss.Style
	Running     lipgloss.Style
	Success     lipgloss.Style
	Failed      lipgloss.Style
	Aborted     lipgloss.Style
	NotExecuted lipgloss.Style
}

type GutterStyles struct {
	LineNumber       lipgloss.Style
	CurrentLine      lipgloss.Style
	VisualLine       lipgloss.Style
	SoftWrap         lipgloss.Style
	Separator        lipgloss.Style
	CurrentSeparator lipgloss.Style
	CurrentMarker    lipgloss.Style
	VisualSeparator  lipgloss.Style
}

type Styles struct {
	Base         lipgloss.Style
	SelectedBase lipgloss.Style
	BaseList     lipgloss.Style

	SelectedBorder lipgloss.Style
	DefaultBorder  lipgloss.Style
	DialogBorder   lipgloss.Style

	Title        lipgloss.Style
	Subtitle     lipgloss.Style
	Text         lipgloss.Style
	TextSelected lipgloss.Style
	Muted        lipgloss.Style
	Subtle       lipgloss.Style
	List         list.Styles
	Footer       lipgloss.Style

	Success lipgloss.Style
	Error   lipgloss.Style
	Warning lipgloss.Style
	Info    lipgloss.Style
	Default lipgloss.Style

	// Inputs
	TextInput textinput.Styles
	// TextArea  textarea.Styles

	// Help
	Help help.Styles

	StyledIcons StyledIcons
	RawIcon     RawIcon

	TextMatchStyle        lipgloss.Style
	TextCurrentMatchStyle lipgloss.Style

	// LogDefault paints ordinary log lines in the theme foreground rather than
	// leaving them at whatever foreground the terminal happens to default to,
	// which keeps the build's own output high-contrast against the dimmed
	// LogPipeline scaffolding.
	LogDefault lipgloss.Style

	// LogPipeline dims the "[Pipeline]" scaffolding lines Jenkins interleaves
	// with build output, so the output itself reads first.
	LogPipeline lipgloss.Style

	ListDelegate ListDelegateStyles

	// Stage box styles
	StageBox StageBoxStyles

	// Spinner
	Spinner lipgloss.Style

	// Build info
	BuildNumber lipgloss.Style
	BuildDate   lipgloss.Style

	// Line numbers
	LineNumber lipgloss.Style

	// Gutter
	Gutter GutterStyles
}

func (t *Theme) S() *Styles {
	if t.styles == nil {
		t.styles = t.buildStyles()
	}
	return t.styles
}

func (t *Theme) buildStyles() *Styles {
	// The app surface is transparent: the terminal background is set to BgBase
	// via OSC 11 (see App.View), so styles must NOT paint BgBase themselves —
	// in terminals without OSC 11 support that renders as mismatched patches.
	// Only semantic backgrounds (badges, selection, footer, stage boxes) are set.
	base := lipgloss.NewStyle().
		Foreground(t.FgBase)

	baseBorder := base.Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		PaddingLeft(1).
		PaddingRight(1)

	stageBox := lipgloss.NewStyle().
		Width(22).
		Height(8).
		Align(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Border(lipgloss.RoundedBorder())

	return &Styles{
		Base: base,

		SelectedBase: base.Background(t.Primary).Foreground(t.HighlightForeground),

		SelectedBorder: baseBorder.BorderForeground(t.Highlight),
		DefaultBorder:  baseBorder.BorderForeground(t.Border),
		DialogBorder: base.Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Padding(1, 2),

		// BaseList: base,
		BaseList: base.Border(lipgloss.RoundedBorder()).Padding(1, 1),
		Title:    base.Bold(true).Foreground(t.FgBase),

		Subtitle: base.Foreground(t.Secondary).Bold(true),

		Footer: base.Foreground(t.FgBase).Foreground(t.FooterForeground).Background(t.FooterBackground),

		Text:         lipgloss.NewStyle().Foreground(t.FgBase),
		TextSelected: lipgloss.NewStyle().Background(t.GutterVisualBg).Foreground(t.GutterVisualLine),
		StyledIcons: StyledIcons{
			Success:      base.Foreground(t.Success).Render(SuccessIcon),
			Failure:      base.Foreground(t.Error).Render(FailureIcon),
			Running:      base.Foreground(t.Warning).Render(WaitingIcon),
			NotBuilt:     base.Foreground(t.Grey).Render(NotBuildedIcon),
			Unknown:      base.Foreground(t.Grey).Render(UknownStateIcon),
			NotAvailable: base.Foreground(t.Grey).Render(NotAvailable),

			Sunny:           base.Foreground(t.Warning).Render(SunnyIcon),
			ThunderStorm:    base.Foreground(t.Error).Render(ThunderStormIcon),
			Cloudy:          base.Foreground(t.Grey).Render(CloudyIcon),
			OneCloud:        base.Foreground(t.Info).Render(OvercastIcon),
			Thunder:         base.Foreground(t.Info).Render(Thunder),
			ConnectedServer: base.Background(t.FooterServerBg).Padding(0, 1).Foreground(t.FooterServerConnected).Render(ConnectedServerIcon),
			Folder:          base.Foreground(t.Yellow).Padding(0, 1).Render(FolderIcon),
			Branch:          base.Foreground(t.Info).Padding(0, 1).Render(BranchIcon),
			BookMark:        base.Foreground(t.Info).Padding(0, 1).Render(BookmarkIcon),
			Attachment:      base.Foreground(t.Info).Padding(0, 1).Render(AttachmentIcon),
			WorkflowJobIcon: base.Foreground(t.Grey).Padding(0, 1).Render(WorkflowJobIcon),
			Node:            base.Foreground(t.Grey).Padding(0, 1).Render(Node),
			Item:            Item,
			View:            base.Foreground(t.Grey).Render(View),
		},

		RawIcon: RawIcon{
			Node:   Node,
			Item:   Item,
			View:   View,
			Folder: FolderIcon,
		},

		TextMatchStyle: lipgloss.NewStyle().
			Background(t.SearchHighlightBackground).
			Foreground(t.SearchHighlightForeground),
		TextCurrentMatchStyle: lipgloss.NewStyle().
			Background(t.SearchCurrentHighlightBackground).
			Foreground(t.SearchCurrentHighlightForeground).
			Bold(true),

		LogDefault:  base.Foreground(t.FgBase),
		LogPipeline: base.Foreground(t.Grey),

		ListDelegate: ListDelegateStyles{
			NormalTitle: base.Foreground(t.FgBase).PaddingLeft(2),
			NormalDesc:  base.Foreground(t.Secondary).PaddingLeft(2),
		},

		// Stage box styles. The status signal is the border + text color; the
		// fills stayed transparent (the old fills were canvas-tinted BgBase
		// variants, which render as mismatched slabs without OSC 11 support).
		StageBox: StageBoxStyles{
			Base:        stageBox,
			Pending:     stageBox.BorderForeground(t.StagePendingBorder).Foreground(t.FgBase),
			Running:     stageBox.BorderForeground(t.StageRunningBorder).Foreground(t.Primary),
			Success:     stageBox.BorderForeground(t.StageSuccessBorder).Foreground(t.Success),
			Failed:      stageBox.BorderForeground(t.StageFailedBorder).Foreground(t.Error),
			Aborted:     stageBox.BorderForeground(t.StageAbortedBorder).Foreground(t.FgBase),
			NotExecuted: stageBox.BorderForeground(t.StageNotExecutedBorder).Foreground(t.Grey),
		},

		Spinner: lipgloss.NewStyle().Foreground(t.SpinnerColor).Bold(true),

		BuildNumber: lipgloss.NewStyle().
			Bold(true).
			Foreground(t.BuildNumberFg).
			Background(t.BuildNumberBg).
			Padding(0, 1),

		BuildDate: lipgloss.NewStyle().
			Bold(true).
			Background(t.BuildDateBg).
			Foreground(t.BuildDateFg).
			Padding(0, 1).
			Width(20),

		LineNumber: base.Foreground(t.Grey),

		// Gutter surface is transparent; only the current-line / visual
		// selection chips carry a (semantic) background.
		Gutter: GutterStyles{
			LineNumber:       lipgloss.NewStyle().Foreground(t.GutterLineNumber).PaddingRight(1),
			CurrentLine:      lipgloss.NewStyle().Foreground(t.GutterCurrentLine).Background(t.GutterCurrentBg).Bold(true).PaddingRight(1),
			VisualLine:       lipgloss.NewStyle().Foreground(t.GutterVisualLine).Background(t.GutterVisualBg).PaddingRight(1),
			SoftWrap:         lipgloss.NewStyle().Foreground(t.GutterLineNumber).PaddingRight(1),
			Separator:        lipgloss.NewStyle().Foreground(t.GutterSeparator),
			CurrentSeparator: lipgloss.NewStyle().Foreground(t.GutterSeparator).Background(t.GutterCurrentBg),
			CurrentMarker:    lipgloss.NewStyle().Foreground(t.FgBase),
			VisualSeparator:  lipgloss.NewStyle().Foreground(t.GutterSeparator).Background(t.GutterVisualBg),
		},
	}
}

// IsDark reports whether the theme background is better served by a dark
// syntax-highlighting style. Theme foreground roles still carry their own
// contrast; this only chooses a suitable Chroma family.
func (t *Theme) IsDark() bool {
	rgba := color.RGBAModel.Convert(t.BgBase).(color.RGBA)
	linear := func(component uint8) float64 {
		value := float64(component) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	luminance := 0.2126*linear(rgba.R) + 0.7152*linear(rgba.G) + 0.0722*linear(rgba.B)
	return luminance < 0.5
}

type ThemeType string

const (
	Github          ThemeType = "GitHub Dark"
	Dracula         ThemeType = "Dracula"
	Ember           ThemeType = "Ember"
	Ethereal        ThemeType = "Ethereal"
	Gruvbox         ThemeType = "Gruvbox"
	Hackerman       ThemeType = "Hackerman"
	Kanagawa        ThemeType = "Kanagawa"
	MatteBlack      ThemeType = "Matte Black"
	NeonBladeRunner ThemeType = "Neon Blade Runner"
	Nord            ThemeType = "Nord"
	TokyoNight      ThemeType = "Tokyo Night"
)

type themeEntry struct {
	themeType ThemeType
	palette   palette
}

var (
	currentTheme     *Theme
	currentThemeType ThemeType = Github
	themeEntries     map[string]themeEntry
	availableThemes  []ThemeType
)

// SetThemeDefinitions replaces the available catalog. Callers load definitions
// in an asynchronous Bubble Tea command, then install them on the update loop so
// rendering never races filesystem work.
func SetThemeDefinitions(definitions []colortheme.Definition) {
	previous := currentThemeType
	installThemeDefinitions(definitions)
	currentTheme = nil
	SwitchTheme(previous)
}

// AvailableThemeTypes returns a copy of the sorted catalog used by the selector.
func AvailableThemeTypes() []ThemeType {
	ensureThemeDefinitions()
	return append([]ThemeType(nil), availableThemes...)
}

func GetTheme() *Theme {
	if currentTheme == nil {
		SwitchTheme(currentThemeType)
	}
	return currentTheme
}

// paletteFor maps a ThemeType to its palette, falling back to GitHub Dark so
// currentTheme can never be left unset.
func paletteFor(themeType ThemeType) palette {
	ensureThemeDefinitions()
	if entry, ok := themeEntries[themeLookupKey(themeType.String())]; ok {
		return entry.palette
	}
	return themeEntries[themeLookupKey(Github.String())].palette
}

// String returns the display name persisted in config.json.
func (t ThemeType) String() string {
	if t == "" {
		return "Unknown"
	}
	return string(t)
}

// GetCurrentThemeType returns the currently active theme type.
func GetCurrentThemeType() ThemeType {
	return currentThemeType
}

// LookupThemeType resolves a display name or filename-style ID.
func LookupThemeType(name string) (ThemeType, bool) {
	ensureThemeDefinitions()
	entry, ok := themeEntries[themeLookupKey(name)]
	if !ok {
		return "", false
	}
	return entry.themeType, true
}

// ThemeTypeFromString resolves a configured theme and safely falls back to
// GitHub Dark when it is unavailable.
func ThemeTypeFromString(name string) ThemeType {
	if themeType, ok := LookupThemeType(name); ok {
		return themeType
	}
	return Github
}

func SwitchTheme(themeType ThemeType) {
	ensureThemeDefinitions()
	entry, ok := themeEntries[themeLookupKey(themeType.String())]
	if !ok {
		entry = themeEntries[themeLookupKey(Github.String())]
	}
	currentThemeType = entry.themeType
	currentTheme = buildTheme(entry.palette)
}

func ensureThemeDefinitions() {
	if len(themeEntries) == 0 {
		installThemeDefinitions(colortheme.LoadAll(""))
	}
}

func installThemeDefinitions(definitions []colortheme.Definition) {
	themeEntries = make(map[string]themeEntry, len(definitions)*2)
	availableThemes = availableThemes[:0]
	seen := make(map[string]struct{}, len(definitions))

	add := func(definition colortheme.Definition) {
		resolved, err := paletteFromDefinition(definition)
		if err != nil {
			return
		}
		themeType := ThemeType(definition.Name)
		entry := themeEntry{themeType: themeType, palette: resolved}
		themeEntries[themeLookupKey(definition.Name)] = entry
		themeEntries[themeLookupKey(definition.ID)] = entry
		key := themeLookupKey(definition.Name)
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			availableThemes = append(availableThemes, themeType)
		}
	}

	for _, definition := range definitions {
		add(definition)
	}
	if _, ok := themeEntries[themeLookupKey(Github.String())]; !ok {
		if fallback, found := colortheme.Find(colortheme.LoadAll(""), Github.String()); found {
			add(fallback)
		}
	}

	slices.SortFunc(availableThemes, func(a, b ThemeType) int {
		return cmp.Compare(strings.ToLower(a.String()), strings.ToLower(b.String()))
	})
}

func themeLookupKey(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
