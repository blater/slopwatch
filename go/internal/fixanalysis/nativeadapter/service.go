// Package nativeadapter is the sole translation boundary between native
// analysis reports and the provider-neutral fix analysis service.
package nativeadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blater/slopwatch/internal/fixanalysis"
	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
)

type Config struct {
	DisableGitignore  bool
	GitignoreDisabled func() (bool, error)
	InstallationRoot  string
	Languages         []string
	IncludeTests      bool
	TypeScriptTypes   bool
	ShallowProfile    string
	FollowSymlinks    bool
	// BaselineReadCache permits reuse of current analysis units for selected targets.
	BaselineReadCache bool
	Clock             func() time.Time
}

// AnalyzerOptions is the construction snapshot supplied to Factory. Baseline
// refresh can reuse current cache entries; final verification bypasses reads.
type AnalyzerOptions struct {
	DisableGitignore bool
	Languages        []string
	IncludeTests     bool
	TypeScriptTypes  bool
	ShallowProfile   string
	FollowSymlinks   bool
	ReadCache        bool
}

type Analyzer interface {
	Analyze(context.Context, []string, []string) (report.Document, error)
	ScoringIdentity() (string, error)
}

type Factory interface {
	New(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error)
}

type FactoryFunc func(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error)

func (function FactoryFunc) New(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error) {
	return function(workspace, installationRoot, options)
}

type Service struct {
	config  Config
	factory Factory
}

var _ fixanalysis.Service = (*Service)(nil)

func New(config Config) (*Service, error) {
	return NewWithFactory(config, nativeFactory{})
}

func NewWithFactory(config Config, factory Factory) (*Service, error) {
	if factory == nil {
		return nil, errors.New("native analysis adapter requires an analyzer factory")
	}
	if config.InstallationRoot == "" {
		return nil, errors.New("native analysis adapter requires an installation root")
	}
	config.Languages = append([]string(nil), config.Languages...)
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Service{config: config, factory: factory}, nil
}

func (service *Service) analyze(ctx context.Context, analysisRoot string, targets []string, readCache bool) (report.Document, string, error) {
	return service.analyzeReport(ctx, analysisRoot, targets, readCache, true)
}

func (service *Service) analyzeReport(ctx context.Context, analysisRoot string, targets []string, readCache, validateReport bool) (report.Document, string, error) {
	disabled := service.config.DisableGitignore
	if service.config.GitignoreDisabled != nil {
		var err error
		disabled, err = service.config.GitignoreDisabled()
		if err != nil {
			return report.Document{}, "", err
		}
	}
	options := AnalyzerOptions{
		DisableGitignore: disabled,
		Languages:        append([]string(nil), service.config.Languages...),
		IncludeTests:     service.config.IncludeTests, TypeScriptTypes: service.config.TypeScriptTypes,
		ShallowProfile: service.config.ShallowProfile,
		FollowSymlinks: service.config.FollowSymlinks, ReadCache: readCache,
	}
	analyzer, err := service.factory.New(analysisRoot, service.config.InstallationRoot, options)
	if err != nil {
		return report.Document{}, "", err
	}
	identity, err := analyzer.ScoringIdentity()
	if err != nil && validateReport {
		return report.Document{}, "", err
	}
	if identity == "" && validateReport {
		return report.Document{}, "", errors.New("analyzer returned an empty scoring identity")
	}
	document, err := analyzer.Analyze(ctx, append([]string(nil), targets...), append([]string(nil), options.Languages...))
	if err != nil {
		return report.Document{}, "", err
	}
	if validateReport && document.SchemaVersion <= 0 {
		return report.Document{}, "", errors.New("analyzer returned an invalid report schema version")
	}
	if validateReport && document.ProfileSetHash == "" {
		return report.Document{}, "", errors.New("analyzer returned an empty profile-set identity")
	}
	if validateReport && !document.Calibrated {
		return report.Document{}, "", errors.New("analyzer returned an uncalibrated report")
	}
	if identity != "" {
		identity = fmt.Sprintf("%s/report-schema-%d", identity, document.SchemaVersion)
	}
	return document, identity, nil
}

type nativeFactory struct{}

func (nativeFactory) New(workspace, installationRoot string, options AnalyzerOptions) (Analyzer, error) {
	return native.New(workspace, installationRoot, native.Options{
		DisableGitignore: options.DisableGitignore,
		Languages:        append([]string(nil), options.Languages...), IncludeTests: options.IncludeTests,
		TypeScriptTypes: options.TypeScriptTypes, FollowSymlinks: options.FollowSymlinks,
		ShallowProfile: options.ShallowProfile,
		ReadCache:      options.ReadCache,
	})
}
