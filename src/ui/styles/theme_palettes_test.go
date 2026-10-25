package styles

import (
	"image/color"
	"testing"

	colortheme "github.com/atm1020/jenktui/src/theme"
)

func TestBuiltInThemesResolveRepresentativeColors(t *testing.T) {
	resetThemeCatalog(t)
	rgba := func(r, g, b, a uint8) color.Color { return color.RGBA{R: r, G: g, B: b, A: a} }

	cases := []struct {
		theme ThemeType
		want  map[string]color.Color
	}{
		{
			theme: Github,
			want: map[string]color.Color{
				"BgBase":                           rgba(13, 17, 23, 255),
				"Primary":                          rgba(88, 166, 255, 255),
				"Success":                          rgba(63, 185, 80, 255),
				"Error":                            rgba(248, 81, 73, 255),
				"StageRunning":                     rgba(13, 17, 23, 200),
				"Grey":                             rgba(125, 133, 144, 255),
				"SearchCurrentHighlightBackground": rgba(102, 78, 28, 255),
			},
		},
		{
			theme: Dracula,
			want: map[string]color.Color{
				"BgBase":       rgba(40, 42, 54, 255),
				"Primary":      rgba(189, 147, 249, 255),
				"AccentBlue":   rgba(189, 147, 249, 255),
				"StageRunning": rgba(40, 42, 54, 200),
			},
		},
		{
			theme: Nord,
			want: map[string]color.Color{
				"BgBase":           rgba(46, 52, 64, 255),
				"Primary":          rgba(129, 161, 193, 255),
				"StageNotExecuted": rgba(46, 52, 64, 100),
			},
		},
	}

	for _, test := range cases {
		t.Run(test.theme.String(), func(t *testing.T) {
			resolved := buildTheme(paletteFor(test.theme))
			fields := map[string]color.Color{
				"BgBase":                           resolved.BgBase,
				"Primary":                          resolved.Primary,
				"Success":                          resolved.Success,
				"Error":                            resolved.Error,
				"Grey":                             resolved.Grey,
				"AccentBlue":                       resolved.AccentBlue,
				"StageRunning":                     resolved.StageRunning,
				"StageNotExecuted":                 resolved.StageNotExecuted,
				"SearchCurrentHighlightBackground": resolved.SearchCurrentHighlightBackground,
			}
			for name, want := range test.want {
				if got := fields[name]; got != want {
					t.Errorf("%s.%s = %v, want %v", test.theme, name, got, want)
				}
			}
		})
	}
}

func TestAvailableThemeTypesIncludesEmbeddedCatalog(t *testing.T) {
	resetThemeCatalog(t)
	themes := AvailableThemeTypes()
	if len(themes) != 11 {
		t.Fatalf("AvailableThemeTypes() returned %d themes, want 11", len(themes))
	}
	for _, name := range []string{"github-dark", "Dracula", "tokyo night", "Nord"} {
		if _, ok := LookupThemeType(name); !ok {
			t.Errorf("LookupThemeType(%q) did not resolve", name)
		}
	}
}

func TestThemeCatalogFallsBackToGitHub(t *testing.T) {
	resetThemeCatalog(t)
	SwitchTheme(ThemeType("does not exist"))
	if got := GetCurrentThemeType(); got != Github {
		t.Fatalf("current theme = %q, want %q", got, Github)
	}
	if got := GetTheme().BgBase; got != (color.RGBA{R: 13, G: 17, B: 23, A: 255}) {
		t.Fatalf("fallback BgBase = %v", got)
	}
}

func TestThemeDarknessFollowsBackground(t *testing.T) {
	light := &Theme{BgBase: color.RGBA{R: 239, G: 241, B: 245, A: 255}}
	if light.IsDark() {
		t.Fatal("light background reported as dark")
	}
	dark := &Theme{BgBase: color.RGBA{R: 26, G: 27, B: 38, A: 255}}
	if !dark.IsDark() {
		t.Fatal("dark background reported as light")
	}
}

// One muted source role intentionally feeds every low-emphasis semantic field.
func TestBuildThemeReusesMutedToken(t *testing.T) {
	resetThemeCatalog(t)
	resolved := buildTheme(paletteFor(Github))
	for name, got := range map[string]color.Color{
		"Aborted":            resolved.Aborted,
		"SubtleText":         resolved.SubtleText,
		"GutterLineNumber":   resolved.GutterLineNumber,
		"StagePendingBorder": resolved.StagePendingBorder,
		"StageAbortedBorder": resolved.StageAbortedBorder,
	} {
		if got != resolved.Grey {
			t.Errorf("%s = %v, want Grey %v", name, got, resolved.Grey)
		}
	}
}

func resetThemeCatalog(t *testing.T) {
	t.Helper()
	SetThemeDefinitions(colortheme.LoadAll(""))
	SwitchTheme(Github)
	t.Cleanup(func() {
		SetThemeDefinitions(colortheme.LoadAll(""))
		SwitchTheme(Github)
	})
}
