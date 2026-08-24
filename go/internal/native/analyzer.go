package native

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blater/slopwatch/internal/report"
)

var ErrUnsupported = errors.New("native analyzer path is not yet supported")

type Options struct {
	Targets         []string
	Languages       []string
	IncludeTests    bool
	TypeScriptTypes bool
	Timeout         float64
	PassScore       *float64
	// ReadCache permits reuse of verified unit artifacts. Cache writes remain
	// enabled whenever a store is configured, independently of this option.
	ReadCache bool
}

type Analyzer struct {
	workspace string
	root      string
	catalog   catalogDocument
	options   Options
	optionsMu sync.RWMutex
	cache     analyzerCache
	runUnits  analyzerUnitsRunner
}

// SetTypeScriptTypes changes whether subsequent analyses build the optional
// compiler-aware TypeScript graph. In-flight analyses retain their snapshot.
func (analyzer *Analyzer) SetTypeScriptTypes(enabled bool) {
	analyzer.optionsMu.Lock()
	analyzer.options.TypeScriptTypes = enabled
	analyzer.optionsMu.Unlock()
}

func New(workspace, installationRoot string, options Options) (*Analyzer, error) {
	catalog, err := loadCatalog(installationRoot)
	if err != nil {
		return nil, err
	}
	return &Analyzer{workspace: workspace, root: installationRoot, catalog: catalog, options: options}, nil
}

func selectedLanguages(requested []string, discovered map[string][]string) ([]string, error) {
	selected := requested
	if len(selected) == 0 {
		for language := range discovered {
			selected = append(selected, language)
		}
		sort.Strings(selected)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no supported source files found")
	}
	for _, language := range selected {
		if len(discovered[language]) == 0 {
			return nil, fmt.Errorf("no source files discovered for: %s", language)
		}
	}
	return selected, nil
}

func activeCatalog(catalog catalogDocument, options Options) catalogDocument {
	active := catalog
	active.Components = append([]componentDescriptor(nil), catalog.Components...)
	if options.TypeScriptTypes {
		return active
	}
	for index := range active.Components {
		if active.Components[index].requiresTypeScriptTypes() {
			active.Components[index].Defaults.Enabled = false
		}
	}
	return active
}

