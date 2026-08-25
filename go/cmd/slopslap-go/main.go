package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/blater/slopwatch/internal/follow"
	"github.com/blater/slopwatch/internal/isolation"
	isolationdocker "github.com/blater/slopwatch/internal/isolation/docker"
	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
)

type stringList []string

func (list *stringList) String() string { return strings.Join(*list, ",") }
func (list *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value cannot be empty")
	}
	*list = append(*list, value)
	return nil
}

type options struct {
	format          string
	compact         bool
	follow          bool
	trendWindow     time.Duration
	includeTests    bool
	typescriptTypes bool
	followSymlinks  bool
	limit           int
	languages       string
	backends        stringList
	config          string
	passScore       string
	useCache        bool
}

var errThreshold = errors.New("pass score exceeded")

func parser() (*flag.FlagSet, *options) {
	flags := flag.NewFlagSet("slopmark", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := &options{}
	flags.StringVar(&options.format, "format", "text", "select text or json output")
	flags.BoolVar(&options.compact, "c", false, "show only score and path")
	flags.BoolVar(&options.compact, "compact", false, "show only score and path")
	flags.BoolVar(&options.follow, "f", false, "open the live ranking dashboard")
	flags.BoolVar(&options.follow, "follow", false, "open the live ranking dashboard")
	flags.DurationVar(&options.trendWindow, "trend-window", 0, "movement indicator and edit-highlight window (default: saved preference or 10m)")
	flags.BoolVar(&options.includeTests, "include-tests", false, "include test sources")
	flags.BoolVar(&options.typescriptTypes, "typescript-types", false, "enable compiler-aware TypeScript type-safety analysis")
	flags.BoolVar(&options.followSymlinks, "follow-symlinks", false, "follow symbolic links found inside target directories")
	flags.IntVar(&options.limit, "limit", 0, "maximum results; 0 returns all")
	flags.StringVar(&options.languages, "languages", "", "comma-separated languages")
	flags.Var(&options.backends, "backend", "language=backend override (not supported by the native frontend)")
	flags.StringVar(&options.config, "config", "", "preferences file (follow mode)")
	flags.StringVar(&options.passScore, "pass-score", "", "maximum passing score")
	flags.BoolVar(&options.useCache, "use-cache", false, "reuse verified cached analysis units")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: slopmark [OPTIONS] [TARGET ...]")
		fmt.Fprintln(flags.Output(), "\nNative Go frontend (analysis core transition build).")
		flags.PrintDefaults()
	}
	return flags, options
}

func main() {
	// These hidden image entry points must be dispatched before ordinary
	// process supervision or flag parsing. The same installation-owned binary
	// is copied into the confinement image and acts as its PID 1 and empirical
	// descendant-escape probe.
	if handled, code := isolationdocker.ContainerEscapeProbeMain(os.Args[1:]); handled {
		os.Exit(code)
	}
	if handled, code := isolationdocker.ContainerSupervisorMain(os.Args[1:]); handled {
		os.Exit(code)
	}
	if handled, code := isolation.ProbeMain(os.Args[1:]); handled {
		os.Exit(code)
	}
	if handled, code := isolation.SupervisorMain(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		if errors.Is(err, errThreshold) {
			os.Exit(3)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
}

func run(arguments []string) error {
	if len(arguments) > 0 && arguments[0] == "analyze" {
		arguments = arguments[1:]
	}
	flags, parsed := parser()
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if err := validateOptions(parsed); err != nil {
		return err
	}
	workspace, err := absoluteWorkspace()
	if err != nil {
		return err
	}
	installationRoot, err := executableInstallationRoot()
	if err != nil {
		return err
	}
	targets := flags.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}
	languages := splitLanguages(parsed.languages)
	passScore, err := parsePassScore(parsed.passScore)
	if err != nil {
		return err
	}
	if parsed.follow {
		return runFollow(workspace, installationRoot, targets, languages, parsed, passScore)
	}
	return runReport(workspace, installationRoot, targets, languages, parsed, passScore)
}

func validateOptions(parsed *options) error {
	if parsed.limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if parsed.format != "text" && parsed.format != "json" {
		return errors.New("--format must be text or json")
	}
	if parsed.follow && parsed.format != "text" {
		return errors.New("--follow cannot be combined with --format json")
	}
	if len(parsed.backends) != 0 {
		return errors.New("--backend is not supported by the native frontend")
	}
	if parsed.config != "" && !parsed.follow {
		return errors.New("--config requires --follow")
	}
	return nil
}

func absoluteWorkspace() (string, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(workspace)
}

func executableInstallationRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	// The bundled layout is <root>/build/slopmark.
	return filepath.Dir(filepath.Dir(executable)), nil
}

