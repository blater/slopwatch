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

	"github.com/slopslap/slopslap/internal/bridge"
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
	flags.DurationVar(&options.trendWindow, "trend-window", 15*time.Minute, "rank and edit-memory window")
	flags.BoolVar(&options.includeTests, "include-tests", false, "include test sources")
	flags.IntVar(&options.limit, "limit", 0, "maximum results; 0 returns all")
	flags.StringVar(&options.languages, "languages", "", "comma-separated languages")
	flags.Var(&options.backends, "backend", "language=backend override (repeatable)")
	flags.StringVar(&options.config, "config", "", "configuration file")
	flags.Float64Var(&options.timeout, "timeout", 120, "analyzer timeout in seconds")
	flags.StringVar(&options.passScore, "pass-score", "", "maximum passing score")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "usage: slopslap-go [OPTIONS] [TARGET ...]")
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
	workspace, err := os.Getwd()
	if err != nil {
		return err
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return err
	}
	targets := flags.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}
	languages := splitLanguages(parsed.languages)
	var passScore *float64
	if parsed.passScore != "" {
		value, parseErr := strconv.ParseFloat(parsed.passScore, 64)
		if parseErr != nil || value < 0 {
			return errors.New("--pass-score must be a non-negative number")
		}
		passScore = &value
	}
	reference, err := bridge.New(workspace, bridge.Options{
		Targets: targets, Languages: languages, IncludeTests: parsed.includeTests,
		Backends: parsed.backends, Config: parsed.config, Timeout: parsed.timeout,
		PassScore: passScore,
	})
	if err != nil {
		return err
	}
	if parsed.follow {
		var liveAnalyzer follow.Analyzer = reference
		var document report.Document
		if nativeEligible(workspace, parsed) {
			nativeAnalyzer, nativeErr := native.New(workspace, filepath.Dir(reference.Script), native.Options{
				Targets: targets, Languages: languages, IncludeTests: parsed.includeTests,
				Timeout: parsed.timeout, PassScore: passScore,
			})
			if nativeErr == nil {
				document, nativeErr = nativeAnalyzer.Analyze(context.Background(), nil, nil)
			}
			if nativeErr == nil {
				liveAnalyzer = nativeAnalyzer
			} else if !errors.Is(nativeErr, native.ErrUnsupported) {
				return nativeErr
			}
		}
		if len(document.Files) == 0 && liveAnalyzer == reference {
			document, err = reference.Analyze(context.Background(), nil, nil)
			if err != nil {
				return err
			}
		}
		model, modelErr := follow.New(document, liveAnalyzer, follow.Options{
			Workspace: workspace, Targets: targets, Languages: languages,
			IncludeTests: parsed.includeTests, Limit: parsed.limit,
			TrendWindow: parsed.trendWindow, Compact: parsed.compact,
		})
		if modelErr != nil {
			return modelErr
		}
		defer model.Close()
		follow.ConfigureTerminalColours()
		program := tea.NewProgram(model, tea.WithAltScreen())
		_, runErr := program.Run()
		return runErr
	}
	document, err := reference.Analyze(context.Background(), nil, nil)
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

func nativeEligible(workspace string, parsed *options) bool {
	if parsed.config != "" {
		return false
	}
	for _, backend := range parsed.backends {
		_, name, found := strings.Cut(backend, "=")
		if !found || name != "structural" {
			return false
		}
	}
	locations := []string{filepath.Join(workspace, ".slopslap.toml")}
	if home, err := os.UserHomeDir(); err == nil {
		locations = append(locations, filepath.Join(home, ".slopslap.toml"))
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" || !filepath.IsAbs(xdg) {
			xdg = filepath.Join(home, ".config")
		}
		locations = append(locations, filepath.Join(xdg, "slopslap", "config.toml"))
	}
	for _, path := range locations {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return false
		}
	}
	return true
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
	headers := []string{"RANK", "SCORE", "COG MAX/#", "NPATH MAX/#", "CYCLO TOT/MAX", "DEEP", "GOD", "PATH"}
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
	value, ok := report.Sum(file, "deeply_nested_if")
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
