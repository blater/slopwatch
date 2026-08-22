package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/slopslap/slopslap/internal/report"
)

type Options struct {
	Workspace    string
	Targets      []string
	Languages    []string
	IncludeTests bool
	Backends     []string
	Config       string
	Timeout      float64
	PassScore    *float64
}

type Analyzer struct {
	Python string
	Script string
	Base   Options
}

func New(workspace string, options Options) (*Analyzer, error) {
	script := os.Getenv("SLOPSLAP_PYTHON_APP")
	if script == "" {
		var err error
		script, err = findApplication(workspace)
		if err != nil {
			return nil, err
		}
	}
	python := os.Getenv("SLOPSLAP_PYTHON")
	if python == "" {
		python = "python3"
	}
	options.Workspace = workspace
	return &Analyzer{Python: python, Script: script, Base: options}, nil
}

func findApplication(start string) (string, error) {
	starts := []string{start}
	if executable, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(executable))
	}
	for _, initial := range starts {
		directory, err := filepath.Abs(initial)
		if err != nil {
			continue
		}
		for {
			candidate := filepath.Join(directory, "slopslap.py")
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
			directory = parent
		}
	}
	return "", fmt.Errorf("cannot locate slopslap.py; set SLOPSLAP_PYTHON_APP")
}

func (analyzer *Analyzer) analysisOptions(targets, languages []string) Options {
	options := analyzer.Base
	if targets != nil {
		options.Targets = targets
	}
	if languages != nil {
		options.Languages = languages
		selected := map[string]bool{}
		for _, language := range languages {
			selected[language] = true
		}
		backends := options.Backends[:0]
		for _, backend := range options.Backends {
			language, _, _ := strings.Cut(backend, "=")
			if selected[language] {
				backends = append(backends, backend)
			}
		}
		options.Backends = backends
	}
	return options
}

func (analyzer *Analyzer) analysisArgs(options Options) []string {
	args := []string{analyzer.Script, "analyze", "--format", "json", "--limit", "0"}
	if options.Config != "" {
		args = append(args, "--config", options.Config)
	}
	if len(options.Languages) > 0 {
		args = append(args, "--languages", strings.Join(options.Languages, ","))
	}
	if options.IncludeTests {
		args = append(args, "--include-tests")
	}
	for _, backend := range options.Backends {
		args = append(args, "--backend", backend)
	}
	if options.Timeout > 0 {
		args = append(args, "--timeout", strconv.FormatFloat(options.Timeout, 'f', -1, 64))
	}
	if options.PassScore != nil {
		args = append(args, "--pass-score", strconv.FormatFloat(*options.PassScore, 'f', -1, 64))
	}
	args = append(args, options.Targets...)
	return args
}

func decodeCommandFailure(err error, stdout, stderr *bytes.Buffer) (report.Document, error) {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 3 && stdout.Len() > 0 {
		return report.Decode(stdout.Bytes())
	}
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		message = err.Error()
	}
	return report.Document{}, fmt.Errorf("analysis failed: %s", message)
}

func (analyzer *Analyzer) Analyze(ctx context.Context, targets []string, languages []string) (report.Document, error) {
	options := analyzer.analysisOptions(targets, languages)
	args := analyzer.analysisArgs(options)
	command := exec.CommandContext(ctx, analyzer.Python, args...)
	command.Dir = options.Workspace
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return decodeCommandFailure(err, &stdout, &stderr)
	}
	if stderr.Len() > 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	return report.Decode(stdout.Bytes())
}
