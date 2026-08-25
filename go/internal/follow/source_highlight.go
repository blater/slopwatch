package follow

import (
	"embed"
	"path/filepath"
	"strings"
	"sync"

	textmate "github.com/andersonpem/gopher-textmate"
	"github.com/andersonpem/gopher-textmate/render"
	textmatetheme "github.com/andersonpem/gopher-textmate/theme"

	"github.com/blater/slopwatch/internal/style"
)

// The grammars are embedded so the installed binary has no runtime asset or
// network dependency. They are pinned and documented in grammars/NOTICE.md.
//
//go:embed grammars/*.json
var sourceGrammarFiles embed.FS

//go:embed themes/*.json
var sourceThemeFiles embed.FS

var (
	sourceHighlighterOnce sync.Once
	sourceHighlighter     *textmate.Highlighter
	sourceHighlighterErr  error
	sourceHighlighterMu   sync.Mutex
	sourceGrammarsLoaded  = map[string]bool{}
	sourceThemes          = map[style.Theme]*textmatetheme.Theme{}
)

func sourceHighlighterInstance() (*textmate.Highlighter, error) {
	sourceHighlighterOnce.Do(func() {
		sourceHighlighter, sourceHighlighterErr = textmate.New(
			textmate.WithColorMode(render.TrueColor),
			textmate.WithBackground(true),
		)
	})
	return sourceHighlighter, sourceHighlighterErr
}

func sourceGrammar(path string) (string, string) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "source.go", "go.tmLanguage.json"
	case ".java":
		return "source.java", "java.tmLanguage.json"
	case ".rs":
		return "source.rust", "rust.tmLanguage.json"
	case ".ts", ".mts", ".cts":
		return "source.ts", "typescript.tmLanguage.json"
	case ".tsx":
		return "source.tsx", "typescriptreact.tmLanguage.json"
	default:
		return "", ""
	}
}

func sourceGrammarScope(path string) string {
	scope, _ := sourceGrammar(path)
	return scope
}

func highlightSource(path, contents string, selectedTheme style.Theme) string {
	scope, grammar := sourceGrammar(path)
	if scope == "" {
		return contents
	}
	highlighter, err := sourceHighlighterInstance()
	if err != nil {
		return contents
	}
	sourceHighlighterMu.Lock()
	defer sourceHighlighterMu.Unlock()
	if err := applySourceTheme(highlighter, selectedTheme); err != nil {
		return contents
	}
	if !sourceGrammarsLoaded[grammar] {
		data, readErr := sourceGrammarFiles.ReadFile("grammars/" + grammar)
		if readErr != nil {
			return contents
		}
		if _, loadErr := highlighter.LoadGrammarBytes(data); loadErr != nil {
			return contents
		}
		sourceGrammarsLoaded[grammar] = true
	}
	result, err := highlighter.Highlight(scope, contents)
	if err != nil {
		return contents
	}
	return result
}

func applySourceTheme(highlighter *textmate.Highlighter, selected style.Theme) error {
	if selected != style.ThemeLight {
		selected = style.ThemeDark
	}
	parsed := sourceThemes[selected]
	if parsed == nil {
		data := textmate.DefaultThemeBytes()
		if selected == style.ThemeLight {
			var err error
			data, err = sourceThemeFiles.ReadFile("themes/light.json")
			if err != nil {
				return err
			}
		}
		var err error
		parsed, err = textmatetheme.Parse(data)
		if err != nil {
			return err
		}
		if selected == style.ThemeDark {
			parsed.DefaultBG = &textmatetheme.RGB{R: 9, G: 23, B: 35}
		}
		sourceThemes[selected] = parsed
	}
	highlighter.SetTheme(parsed)
	return nil
}