func parsePassScore(raw string) (*float64, error) {
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		return nil, errors.New("--pass-score must be a non-negative number")
	}
	return &value, nil
}

func runFollow(workspace, installationRoot string, targets, languages []string, parsed *options, passScore *float64) error {
	nativeAnalyzer, err := native.New(workspace, installationRoot, native.Options{
		Targets: targets, Languages: languages, IncludeTests: parsed.includeTests,
		TypeScriptTypes: parsed.typescriptTypes, FollowSymlinks: parsed.followSymlinks,
		PassScore: passScore,
		ReadCache: true,
	})
	if err != nil {
		return err
	}
	nativeAnalyzer.EnableDefaultCache()
	initial := report.Document{}
	if cached, ok := nativeAnalyzer.CachedProjection(); ok {
		initial = cached
	} else {
		// A first-ever dashboard launch should be no slower than slopmark's
		// ordinary fresh scan. Reuse is enabled after that result is visible.
		nativeAnalyzer.SetCacheReads(false)
	}
	preferencesPath := parsed.config
	if preferencesPath == "" {
		var pathErr error
		preferencesPath, pathErr = preferences.DefaultPath()
		if pathErr != nil {
			return pathErr
		}
	} else {
		preferencesPath, err = filepath.Abs(preferencesPath)
		if err != nil {
			return fmt.Errorf("resolve preferences path: %w", err)
		}
	}
	fixFeature, fixErr := buildFixFeature(context.Background(), workspace, installationRoot, preferencesPath, parsed, languages)
	follow.ConfigureTerminalColours()
	followOptions := follow.Options{
		Workspace: workspace, Targets: targets, Languages: languages,
		IncludeTests: parsed.includeTests, Limit: parsed.limit,
		FollowSymlinks: parsed.followSymlinks,
		TrendWindow:    parsed.trendWindow, Compact: parsed.compact, TypeScriptTypes: parsed.typescriptTypes,
		PreferencesPath: preferencesPath,
	}
	if fixFeature != nil {
		followOptions.FixWorkspace = fixFeature.workspace
		followOptions.ConfigStore = fixFeature.config
		followOptions.ConfigWorkspace = fixFeature.workspace
		followOptions.ProfileProber = fixFeature.prober
		followOptions.ProfileCatalog = fixFeature.catalog
	}
	if fixErr == nil && fixFeature != nil {
		followOptions.FixService = fixFeature.service
	} else if fixErr != nil {
		followOptions.FixUnavailableReason = fixErr.Error()
	}
	model, modelErr := follow.New(initial, nativeAnalyzer, followOptions)
	if modelErr != nil {
		_ = closeFixFeature(fixFeature)
		return modelErr
	}
	model.StartInitialAnalysis()
	defer model.Close()
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, runErr := program.Run()
	closeErr := closeFixFeature(fixFeature)
	return errors.Join(runErr, closeErr)
}

