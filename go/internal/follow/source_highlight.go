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
		for _, grammar := range []string{
			"go.tmLanguage.json",
			"java.tmLanguage.json",
			"rust.tmLanguage.json",
			"typescript.tmLanguage.json",
			"typescriptreact.tmLanguage.json",
		} {
			var data []byte
			data, sourceHighlighterErr = sourceGrammarFiles.ReadFile("grammars/" + grammar)
			if sourceHighlighterErr != nil {
				return
			}
			if _, sourceHighlighterErr = sourceHighlighter.LoadGrammarBytes(data); sourceHighlighterErr != nil {
				return
			}
		}
	})
	return sourceHighlighter, sourceHighlighterErr
}

func sourceGrammarScope(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "source.go"
	case ".java":
		return "source.java"
	case ".rs":
		return "source.rust"
	case ".ts", ".mts", ".cts":
		return "source.ts"
	case ".tsx":
		return "source.tsx"
	default:
		return ""
	}
}

func highlightSource(path, contents string) string {
	scope := sourceGrammarScope(path)
	if scope == "" {
		return contents
	}
	highlighter, err := sourceHighlighterInstance()
	if err != nil {
		return contents
	}
	result, err := highlighter.Highlight(scope, contents)
	if err != nil {
		return contents
	}
	return result
}
