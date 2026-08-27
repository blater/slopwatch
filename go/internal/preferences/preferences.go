// Package preferences owns the versioned, user-editable Slopwatch preferences
// document and its durable storage lifecycle.
package preferences

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/blater/slopwatch/internal/fixprompt"
	"github.com/blater/slopwatch/internal/scoring"
	"github.com/pelletier/go-toml/v2"

	"github.com/blater/slopwatch/internal/userdata"
)

const CurrentVersion = 1

type Document struct {
	Version     int         `toml:"version"`
	Appearance  Appearance  `toml:"appearance"`
	Table       Table       `toml:"table"`
	Interaction Interaction `toml:"interaction"`
	Scoring     Scoring     `toml:"scoring"`
	Agents      Agents      `toml:"agents"`
	Fix         Fix         `toml:"fix"`
	Concurrency Concurrency `toml:"concurrency"`
	Delivery    Delivery    `toml:"delivery"`
}

type Appearance struct {
	Theme string `toml:"theme"`
}

type Table struct {
	VisibleColumns []string `toml:"visible_columns"`
	SortBy         string   `toml:"sort_by"`
	SortDescending bool     `toml:"sort_descending"`
}

type Interaction struct {
	TrendWindow string `toml:"trend_window"`
}

type Scoring struct {
	WeightStep    float64                        `toml:"weight_step"`
	MaximumWeight float64                        `toml:"maximum_weight"`
	Components    map[string]ComponentPreference `toml:"components"`
}

type ComponentPreference struct {
	Enabled bool    `toml:"enabled"`
	Weight  float64 `toml:"weight"`
}

type Agents struct {
	Profiles []AgentProfile `toml:"profiles"`
}

type AgentProfile struct {
	ID                string            `toml:"id"`
	Label             string            `toml:"label"`
	Runtime           string            `toml:"runtime"`
	Executable        string            `toml:"executable"`
	RuntimeProfile    string            `toml:"runtime_profile"`
	AuthenticationRef string            `toml:"authentication_ref"`
	Options           map[string]string `toml:"options"`
}

type Fix struct {
	TargetScore    float64  `toml:"target_score"`
	Focus          []string `toml:"focus"`
	ChangeScope    string   `toml:"change_scope"`
	Profile        string   `toml:"profile"`
	Model          string   `toml:"model"`
	Effort         string   `toml:"effort"`
	PromptTemplate string   `toml:"prompt_template"`
}

type Concurrency struct {
	MaxAgents                int   `toml:"max_agents"`
	MaxVerifiers             int   `toml:"max_verifiers"`
	MaxActorsPerJob          int   `toml:"max_actors_per_job"`
	MaxCandidatePreviewBytes int64 `toml:"max_candidate_preview_bytes"`
	MaxCandidatePreviewLines int   `toml:"max_candidate_preview_lines"`
}

type Delivery struct {
	Workspace                string `toml:"workspace"`
	Git                      string `toml:"git"`
	Publish                  string `toml:"publish"`
	Remote                   string `toml:"remote"`
	BaseBranch               string `toml:"base_branch"`
	BranchTemplate           string `toml:"branch_template"`
	Publisher                string `toml:"publisher"`
	DraftPullRequests        bool   `toml:"draft_pull_requests"`
	CommandOutputBytes       int64  `toml:"command_output_bytes"`
	CommitTitleTemplate      string `toml:"commit_title_template"`
	CommitBodyTemplate       string `toml:"commit_body_template"`
	PullRequestTitleTemplate string `toml:"pull_request_title_template"`
	PullRequestBodyTemplate  string `toml:"pull_request_body_template"`
}

// PartialDocument retains which top-level preference groups were explicitly
// present. It is used for repository-scoped overrides and origin metadata;
// ordinary consumers continue using Document.
type PartialDocument struct {
	Version     *int         `toml:"version,omitempty"`
	Appearance  *Appearance  `toml:"appearance,omitempty"`
	Table       *Table       `toml:"table,omitempty"`
	Interaction *Interaction `toml:"interaction,omitempty"`
	Scoring     *Scoring     `toml:"scoring,omitempty"`
	Agents      *Agents      `toml:"agents,omitempty"`
	Fix         *Fix         `toml:"fix,omitempty"`
	Concurrency *Concurrency `toml:"concurrency,omitempty"`
	Delivery    *Delivery    `toml:"delivery,omitempty"`
}

// DefaultDocument returns a complete independent preferences document.
func DefaultDocument() Document {
	components := make(map[string]ComponentPreference)
	for _, component := range scoring.Components() {
		components[component.ID] = ComponentPreference{Enabled: component.DefaultOn, Weight: component.DefaultWeight}
	}
	return Document{
		Version:    CurrentVersion,
		Appearance: Appearance{Theme: "dark"},
		Table: Table{
			VisibleColumns: []string{"cog", "npath", "cyclo", "deep", "god", "coupling"},
			SortBy:         "score", SortDescending: true,
		},
		Interaction: Interaction{TrendWindow: (10 * time.Minute).String()},
		Scoring:     Scoring{WeightStep: 0.5, MaximumWeight: 20, Components: components},
		Fix: Fix{
			TargetScore: 100, Focus: []string{"score"}, ChangeScope: "repository",
			PromptTemplate: fixprompt.DefaultTemplate,
		},
		Concurrency: Concurrency{
			MaxAgents:                2,
			MaxVerifiers:             1,
			MaxActorsPerJob:          32,
			MaxCandidatePreviewBytes: 4 * 1024 * 1024,
			MaxCandidatePreviewLines: 5000,
		},
		Delivery: Delivery{
			Workspace: "current-files", Git: "uncommitted", Publish: "local", Remote: "origin", BaseBranch: "main",
			BranchTemplate: "slopwatch/fix/{target-stem}-{job-short-id}", Publisher: "github-cli",
			DraftPullRequests:   true,
			CommandOutputBytes:  4 * 1024 * 1024,
			CommitTitleTemplate: "Refactor {targets} with Slopwatch",
			CommitBodyTemplate:  "Automated remediation for {goal}.", PullRequestTitleTemplate: "Refactor {targets} with Slopwatch",
			PullRequestBodyTemplate: "Automated remediation for {goal}.",
		},
	}
}

