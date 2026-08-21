package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/slopslap/slopslap/internal/report"
)

var ErrUnsupported = errors.New("native analyzer path is not yet supported")

type Options struct {
	Targets      []string
	Languages    []string
	IncludeTests bool
	Timeout      float64
	PassScore    *float64
}

type Analyzer struct {
	workspace string
	root      string
	catalog   catalogDocument
	options   Options
}

func New(workspace, installationRoot string, options Options) (*Analyzer, error) {
	catalog, err := loadCatalog(installationRoot)
	if err != nil {
		return nil, err
	}
	return &Analyzer{workspace: workspace, root: installationRoot, catalog: catalog, options: options}, nil
}

func (analyzer *Analyzer) Analyze(parent context.Context, targets []string, languages []string) (report.Document, error) {
	options := analyzer.options
	if targets != nil {
		options.Targets = targets
	}
	if languages != nil {
		options.Languages = languages
	}
	discovered, err := analyzer.discover(options.Targets, options.IncludeTests)
	if err != nil {
		return report.Document{}, err
	}
	selected := options.Languages
	if len(selected) == 0 {
		for language := range discovered {
			selected = append(selected, language)
		}
		sort.Strings(selected)
	}
	if len(selected) == 0 {
		return report.Document{}, fmt.Errorf("no supported Go, Java, or Rust source files found")
	}
	for _, language := range selected {
		if language == "typescript" {
			return report.Document{}, ErrUnsupported
		}
		if len(discovered[language]) == 0 {
			return report.Document{}, fmt.Errorf("no source files discovered for: %s", language)
		}
	}
	executable := filepath.Join(analyzer.root, "analyzers", "structural", "slopslap-structural")
	if info, statErr := os.Stat(executable); statErr != nil || info.IsDir() {
		return report.Document{}, fmt.Errorf("%w: structural analyzer is not installed", ErrUnsupported)
	}
	allRecords := []protocolRecord{}
	for _, language := range selected {
		components := []requestedComponent{}
		for _, descriptor := range analyzer.catalog.Components {
			if descriptor.Defaults.Enabled && descriptor.supported(language) {
				components = append(components, requestedComponent{descriptor.ID, descriptor.Version})
			}
		}
		if len(components) == 0 {
			continue
		}
		invocation, uuidErr := invocationID()
		if uuidErr != nil {
			return report.Document{}, uuidErr
		}
		requestOptions := map[string]any{"include_tests": options.IncludeTests}
		if language == "java" {
			if java, lookErr := exec.LookPath("java"); lookErr == nil {
				if absolute, absErr := filepath.Abs(java); absErr == nil {
					requestOptions["java_path"] = absolute
				}
			}
		}
		request := analyzerRequest{"request", 1, invocation, analyzer.workspace, []protocolUnit{{language + "-unit", language, discovered[language], map[string]any{}}}, components, requestOptions, map[string]int{"max_seconds": int(options.Timeout)}}
		timeout := time.Duration(options.Timeout * float64(time.Second))
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		ctx, cancel := context.WithTimeout(parent, timeout)
		records, runErr := runAnalyzer(ctx, executable, request, options.Timeout)
		cancel()
		if runErr != nil {
			return report.Document{}, fmt.Errorf("%s analyzer failed: %w", language, runErr)
		}
		allRecords = append(allRecords, records...)
	}
	return scoreRecords(analyzer.catalog, selected, allRecords, options.PassScore)
}

var ignored = map[string]bool{".git": true, ".gradle": true, ".idea": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, "build": true, "coverage": true, "dist": true, "node_modules": true, "out": true, "target": true, "vendor": true}
var testDirs = map[string]bool{"__tests__": true, "integration-test": true, "integration-tests": true, "integrationtest": true, "integrationtests": true, "spec": true, "specs": true, "test": true, "test-fixtures": true, "testfixtures": true, "tests": true}

func (analyzer *Analyzer) discover(targets []string, includeTests bool) (map[string][]string, error) {
	if len(targets) == 0 {
		targets = []string{"."}
	}
	extensions := map[string]string{".go": "go", ".java": "java", ".rs": "rust", ".ts": "typescript", ".tsx": "typescript", ".mts": "typescript", ".cts": "typescript"}
	grouped := map[string]map[string]bool{}
	add := func(path string) error {
		relative, err := filepath.Rel(analyzer.workspace, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("analysis target escapes workspace: %s", path)
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		for _, part := range parts[:max(0, len(parts)-1)] {
			lower := strings.ToLower(part)
			if ignored[lower] {
				return nil
			}
			if !includeTests && testDirs[lower] {
				return nil
			}
		}
		name := strings.ToLower(filepath.Base(relative))
		language := extensions[strings.ToLower(filepath.Ext(name))]
		if language == "" {
			return nil
		}
		if !includeTests && language == "go" && strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if !includeTests && language == "typescript" && typescriptTest(name) {
			return nil
		}
		if grouped[language] == nil {
			grouped[language] = map[string]bool{}
		}
		grouped[language][filepath.ToSlash(relative)] = true
		return nil
	}
	for _, target := range targets {
		absolute := target
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(analyzer.workspace, target)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if err := add(resolved); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(resolved, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != resolved && ignored[strings.ToLower(entry.Name())] {
					return filepath.SkipDir
				}
				return nil
			}
			return add(path)
		})
		if err != nil {
			return nil, err
		}
	}
	result := map[string][]string{}
	for language, paths := range grouped {
		for path := range paths {
			result[language] = append(result[language], path)
		}
		sort.Strings(result[language])
	}
	return result, nil
}

func typescriptTest(name string) bool {
	for _, suffix := range []string{".spec.cts", ".spec.mts", ".spec.ts", ".spec.tsx", ".test.cts", ".test.mts", ".test.ts", ".test.tsx"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
