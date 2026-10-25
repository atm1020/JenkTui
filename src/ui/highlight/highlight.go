package highlight

import (
	"bytes"

	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromaStyles "github.com/alecthomas/chroma/v2/styles"
)

// SyntaxHighlight applies syntax highlighting to source code.
// fileName is used to detect the language (e.g., "script.groovy").
// If language detection fails, returns the original source.
func SyntaxHighlight(source, fileName string) string {
	// Determine the language lexer to use
	l := lexers.Match(fileName)
	if l == nil {
		l = lexers.Analyse(source)
	}
	if l == nil {
		return source // No lexer found, return original
	}
	l = chroma.Coalesce(l)

	// Get the terminal formatter for ANSI colors
	f := formatters.Get("terminal16m")
	if f == nil {
		f = formatters.Fallback
	}

	// Get style based on current theme
	style := getChromaStyle()

	// Tokenize and format
	it, err := l.Tokenise(nil, source)
	if err != nil {
		return source
	}

	var buf bytes.Buffer
	err = f.Format(&buf, style, it)
	if err != nil {
		return source
	}

	return buf.String()
}

// getChromaStyle returns a chroma style that matches the current theme
func getChromaStyle() *chroma.Style {
	themeType := styles.GetCurrentThemeType()

	var styleName string
	switch themeType {
	case styles.Dracula:
		styleName = "dracula"
	case styles.Gruvbox:
		styleName = "gruvbox"
	case styles.Nord:
		styleName = "nord"
	case styles.TokyoNight:
		styleName = "tokyonight-night"
	case styles.Github:
		styleName = "github-dark"
	default:
		styleName = "github-dark"
		if !styles.GetTheme().IsDark() {
			styleName = "github"
		}
	}

	style := chromaStyles.Get(styleName)
	if style == nil {
		style = chromaStyles.Fallback
	}

	return style
}
