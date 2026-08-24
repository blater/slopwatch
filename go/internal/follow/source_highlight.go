package follow

import (
	"embed"
	"path/filepath"
	"strings"
	"sync"

	textmate "github.com/andersonpem/gopher-textmate"
	"github.com/andersonpem/gopher-textmate/render"
)

// The grammars are embedded so the installed binary has no runtime asset or
// network dependency. They are pinned and documented in grammars/NOTICE.md.
//
//go:embed grammars/*.json
var sourceGrammarFiles embed.FS

var (
	sourceHighlighterOnce sync.Once
	sourceHighlighter     *textmate.Highlighter
	sourceHighlighterErr  error
	sourceHighlighterMu   sync.Mutex
	sourceGrammarsLoaded  = map[string]bool{}
)

func sourceHighlighterInstance() (*textmate.Highlighter, error) {
	sourceHighlighterOnce.Do(func() {
		sourceHighlighter, sourceHighlighterErr = textmate.New(
			textmate.WithColorMode(render.TrueColor),
		)
		if sourceHighlighterErr != nil {
			return
		}
		if sourceHighlighterErr = sourceHighlighter.SetThemeBytes(textmate.DefaultThemeBytes()); sourceHighlighterErr != nil {
			return
		}
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

func highlightSource(path, contents string) string {
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
