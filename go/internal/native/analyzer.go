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

	"github.com/blater/slopwatch/internal/report"
)

var ErrUnsupported = errors.New("native analyzer path is not yet supported")

type Options struct {
	Targets         []string
	Languages       []string
	IncludeTests    bool
	TypeScriptTypes bool
	FollowSymlinks  bool
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

type analysisEngine Analyzer

func (analyzer *Analyzer) engine() *analysisEngine { return (*analysisEngine)(analyzer) }

func (analyzer *Analyzer) discover(targets []string, includeTests, followSymlinks bool) (map[string][]string, error) {
	return discover(analyzer.engine(), targets, includeTests, followSymlinks)
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

func analyzeLanguage(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, executable, language string, files []string, options Options) (scoreInputs, error) {
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
	unitOptions := map[string]any{"include_tests": options.IncludeTests, "follow_symlinks": options.FollowSymlinks}
	request := analyzerRequest{"request", 1, invocation, analyzer.workspace, []protocolUnit{{language + "-unit", language, files, unitOptions}}, components, requestOptions, map[string]int{}}
	records, runErr := runAnalyzer(parent, executable, request)
	if runErr != nil {
		return scoreInputs{}, fmt.Errorf("%s analyzer failed: %w", language, runErr)
	}
	return records, nil
}

func (analyzer *Analyzer) Analyze(parent context.Context, targets []string, languages []string) (report.Document, error) {
	return analyze(analyzer.engine(), parent, targets, languages)
}

func analysisOptions(analyzer *analysisEngine, targets, languages []string) Options {
	analyzer.optionsMu.RLock()
	options := analyzer.options
	analyzer.optionsMu.RUnlock()
	if targets != nil {
		options.Targets = targets
	}
	if languages != nil {
		options.Languages = languages
	}
	return options
}

func persistentCacheEnabled(analyzer *analysisEngine, options Options) bool {
	return options.ReadCache && !options.FollowSymlinks && cacheStore(analyzer) != nil
}

func analyzeFresh(
	analyzer *analysisEngine,
	parent context.Context,
	catalog catalogDocument,
	discovered map[string][]string,
	selected []string,
	options Options,
) (report.Document, error) {
	document, err := analyzeUncached(analyzer, parent, catalog, discovered, selected, options)
	if err != nil {
		return report.Document{}, err
	}
	if err := validateDocumentInventory(document, discovered, selected); err != nil {
		return report.Document{}, err
	}
	document.Summary["discovered_source_count"] = len(discoveredPathSet(discovered, selected))
	persistProjection(analyzer, document, options)
	return document, nil
}

func analyze(analyzer *analysisEngine, parent context.Context, targets []string, languages []string) (report.Document, error) {
	options := analysisOptions(analyzer, targets, languages)
	discovered, err := discover(analyzer, options.Targets, options.IncludeTests, options.FollowSymlinks)
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
	if persistentCacheEnabled(analyzer, options) {
		document, handled, cacheErr := analyzeWithPersistentCache(analyzer, parent, catalog, discovered, selected, options)
		if handled {
			return document, cacheErr
		}
	}
	return analyzeFresh(analyzer, parent, catalog, discovered, selected, options)
}

func validateDocumentInventory(document report.Document, discovered map[string][]string, selected []string) error {
	expected := discoveredPathSet(discovered, selected)
	actual := make(map[string]bool, len(document.Files))
	for _, file := range document.Files {
		actual[file.Path] = true
	}
	if len(actual) == len(expected) {
		complete := true
		for path := range expected {
			if !actual[path] {
				complete = false
				break
			}
		}
		if complete {
			return nil
		}
	}
	missing := 0
	for path := range expected {
		if !actual[path] {
			missing++
		}
	}
	return fmt.Errorf("analysis returned %d of %d discovered source files (%d missing)", len(actual), len(expected), missing)
}

func analyzeUncached(analyzer *analysisEngine, parent context.Context, catalog catalogDocument, discovered map[string][]string, selected []string, options Options) (report.Document, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	inputsByLanguage := make([]scoreInputs, len(selected))
	var firstError error
	var errorOnce sync.Once
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
			inputs, runErr := analyzeLanguage(analyzer, ctx, catalog, executable, language, discovered[language], options)
			if runErr != nil {
				errorOnce.Do(func() {
					firstError = runErr
					cancel()
				})
				return
			}
			inputsByLanguage[index] = inputs
		}()
	}
	workers.Wait()
	if firstError != nil {
		return report.Document{}, firstError
	}
	inputs := newScoreInputs()
	for index := range selected {
		inputs.merge(inputsByLanguage[index])
	}
	return scoreInputsReport(catalog, selected, inputs, options.PassScore)
}