func runReport(workspace, installationRoot string, targets, languages []string, parsed *options, passScore *float64) error {
	var document report.Document
	var err error
	nativeAnalyzer, nativeErr := native.New(workspace, installationRoot, native.Options{
		Targets: targets, Languages: languages,
		IncludeTests: parsed.includeTests, TypeScriptTypes: parsed.typescriptTypes,
		FollowSymlinks: parsed.followSymlinks,
		PassScore:      passScore, ReadCache: parsed.useCache,
	})
	if nativeErr != nil {
		return nativeErr
	}
	nativeAnalyzer.EnableDefaultCache()
	document, err = nativeAnalyzer.Analyze(context.Background(), targets, languages)
	if err != nil {
		return err
	}
	document.SortAndRank()
	if parsed.limit > 0 && len(document.Files) > parsed.limit {
		document.Files = document.Files[:parsed.limit]
		document.ReturnedFiles = len(document.Files)
		document.Truncated = true
	}
	if parsed.format == "json" {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(document); err != nil {
			return err
		}
		if passScore != nil && !summaryPassed(document) {
			return errThreshold
		}
		return nil
	}
	fmt.Println(renderTable(document, parsed.compact, passScore != nil))
	if passScore != nil && !summaryPassed(document) {
		return errThreshold
	}
	return nil
}

func summaryPassed(document report.Document) bool {
	passed, ok := document.Summary["passed"].(bool)
	return !ok || passed
}

func splitLanguages(value string) []string {
	seen := map[string]bool{}
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func renderTable(document report.Document, compact, includePass bool) string {
	headers := []string{"RANK", "SCORE", "COG MAX/#", "NPATH MAX/#", "CYCLO TOT/MAX", "SHALLOW", "GOD", "PATH"}
	if compact {
		headers = []string{"SCORE", "PATH"}
	} else if includePass {
		headers = append([]string{"PASS"}, headers...)
	}
	rows := make([][]string, 0, len(document.Files))
	for _, file := range document.Files {
		if compact {
			rows = append(rows, []string{report.DisplayNumber(file.Score), file.Path})
			continue
		}
		row := []string{
			strconv.Itoa(file.Rank), report.DisplayNumber(file.Score), maxCount(file, "cognitive_complexity"),
			maxCount(file, "npath_complexity"), cyclomatic(file), depth(file), contribution(file, "god_class"), file.Path,
		}
		if includePass {
			passed := "NO"
			if file.Passed != nil && *file.Passed {
				passed = "yes"
			}
			row = append([]string{passed}, row...)
		}
		rows = append(rows, row)
	}
	widths := make([]int, len(headers))
	for i, value := range headers {
		widths[i] = len(value)
	}
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], len(value))
		}
	}
	format := func(row []string) string {
		parts := make([]string, len(row))
		for i, value := range row {
			if i == len(row)-1 {
				parts[i] = fmt.Sprintf("%-*s", widths[i], value)
			} else {
				parts[i] = fmt.Sprintf("%*s", widths[i], value)
			}
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	lines := []string{format(headers)}
	for _, row := range rows {
		lines = append(lines, format(row))
	}
	return strings.Join(lines, "\n")
}

func maxCount(file report.File, id string) string {
	component, ok := file.Components[id]
	if !ok {
		return "-"
	}
	value, _ := report.Max(file, id)
	return fmt.Sprintf("%s/%d", report.DisplayNumber(value), len(component.Subjects))
}
func cyclomatic(file report.File) string {
	typeValue, typeOK := report.Max(file, "cyclomatic_class_complexity")
	methodValue, methodOK := report.Max(file, "cyclomatic_method_complexity")
	if !typeOK && !methodOK {
		return "-"
	}
	left, right := "-", "-"
	if typeOK {
		left = report.DisplayNumber(typeValue)
	}
	if methodOK {
		right = report.DisplayNumber(methodValue)
	}
	return left + "/" + right
}
func depth(file report.File) string {
	value, ok := report.Max(file, "module_shallowness")
	if !ok {
		return "-"
	}
	return report.DisplayNumber(value)
}
func contribution(file report.File, id string) string {
	value, ok := report.Contribution(file, id)
	if !ok {
		return "-"
	}
	return report.DisplayNumber(value)
}