func DefaultPath() (string, error) {
	root, err := userdata.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "preferences.toml"), nil
}

func LoadOrCreate(path string, defaults Document) (Document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		value := Clone(defaults)
		if err := Save(path, value); err != nil {
			return Document{}, err
		}
		return value, nil
	}
	if err != nil {
		return Document{}, fmt.Errorf("read preferences %s: %w", path, err)
	}
	value := Clone(defaults)
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Recover(path, defaults)
	}
	if value.Version != CurrentVersion {
		return Recover(path, defaults)
	}
	return value, nil
}

// Recover preserves an unreadable or invalid preferences document beside the
// live file and replaces it with current defaults. This is deliberately a
// reset, not schema migration or compatibility parsing.
func Recover(path string, defaults Document) (Document, error) {
	if err := Quarantine(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Document{}, fmt.Errorf("preserve invalid preferences %s: %w", path, err)
	}
	value := Clone(defaults)
	if err := Save(path, value); err != nil {
		return Document{}, err
	}
	return value, nil
}

// Quarantine moves a bad preferences document out of the live path while
// keeping it in the same Slopwatch directory for inspection.
func Quarantine(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	return replaceFile(path, path+".invalid")
}

func Save(path string, value Document) error {
	return save(path, value)
}

// LoadPartial reads a preferences document while retaining which top-level
// groups were present. A missing document is not an error.
func LoadPartial(path string) (PartialDocument, []byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PartialDocument{}, nil, false, nil
	}
	if err != nil {
		return PartialDocument{}, nil, false, fmt.Errorf("read preferences %s: %w", path, err)
	}
	value, err := DecodePartial(data)
	if err != nil {
		return PartialDocument{}, nil, false, fmt.Errorf("decode preferences %s: %w", path, err)
	}
	return value, append([]byte(nil), data...), true, nil
}

// DecodePartial decodes a strict partial preferences document.
func DecodePartial(data []byte) (PartialDocument, error) {
	var value PartialDocument
	decoder := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return PartialDocument{}, err
	}
	if value.Version != nil && *value.Version != CurrentVersion {
		return PartialDocument{}, fmt.Errorf("schema version %d is not supported; supported version is %d", *value.Version, CurrentVersion)
	}
	return ClonePartial(value), nil
}

// SavePartial writes a strict partial preferences document atomically. The
// current schema version is always included in persisted documents.
func SavePartial(path string, value PartialDocument) error {
	value = ClonePartial(value)
	version := CurrentVersion
	value.Version = &version
	return save(path, value)
}

func save(path string, value any) error {
	data, err := toml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	header := []byte("# Slopwatch preferences. Managed by the dashboard; manual edits are read at launch.\n# CLI options override matching values for that run.\n")
	data = append(header, data...)
	if err := writeAtomic(path, data); err != nil {
		return fmt.Errorf("write preferences %s: %w", path, err)
	}
	return nil
}

// Clone returns a document that shares no mutable slices or maps with value.
func Clone(value Document) Document {
	result := value
	result.Table.VisibleColumns = append([]string(nil), value.Table.VisibleColumns...)
	result.Scoring.Components = make(map[string]ComponentPreference, len(value.Scoring.Components))
	for id, component := range value.Scoring.Components {
		result.Scoring.Components[id] = component
	}
	result.Agents.Profiles = make([]AgentProfile, len(value.Agents.Profiles))
	for index, profile := range value.Agents.Profiles {
		result.Agents.Profiles[index] = profile
		result.Agents.Profiles[index].Options = cloneStrings(profile.Options)
	}
	result.Fix.Focus = append([]string(nil), value.Fix.Focus...)
	return result
}

// ClonePartial returns a partial document that shares no mutable values with
// its input and preserves nil pointers (field presence).
func ClonePartial(value PartialDocument) PartialDocument {
	result := value
	if value.Version != nil {
		version := *value.Version
		result.Version = &version
	}
	if value.Appearance != nil {
		item := *value.Appearance
		result.Appearance = &item
	}
	if value.Table != nil {
		item := *value.Table
		item.VisibleColumns = append([]string(nil), value.Table.VisibleColumns...)
		result.Table = &item
	}
	if value.Interaction != nil {
		item := *value.Interaction
		result.Interaction = &item
	}
	if value.Scoring != nil {
		item := *value.Scoring
		item.Components = make(map[string]ComponentPreference, len(value.Scoring.Components))
		for id, component := range value.Scoring.Components {
			item.Components[id] = component
		}
		result.Scoring = &item
	}
	if value.Agents != nil {
		item := Agents{Profiles: make([]AgentProfile, len(value.Agents.Profiles))}
		for index, profile := range value.Agents.Profiles {
			item.Profiles[index] = profile
			item.Profiles[index].Options = cloneStrings(profile.Options)
		}
		result.Agents = &item
	}
	if value.Fix != nil {
		item := *value.Fix
		item.Focus = append([]string(nil), value.Fix.Focus...)
		result.Fix = &item
	}
	if value.Concurrency != nil {
		item := *value.Concurrency
		result.Concurrency = &item
	}
	if value.Delivery != nil {
		item := *value.Delivery
		result.Delivery = &item
	}
	return result
}

func cloneStrings(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".preferences-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceFile(temporaryPath, path)
}