var ignored = map[string]bool{".git": true, ".gradle": true, ".idea": true, ".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, "build": true, "coverage": true, "dist": true, "node_modules": true, "out": true, "target": true, "vendor": true}
var testDirs = map[string]bool{"__tests__": true, "integration-test": true, "integration-tests": true, "integrationtest": true, "integrationtests": true, "spec": true, "specs": true, "test": true, "test-fixtures": true, "testfixtures": true, "tests": true}
var sourceLanguages = map[string]string{".go": "go", ".java": "java", ".rs": "rust", ".ts": "typescript", ".tsx": "typescript", ".mts": "typescript", ".cts": "typescript"}

func addDiscoveredPath(analyzer *analysisEngine, grouped map[string]map[string]bool, path string, includeTests bool) error {
	language, include := discoveredLanguage(path, includeTests)
	if !include {
		return nil
	}
	relative, err := filepath.Rel(analyzer.workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("target %q is outside the analysis root %q", path, analyzer.workspace)
	}
	if excludedDiscoveryDirectory(relative, includeTests) {
		return nil
	}
	if grouped[language] == nil {
		grouped[language] = map[string]bool{}
	}
	grouped[language][filepath.ToSlash(relative)] = true
	return nil
}

func discoveredLanguage(path string, includeTests bool) (string, bool) {
	name := strings.ToLower(filepath.Base(path))
	language := sourceLanguages[strings.ToLower(filepath.Ext(name))]
	if language == "" {
		return "", false
	}
	if !includeTests && language == "go" && strings.HasSuffix(name, "_test.go") {
		return "", false
	}
	if !includeTests && language == "typescript" && typescriptTest(name) {
		return "", false
	}
	return language, true
}

func excludedDiscoveryDirectory(relative string, includeTests bool) bool {
	parts := strings.Split(filepath.ToSlash(relative), "/")
	for _, part := range parts[:max(0, len(parts)-1)] {
		lower := strings.ToLower(part)
		if ignored[lower] || (!includeTests && testDirs[lower]) {
			return true
		}
	}
	return false
}

func walkTarget(analyzer *analysisEngine, target string, grouped map[string]map[string]bool, includeTests, followSymlinks bool) error {
	metadata, err := os.Lstat(target)
	if err != nil {
		return err
	}
	info := os.FileInfo(metadata)
	if metadata.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(target)
		if err != nil {
			return err
		}
	}
	if !info.IsDir() {
		return addDiscoveredPath(analyzer, grouped, target, includeTests)
	}
	return walkDirectory(analyzer, target, grouped, includeTests, followSymlinks, map[string]bool{})
}

func walkDirectory(analyzer *analysisEngine, directory string, grouped map[string]map[string]bool, includeTests, followSymlinks bool, visited map[string]bool) error {
	if followSymlinks {
		resolved, err := filepath.EvalSymlinks(directory)
		if err != nil {
			return err
		}
		if visited[resolved] {
			return nil
		}
		visited[resolved] = true
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		isDirectory := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if !followSymlinks {
				continue
			}
			metadata, err := os.Stat(path)
			if err != nil {
				return err
			}
			isDirectory = metadata.IsDir()
		}
		if isDirectory {
			if ignored[strings.ToLower(entry.Name())] {
				continue
			}
			if err := walkDirectory(analyzer, path, grouped, includeTests, followSymlinks, visited); err != nil {
				return err
			}
			continue
		}
		if err := addDiscoveredPath(analyzer, grouped, path, includeTests); err != nil {
			return err
		}
	}
	return nil
}

func discover(analyzer *analysisEngine, targets []string, includeTests, followSymlinks bool) (map[string][]string, error) {
	if len(targets) == 0 {
		targets = []string{"."}
	}
	grouped := map[string]map[string]bool{}
	for _, target := range targets {
		absolute := target
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(analyzer.workspace, target)
		}
		if err := walkTarget(analyzer, filepath.Clean(absolute), grouped, includeTests, followSymlinks); err != nil {
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
