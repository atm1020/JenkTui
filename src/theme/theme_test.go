package theme

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const validTheme = `{
  "bg": "#101820",
  "accent": "#4da3ff",
  "bright_fg": "#f5f7fa",
  "fg": "#aab4c0",
  "green": "#72c472",
  "yellow": "#e0b84f",
  "red": "#ef6b73"
}`

func TestParseAndValidate(t *testing.T) {
	definition, err := Parse("ocean", strings.NewReader(validTheme))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if definition.Name != "ocean" || definition.Accent != "#4da3ff" || definition.Red != "#ef6b73" {
		t.Fatalf("Parse() = %+v", definition)
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	if _, err := Parse("broken", strings.NewReader(`{"accent":`)); err == nil {
		t.Fatal("Parse() accepted malformed JSON")
	}
}

func TestValidateRejectsIncompleteAndMalformedThemes(t *testing.T) {
	definition, err := Parse("broken", strings.NewReader(`{"accent":"blue"}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := definition.Validate(); err == nil {
		t.Fatal("Validate() accepted an incomplete theme")
	}

	definition, err = Parse("broken", strings.NewReader(strings.Replace(validTheme, "#4da3ff", "blue", 1)))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := definition.Validate(); err == nil || !strings.Contains(err.Error(), "accent must be #RRGGBB") {
		t.Fatalf("Validate() error = %v, want malformed accent error", err)
	}
}

func TestLoadAllIncludesSortedBuiltins(t *testing.T) {
	definitions := LoadAll("")
	if len(definitions) != 11 {
		t.Fatalf("LoadAll() returned %d built-ins, want 11", len(definitions))
	}
	for i := 1; i < len(definitions); i++ {
		if strings.ToLower(definitions[i-1].Name) > strings.ToLower(definitions[i].Name) {
			t.Fatalf("themes not sorted: %q before %q", definitions[i-1].Name, definitions[i].Name)
		}
	}

	for _, name := range []string{DefaultName, "Dracula", "Nord", "Tokyo Night"} {
		if _, ok := Find(definitions, name); !ok {
			t.Errorf("built-in theme %q not found", name)
		}
	}
	if definition, ok := Find(definitions, "tokyo-night"); !ok || definition.Name != "Tokyo Night" {
		t.Fatalf("Find(tokyo-night) = (%+v, %v)", definition, ok)
	}
}

func TestLoadAllAddsCustomTheme(t *testing.T) {
	configDir := t.TempDir()
	themeDir := filepath.Join(configDir, "themes")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "ocean.json"), []byte(validTheme), 0o600); err != nil {
		t.Fatal(err)
	}

	definitions := LoadAll(configDir)
	definition, ok := Find(definitions, "ocean")
	if !ok {
		t.Fatal("custom theme was not loaded")
	}
	if definition.Accent != "#4da3ff" {
		t.Fatalf("custom Accent = %q, want #4da3ff", definition.Accent)
	}
}

func TestLoadAllCustomThemeOverridesBuiltin(t *testing.T) {
	configDir := t.TempDir()
	themeDir := filepath.Join(configDir, "themes")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	override := strings.Replace(validTheme, "#4da3ff", "#ff00ff", 1)
	if err := os.WriteFile(filepath.Join(themeDir, "dracula.json"), []byte(override), 0o600); err != nil {
		t.Fatal(err)
	}

	definitions := LoadAll(configDir)
	definition, ok := Find(definitions, "Dracula")
	if !ok {
		t.Fatal("overridden Dracula theme was not loaded")
	}
	if definition.Name != "Dracula" || definition.ID != "dracula" {
		t.Fatalf("override identity = (%q, %q), want (Dracula, dracula)", definition.Name, definition.ID)
	}
	if definition.Accent != "#ff00ff" {
		t.Fatalf("override Accent = %q, want #ff00ff", definition.Accent)
	}
}

func TestLoadAllIgnoresInvalidAndNonThemeFiles(t *testing.T) {
	configDir := t.TempDir()
	themeDir := filepath.Join(configDir, "themes")
	if err := os.MkdirAll(filepath.Join(themeDir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "broken.json"), []byte(`{"accent":"blue"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "ignored.txt"), []byte(validTheme), 0o600); err != nil {
		t.Fatal(err)
	}

	definitions := LoadAll(configDir)
	if len(definitions) != 11 {
		t.Fatalf("LoadAll() returned %d themes after invalid files, want 11 built-ins", len(definitions))
	}
	for _, name := range []string{"broken", "ignored", "nested"} {
		if _, ok := Find(definitions, name); ok {
			t.Errorf("invalid entry %q was loaded", name)
		}
	}
}

func TestBuiltinThemesMeetTextContrast(t *testing.T) {
	for _, definition := range LoadAll("") {
		if err := definition.Validate(); err != nil {
			t.Fatalf("built-in theme %q is invalid: %v", definition.Name, err)
		}
		for _, role := range []struct {
			name  string
			color string
		}{
			{"accent", definition.Accent},
			{"bright_fg", definition.BrightFG},
			{"fg", definition.FG},
			{"green", definition.Green},
			{"yellow", definition.Yellow},
			{"red", definition.Red},
		} {
			if ratio := contrastRatio(role.color, definition.BG); ratio < 4.5 {
				t.Errorf("theme %q %s contrast = %.2f:1, want at least 4.5:1", definition.Name, role.name, ratio)
			}
		}
	}
}

func contrastRatio(a, b string) float64 {
	lighter := relativeLuminance(a)
	darker := relativeLuminance(b)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(hex string) float64 {
	value, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 24)
	if err != nil {
		return 0
	}
	linear := func(channel uint64) float64 {
		component := float64(channel) / 255
		if component <= 0.04045 {
			return component / 12.92
		}
		return math.Pow((component+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(value>>16) + 0.7152*linear((value>>8)&0xff) + 0.0722*linear(value&0xff)
}
