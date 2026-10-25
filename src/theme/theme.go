// Package theme loads JenkTui color-theme definitions.
package theme

import (
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Additional built-in palettes are adapted from cliamp's contrast-checked
// themes: https://github.com/bjarneo/cliamp
//
//go:embed themes/*.json
var builtinThemes embed.FS

// DefaultName is the theme used when configuration names no available theme.
const (
	DefaultName        = "GitHub Dark"
	themeFileExtension = ".json"
)

// Definition is the compact set of color roles from which the UI's semantic
// palette is derived. ID is the stable filename-style identifier; Name is what
// the selector and config file display.
type Definition struct {
	ID       string `json:"-"`
	Name     string `json:"-"`
	BG       string `json:"bg"`
	Accent   string `json:"accent"`
	BrightFG string `json:"bright_fg"`
	FG       string `json:"fg"`
	Green    string `json:"green"`
	Yellow   string `json:"yellow"`
	Red      string `json:"red"`
}

var (
	hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

	builtinDisplayNames = map[string]string{
		"dracula":           "Dracula",
		"ember":             "Ember",
		"ethereal":          "Ethereal",
		"github-dark":       DefaultName,
		"gruvbox":           "Gruvbox",
		"hackerman":         "Hackerman",
		"kanagawa":          "Kanagawa",
		"matte-black":       "Matte Black",
		"neon-blade-runner": "Neon Blade Runner",
		"nord":              "Nord",
		"tokyo-night":       "Tokyo Night",
	}
)

// Validate requires a complete seven-color palette in CSS hex notation.
func (d Definition) Validate() error {
	if strings.TrimSpace(d.Name) == "" || themeKey(d.Name) == "" {
		return fmt.Errorf("theme name must contain a letter or number")
	}

	for _, field := range []struct {
		name  string
		value string
	}{
		{"bg", d.BG},
		{"accent", d.Accent},
		{"bright_fg", d.BrightFG},
		{"fg", d.FG},
		{"green", d.Green},
		{"yellow", d.Yellow},
		{"red", d.Red},
	} {
		if field.value == "" {
			return fmt.Errorf("theme %q: %s is required", d.Name, field.name)
		}
		if !hexColor.MatchString(field.value) {
			return fmt.Errorf("theme %q: %s must be #RRGGBB", d.Name, field.name)
		}
	}
	return nil
}

// Parse decodes one JSON theme definition. ID and Name come from the filename,
// not file contents, so config lookup and built-in overrides remain stable.
func Parse(name string, r io.Reader) (Definition, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return Definition{}, err
	}

	definition := Definition{ID: name, Name: name}
	if err := json.Unmarshal(data, &definition); err != nil {
		return Definition{}, err
	}
	return definition, nil
}

// LoadAll returns embedded themes plus valid custom themes from
// <configDir>/themes/*.json. A custom file overrides a built-in theme when its
// filename matches the built-in ID or display name, case-insensitively. Invalid
// files are ignored and valid results are sorted by display name.
func LoadAll(configDir string) []Definition {
	definitions := make(map[string]Definition)
	loadBuiltin(definitions)
	if configDir != "" {
		loadUserDir(filepath.Join(configDir, "themes"), definitions)
	}

	result := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition)
	}
	slices.SortFunc(result, func(a, b Definition) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return result
}

// Find resolves either a display name ("Tokyo Night") or a filename-style ID
// ("tokyo-night") against definitions.
func Find(definitions []Definition, name string) (Definition, bool) {
	key := themeKey(name)
	for _, definition := range definitions {
		if key == themeKey(definition.Name) || key == themeKey(definition.ID) {
			return definition, true
		}
	}
	return Definition{}, false
}

func loadBuiltin(definitions map[string]Definition) {
	entries, err := builtinThemes.ReadDir("themes")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != themeFileExtension {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		file, err := builtinThemes.Open("themes/" + entry.Name())
		if err != nil {
			continue
		}
		definition, parseErr := Parse(id, file)
		_ = file.Close()
		if displayName, ok := builtinDisplayNames[id]; ok {
			definition.Name = displayName
		}
		if parseErr != nil || definition.Validate() != nil {
			continue
		}
		definitions[themeKey(id)] = definition
	}
}

func loadUserDir(dir string, definitions map[string]Definition) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != themeFileExtension {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		definition, parseErr := Parse(name, file)
		_ = file.Close()
		if parseErr != nil || definition.Validate() != nil {
			continue
		}

		key := themeKey(name)
		if existing, ok := definitions[key]; ok {
			// Preserve the stable built-in identity and display name while replacing
			// its colors with the user's override.
			definition.ID = existing.ID
			definition.Name = existing.Name
		}
		definitions[key] = definition
	}
}

// themeKey makes display names and filename slugs interchangeable without
// imposing a naming convention on custom themes.
func themeKey(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
