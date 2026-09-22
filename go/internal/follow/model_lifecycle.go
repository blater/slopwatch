package follow

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/sourceignore"
	tea "github.com/charmbracelet/bubbletea"
)

const analysisProgressBatchInterval = 2 * time.Second

func waitForChange(watcher *sourceWatcher) tea.Cmd {
	return func() tea.Msg { return watcher.wait() }
}

func startWatcher(watcher *sourceWatcher) tea.Cmd {
	return func() tea.Msg { return watcherReady{err: watcher.start(), watcher: watcher} }
}

func analysisCommand(analyzer Analyzer, configuredTargets []string, paths []string, full bool) tea.Cmd {
	return analysisCommandWithProgress(analyzer, configuredTargets, paths, full, nil)
}

func analysisCommandWithProgress(analyzer Analyzer, configuredTargets []string, paths []string, full bool, emit func(report.Document)) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		targets := paths
		if full {
			targets = configuredTargets
		}
		languages := languagesForPaths(paths)
		if full {
			languages = nil
		}
		if emit != nil {
			ctx = native.WithAnalysisProgress(ctx, emit)
		}
		document, err := analyzer.Analyze(ctx, targets, languages)
		return analysisResult{document: document, replace: paths, paths: append([]string(nil), paths...), full: full, err: err}
	}
}

func languagesForPaths(paths []string) []string {
	seen := map[string]bool{}
	for _, path := range paths {
		language := ""
		switch strings.ToLower(filepath.Ext(path)) {
		case ".go":
			language = "go"
		case ".java":
			language = "java"
		case ".rs":
			language = "rust"
		case ".ts", ".tsx", ".mts", ".cts":
			language = "typescript"
		}
		if language != "" {
			seen[language] = true
		}
	}
	result := make([]string, 0, len(seen))
	for language := range seen {
		result = append(result, language)
	}
	sort.Strings(result)
	return result
}

func startupAnalysisCommand(analyzer Analyzer, targets []string) tea.Cmd {
	return startupAnalysisCommandWithProgress(analyzer, targets, nil)
}

func startupAnalysisCommandWithProgress(analyzer Analyzer, targets []string, emit func(report.Document), matchers ...*sourceignore.Matcher) tea.Cmd {
	if startup, ok := analyzer.(interface {
		AnalyzeStartup(context.Context, []string) (report.Document, error)
	}); ok {
		return func() tea.Msg {
			ctx := context.Background()
			if len(matchers) > 0 {
				ctx = native.WithFollowMatcher(ctx, matchers[0])
			}
			if emit != nil {
				ctx = native.WithAnalysisProgress(ctx, emit)
			}
			document, err := startup.AnalyzeStartup(ctx, targets)
			return analysisResult{document: document, full: true, err: err}
		}
	}
	return analysisCommandWithProgress(analyzer, targets, nil, true, emit)
}
