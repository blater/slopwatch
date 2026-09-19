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
	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/preferences"
	"github.com/blater/slopwatch/internal/report"
	"github.com/blater/slopwatch/internal/userdata"
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
	shallowProfile  string
	scoreProfile    string
	typescriptTypes bool
	followSymlinks  bool
	limit           int
	languages       string
	backends        stringList
	config          string
	passScore       string
	useCache        bool
}

const shallowProfileLegacy = native.ShallowProfileLegacy

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
	flags.StringVar(&options.shallowProfile, "shallow-profile", "", "select the SHALLOW profile (default responsibility-v4; legacy-signature-v3 for comparison)")
	flags.StringVar(&options.scoreProfile, "score-profile", "", "select the scoring profile (legacy-signature-v3 or responsibility-v4)")
	flags.BoolVar(&options.typescriptTypes, "typescript-types", false, "enable compiler-aware TypeScript type-safety analysis")
	flags.BoolVar(&options.followSymlinks, "follow-symlinks", false, "follow symbolic links found inside target directories")
	flags.IntVar(&options.limit, "limit", 0, "maximum results; 0 returns all")
	flags.StringVar(&options.languages, "languages", "", "comma-separated languages")
	flags.Var(&options.backends, "backend", "language=backend override (not supported by the native frontend)")
	flags.StringVar(&options.config, "config", "", "preferences file")
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
	profile, err := selectedShallowProfile(parsed)
	if err != nil {
		return err
	}
	parsed.shallowProfile = profile
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
	_, err := selectedShallowProfile(parsed)
	return err
}

func selectedShallowProfile(parsed *options) (string, error) {
	if parsed.shallowProfile != "" && parsed.scoreProfile != "" && parsed.shallowProfile != parsed.scoreProfile {
		return "", errors.New("--shallow-profile and --score-profile must agree")
	}
	profile := parsed.scoreProfile
	if profile == "" {
		profile = parsed.shallowProfile
	}
	switch profile {
	case "":
		return native.ShallowProfileResponsibilityV4, nil
	case shallowProfileLegacy:
		return shallowProfileLegacy, nil
	case native.ShallowProfileResponsibilityV4:
		return native.ShallowProfileResponsibilityV4, nil
	default:
		return "", fmt.Errorf("score profile must be %s or %s", shallowProfileLegacy, native.ShallowProfileResponsibilityV4)
	}
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
		TypeScriptTypes: parsed.typescriptTypes, ShallowProfile: parsed.shallowProfile, FollowSymlinks: parsed.followSymlinks,
		PassScore: passScore,
		ReadCache: true,
	})
	if err != nil {
		return err
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
	pref, err := preferences.LoadOrCreate(preferencesPath, preferences.DefaultDocument())
	if err != nil {
		return err
	}
	nativeAnalyzer.SetDisableGitignore(!pref.Files.HonorGitignore)
	userDataRoot, err := userdata.Root()
	if err != nil {
		return err
	}
	nativeAnalyzer.EnableCache(filepath.Join(userDataRoot, "analysis"))
	initial := report.Document{}
	if cached, ok := nativeAnalyzer.CachedProjection(); ok {
		initial = cached
	} else {
		// A first-ever dashboard launch should be no slower than slopmark's
		// ordinary fresh scan. Reuse is enabled after that result is visible.
		nativeAnalyzer.SetCacheReads(false)
	}
	fixFeature, fixErr := buildFixFeature(context.Background(), workspace, installationRoot, preferencesPath, userDataRoot, parsed, languages)
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
	preferencesPath := parsed.config
	if preferencesPath == "" {
		var err error
		preferencesPath, err = preferences.DefaultPath()
		if err != nil {
			return err
		}
	}
	pref, prefErr := preferences.LoadExisting(preferencesPath, preferences.DefaultDocument())
	if prefErr != nil {
		return prefErr
	}
	var document report.Document
	var err error
	nativeAnalyzer, nativeErr := native.New(workspace, installationRoot, native.Options{
		Targets: targets, Languages: languages,
		IncludeTests: parsed.includeTests, TypeScriptTypes: parsed.typescriptTypes, ShallowProfile: parsed.shallowProfile,
		FollowSymlinks: parsed.followSymlinks,
		PassScore:      passScore, ReadCache: parsed.useCache,
	})
	if nativeErr != nil {
		return nativeErr
	}
	nativeAnalyzer.SetDisableGitignore(!pref.Files.HonorGitignore)
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
		if err := encoder.Encode(document); err != nil {
			return err
		}
		if passScore != nil && !summaryPassed(document) {
			return errThreshold
		}
		return nil
	}
	fmt.Println(renderTable(document, parsed.compact, passScore != nil))
	if diagnostics := renderDiagnostics(document); diagnostics != "" {
		fmt.Println(diagnostics)
	}
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