func (analyzer *Analyzer) analyzeLanguage(parent context.Context, catalog catalogDocument, executable, language string, files []string, options Options) (scoreInputs, error) {
	components := []requestedComponent{}
	for _, descriptor := range catalog.Components {
		if descriptor.Defaults.Enabled && descriptor.supported(language) {
			components = append(components, requestedComponent{descriptor.ID, descriptor.Version})
		}
	}
	if len(components) == 0 {
		return newScoreInputs(), nil
	}
	invocation, err := invocationID()
	if err != nil {
		return scoreInputs{}, err
	}
	typeScriptMode := "off"
	if options.TypeScriptTypes {
		typeScriptMode = "auto"
	}
	requestOptions := map[string]any{"include_tests": options.IncludeTests, "typescript_types": typeScriptMode}
	request := analyzerRequest{"request", 1, invocation, analyzer.workspace, []protocolUnit{{language + "-unit", language, files, map[string]any{}}}, components, requestOptions, map[string]int{"max_seconds": int(options.Timeout)}}
	timeout := time.Duration(options.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	records, runErr := runAnalyzer(ctx, executable, request, options.Timeout)
	if runErr != nil {
		return scoreInputs{}, fmt.Errorf("%s analyzer failed: %w", language, runErr)
	}
	return records, nil
}

func (analyzer *Analyzer) Analyze(parent context.Context, targets []string, languages []string) (report.Document, error) {
	analyzer.optionsMu.RLock()
	options := analyzer.options
	analyzer.optionsMu.RUnlock()
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
	selected, err := selectedLanguages(options.Languages, discovered)
	if err != nil {
		return report.Document{}, err
	}
	catalog := activeCatalog(analyzer.catalog, options)
	// Cache writes and cache reads are deliberately independent. The default
	// slopmark path must remain the ordinary fresh analyzer plus a cheap
	// post-analysis projection write; only explicit reuse enters the package
	// coordinator and pays for planning, hashing, and snapshot validation.
	if options.ReadCache && analyzer.cacheStore() != nil {
		document, handled, cacheErr := analyzer.analyzeWithPersistentCache(parent, catalog, discovered, selected, options)
		if handled {
			return document, cacheErr
		}
	}
	document, err := analyzer.analyzeUncached(parent, catalog, discovered, selected, options)
	if err != nil {
		return report.Document{}, err
	}
	analyzer.persistProjection(document, options)
	return document, nil
}

func (analyzer *Analyzer) analyzeUncached(parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	inputsByLanguage := make([]scoreInputs, len(selected))
	errorsByLanguage := make([]error, len(selected))
	var workers sync.WaitGroup
	workers.Add(len(selected))
	for index, language := range selected {
		index, language := index, language
		go func() {
			defer workers.Done()
			executable := filepath.Join(analyzer.root, "analyzers", "structural", "slopslap-structural")
			if language == "typescript" {
				executable = filepath.Join(analyzer.root, "build", "typescript", "slopslap-typescript")
			}
			inputs, runErr := analyzer.analyzeLanguage(ctx, catalog, executable, language, discovered[language], options)
			if runErr != nil {
				errorsByLanguage[index] = runErr
				cancel()
				return
			}
			inputsByLanguage[index] = inputs
		}()
	}
	workers.Wait()
	inputs := newScoreInputs()
	for index := range selected {
		if errorsByLanguage[index] != nil {
			return report.Document{}, errorsByLanguage[index]
		}
		inputs.merge(inputsByLanguage[index])
	}
	return scoreInputsReport(catalog, selected, inputs, options.PassScore)
}

var ignored = map[string]bool{".git": true, ".gradle": true, ".idea": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, "build": true, "coverage": true, "dist": true, "node_modules": true, "out": true, "target": true, "vendor": true}
var testDirs = map[string]bool{"__tests__": true, "integration-test": true, "integration-tests": true, "integrationtest": true, "integrationtests": true, "spec": true, "specs": true, "test": true, "test-fixtures": true, "testfixtures": true, "tests": true}
var sourceLanguages = map[string]string{".go": "go", ".java": "java", ".rs": "rust", ".ts": "typescript", ".tsx": "typescript", ".mts": "typescript", ".cts": "typescript"}

func (analyzer *Analyzer) addDiscoveredPath(grouped map[string]map[string]bool, path string, includeTests bool) error {
	relative, err := filepath.Rel(analyzer.workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("analysis target escapes workspace: %s", path)
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		lower := strings.ToLower(part)
		if ignored[lower] || (!includeTests && testDirs[lower]) {
			return nil
		}
	}
	name := strings.ToLower(filepath.Base(relative))
	language := sourceLanguages[strings.ToLower(filepath.Ext(name))]
	if language == "" || (!includeTests && language == "go" && strings.HasSuffix(name, "_test.go")) || (!includeTests && language == "typescript" && typescriptTest(name)) {
		return nil
	}
	if grouped[language] == nil {
		grouped[language] = map[string]bool{}
	}
	grouped[language][filepath.ToSlash(relative)] = true
	return nil
}

func (analyzer *Analyzer) walkTarget(resolved string, grouped map[string]map[string]bool, includeTests bool) error {
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return analyzer.addDiscoveredPath(grouped, resolved, includeTests)
	}
	return filepath.WalkDir(resolved, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != resolved && ignored[strings.ToLower(entry.Name())] {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		return analyzer.addDiscoveredPath(grouped, path, includeTests)
	})
}

func (analyzer *Analyzer) discover(targets []string, includeTests bool) (map[string][]string, error) {
	if len(targets) == 0 {
		targets = []string{"."}
	}
	grouped := map[string]map[string]bool{}
	for _, target := range targets {
		absolute := target
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(analyzer.workspace, target)
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		if err := analyzer.walkTarget(resolved, grouped, includeTests); err != nil {
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
