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

	"github.com/slopslap/slopslap/internal/follow"
	"github.com/slopslap/slopslap/internal/native"
	"github.com/slopslap/slopslap/internal/report"
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
	format       string
	compact      bool
	follow       bool
	trendWindow  time.Duration
	includeTests bool
	limit        int
	languages    string
	backends     stringList
	config       string
	timeout      float64
	passScore    string
}

var errThreshold = errors.New("pass score exceeded")

func parser() (*flag.FlagSet, *options) {
	flags := flag.NewFlagSet("slopslap", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	options := &options{}
	flags.StringVar(&options.format, "format", "text", "select text or json output")
	flags.BoolVar(&options.compact, "c", false, "show only score and path")
	flags.BoolVar(&options.compact, "compact", false, "show only score and path")
	flags.BoolVar(&options.follow, "f", false, "open the live ranking dashboard")
	flags.BoolVar(&options.follow, "follow", false, "open the live ranking dashboard")
	flags.DurationVar(&options.trendWindow, "trend-window", 10*time.Minute, "movement indicator and edit-highlight window")
	flags.BoolVar(&options.includeTests, "include-tests", false, "include test sources")
	flags.IntVar(&options.limit, "limit", 0, "maximum results; 0 returns all")
	flags.StringVar(&options.languages, "languages", "", "comma-separated languages")
	flags.Var(&options.backends, "backend", "language=backend override (repeatable)")
	flags.StringVar(&options.config, "config", "", "configuration file")
	flags.Float64Var(&options.timeout, "timeout", 120, "analyzer timeout in seconds")
	flags.StringVar(&options.passScore, "pass-score", "", "maximum passing score")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: slopslap [OPTIONS] [TARGET ...]")
		fmt.Fprintln(flags.Output(), "\nNative Go frontend (analysis core transition build).")
		flags.PrintDefaults()
	}
	return flags, options
}

func main() {
	if err := run(os.Args[1:]); err != nil {
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
		return runFollow(workspace, targets, languages, parsed, passScore)
	}
	return runReport(workspace, targets, languages, parsed, passScore)
}

func validateOptions(parsed *options) error {
	if parsed.limit < 0 {
		return errors.New("--limit must be non-negative")
	}
	if parsed.timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if parsed.format != "text" && parsed.format != "json" {
		return errors.New("--format must be text or json")
	}
	if parsed.follow && parsed.format != "text" {
		return errors.New("--follow cannot be combined with --format json")
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

func runFollow(workspace string, targets, languages []string, parsed *options, passScore *float64) error {
	nativeAnalyzer, err := native.New(workspace, workspace, native.Options{
		Targets: targets, Languages: languages, IncludeTests: parsed.includeTests,
		Timeout: parsed.timeout, PassScore: passScore,
	})
	if err != nil {
		return err
	}
	model, modelErr := follow.New(report.Document{}, nativeAnalyzer, follow.Options{
		Workspace: workspace, Targets: targets, Languages: languages,
		IncludeTests: parsed.includeTests, Limit: parsed.limit,
		TrendWindow: parsed.trendWindow, Compact: parsed.compact,
	})
	if modelErr != nil {
		return modelErr
	}
	model.StartInitialAnalysis()
	defer model.Close()
	follow.ConfigureTerminalColours()
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, runErr := program.Run()
	return runErr
}

func runReport(workspace string, targets, languages []string, parsed *options, passScore *float64) error {
	var document report.Document
	var err error
	nativeAnalyzer, nativeErr := native.New(workspace, workspace, native.Options{
		Targets: targets, Languages: languages,
		IncludeTests: parsed.includeTests, Timeout: parsed.timeout, PassScore: passScore,
	})
	if nativeErr != nil {
		return nativeErr
	}
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
