package analysiscache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// InputFingerprint records a canonical path and the digest of its verified
// contents. Sources and configuration inputs are kept separate in UnitKeyInput
// so changes to either are explicit in diagnostics and tests.
type InputFingerprint struct {
	Path        string `json:"path"`
	ContentHash Digest `json:"content_hash"`
}

// DependencyFingerprint identifies the exact result of a direct dependency.
type DependencyFingerprint struct {
	UnitID      string `json:"unit_id"`
	Fingerprint Key    `json:"fingerprint"`
}

// ComponentDefinition identifies an analyzer component implementation.
type ComponentDefinition struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

// UnitKeyInput contains every correctness-affecting analysis input. Presentation
// settings (weights, sort order, columns, pass threshold) are intentionally absent.
type UnitKeyInput struct {
	UnitID           string                  `json:"unit_id"`
	Language         string                  `json:"language"`
	Sources          []InputFingerprint      `json:"sources"`
	Configuration    []InputFingerprint      `json:"configuration"`
	Dependencies     []DependencyFingerprint `json:"dependencies"`
	AnalyzerDigest   Digest                  `json:"analyzer_digest"`
	FactVersion      string                  `json:"fact_version"`
	ProtocolVersion  string                  `json:"protocol_version"`
	CatalogVersion   string                  `json:"catalog_version"`
	Components       []ComponentDefinition   `json:"components"`
	ParserMode       string                  `json:"parser_mode"`
	TypeAnalysisMode string                  `json:"type_analysis_mode"`
	IncludeTests     bool                    `json:"include_tests"`
	Targets          []string                `json:"targets"`
	Languages        []string                `json:"languages"`
	Toolchain        map[string]string       `json:"toolchain"`
}

// UnitKey returns an order-independent, stable SHA-256 key. Paths are converted
// to slash-separated clean paths; callers should make them workspace-relative
// before constructing the input.
func UnitKey(input UnitKeyInput) (Key, error) {
	canonical := input
	canonical.Sources = canonicalInputs(input.Sources)
	canonical.Configuration = canonicalInputs(input.Configuration)
	canonical.Dependencies = append([]DependencyFingerprint{}, input.Dependencies...)
	sort.Slice(canonical.Dependencies, func(i, j int) bool {
		if canonical.Dependencies[i].UnitID != canonical.Dependencies[j].UnitID {
			return canonical.Dependencies[i].UnitID < canonical.Dependencies[j].UnitID
		}
		return canonical.Dependencies[i].Fingerprint < canonical.Dependencies[j].Fingerprint
	})
	canonical.Components = append([]ComponentDefinition{}, input.Components...)
	sort.Slice(canonical.Components, func(i, j int) bool {
		if canonical.Components[i].ID != canonical.Components[j].ID {
			return canonical.Components[i].ID < canonical.Components[j].ID
		}
		return canonical.Components[i].Version < canonical.Components[j].Version
	})
	canonical.Targets = canonicalStrings(input.Targets, true)
	canonical.Languages = canonicalStrings(input.Languages, false)
	if canonical.Toolchain == nil {
		canonical.Toolchain = map[string]string{}
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode unit cache key: %w", err)
	}
	return keyFor(payload), nil
}

// WorkspaceKey returns a stable identity for a canonical workspace path.
func WorkspaceKey(path string) (Key, error) {
	canonical, err := canonicalWorkspace(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace: %w", err)
	}
	return keyFor([]byte(canonical)), nil
}

// ViewOptions contains the scope that determines which rows belong in a
// workspace display. Presentation-only settings are intentionally absent.
type ViewOptions struct {
	Targets         []string `json:"targets"`
	Languages       []string `json:"languages"`
	IncludeTests    bool     `json:"include_tests"`
	TypeScriptTypes bool     `json:"typescript_types"`
	FollowSymlinks  bool     `json:"follow_symlinks"`
}

// WorkspaceViewKey isolates manifests and provisional projections for analysis
// scopes within the same repository. Targets and languages are order
// independent. Weights, sorting, columns, and pass thresholds do not
// participate in this identity.
func WorkspaceViewKey(path string, options ViewOptions) (ViewKey, error) {
	workspace, err := canonicalWorkspace(path)
	if err != nil {
		return "", fmt.Errorf("canonicalize workspace view: %w", err)
	}
	canonical := struct {
		Workspace string      `json:"workspace"`
		Options   ViewOptions `json:"options"`
	}{
		Workspace: workspace,
		Options:   options,
	}
	canonical.Options.Targets = canonicalStrings(options.Targets, true)
	canonical.Options.Languages = canonicalStrings(options.Languages, false)
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode workspace view key: %w", err)
	}
	return keyFor(payload), nil
}

// DigestBytes computes the content identity used by source blobs.
func DigestBytes(data []byte) Digest {
	sum := sha256.Sum256(data)
	return Digest(hex.EncodeToString(sum[:]))
}

func keyFor(data []byte) Key { return Key(DigestBytes(data)) }

func canonicalInputs(inputs []InputFingerprint) []InputFingerprint {
	result := append([]InputFingerprint{}, inputs...)
	for index := range result {
		result[index].Path = canonicalPath(result[index].Path)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		return result[i].ContentHash < result[j].ContentHash
	})
	return result
}

func canonicalStrings(values []string, paths bool) []string {
	result := append([]string{}, values...)
	if paths {
		for index := range result {
			result[index] = canonicalPath(result[index])
		}
	}
	sort.Strings(result)
	return compactSortedStrings(result)
}

func compactSortedStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func canonicalWorkspace(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Clean(resolved)), nil
}

func canonicalPath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." && strings.TrimSpace(path) == "" {
		return ""
	}
	return clean
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}
