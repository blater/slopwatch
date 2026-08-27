package native

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	"github.com/blater/slopmochi/internal/analysiscache"
	"github.com/blater/slopmochi/internal/unitplan"
)

func analysisCatalogDigest(catalog catalogDocument) (analysiscache.Digest, error) {
	type componentIdentity struct {
		ID               string            `json:"id"`
		Version          string            `json:"version"`
		Axis             string            `json:"axis"`
		Kind             string            `json:"kind"`
		Aggregator       string            `json:"aggregator"`
		DeduplicationKey []string          `json:"deduplication_key"`
		Support          map[string]string `json:"support"`
		Enabled          bool              `json:"enabled"`
	}
	identity := struct {
		Languages  []string             `json:"languages"`
		Analyzers  []analyzerDescriptor `json:"analyzers"`
		Components []componentIdentity  `json:"components"`
	}{Languages: catalog.Languages, Analyzers: catalog.Analyzers}
	for _, component := range catalog.Components {
		identity.Components = append(identity.Components, componentIdentity{
			ID: component.ID, Version: component.Version, Axis: component.Axis,
			Kind: component.Kind, Aggregator: component.Aggregator,
			DeduplicationKey: component.DeduplicationKey, Support: component.Support,
			Enabled: component.Defaults.Enabled,
		})
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return analysiscache.DigestBytes(encoded), nil
}

func unitKeyInput(unit unitplan.Unit, dependencies []analysiscache.DependencyFingerprint, digests map[string]analysiscache.Digest, analyzerDigest, catalogDigest analysiscache.Digest, catalog catalogDocument, options Options) analysiscache.UnitKeyInput {
	sources := fingerprints(append(append([]string{}, unit.Sources...), unit.ContextSources...), digests)
	configuration := fingerprints(unit.ConfigInputs, digests)
	typeMode := "off"
	if options.TypeScriptTypes {
		typeMode = "auto"
	}
	components := componentsForLanguage(catalog, string(unit.Language))
	definitions := make([]analysiscache.ComponentDefinition, len(components))
	for index, component := range components {
		definitions[index] = analysiscache.ComponentDefinition{ID: component.ID, Version: component.Version}
	}
	return analysiscache.UnitKeyInput{
		UnitID: unit.ID, Language: string(unit.Language), Sources: sources,
		Configuration: configuration, Dependencies: dependencies,
		AnalyzerDigest: analyzerDigest, FactVersion: nativeFactVersion,
		ProtocolVersion: nativeProtocolVersion, CatalogVersion: string(catalogDigest),
		Components: definitions, ParserMode: string(unit.Mode), TypeAnalysisMode: typeMode,
		IncludeTests: unitOutputIncludesTests(unit),
		Toolchain:    map[string]string{"go_runtime": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH},
	}
}

func unitOutputIncludesTests(unit unitplan.Unit) bool {
	return hasCapability(unit, unitplan.CapabilityTests) || containsTestPath(unit.Sources, string(unit.Language))
}

func fingerprints(paths []string, digests map[string]analysiscache.Digest) []analysiscache.InputFingerprint {
	seen := make(map[string]bool, len(paths))
	result := make([]analysiscache.InputFingerprint, 0, len(paths))
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			result = append(result, analysiscache.InputFingerprint{Path: path, ContentHash: digests[path]})
		}
	}
	return result
}

func componentsForLanguage(catalog catalogDocument, language string) []requestedComponent {
	result := []requestedComponent{}
	for _, descriptor := range catalog.Components {
		if descriptor.Defaults.Enabled && descriptor.supported(language) {
			result = append(result, requestedComponent{ID: descriptor.ID, Version: descriptor.Version})
		}
	}
	return result
}

func backendDigest(analyzer *analysisEngine, language string) (analysiscache.Digest, error) {
	paths := []string{analyzerExecutable(analyzer.root, language)}
	structural := filepath.Join(analyzer.root, "analyzers", "structural")
	switch language {
	case "java":
		paths = append(paths, filepath.Join(structural, "slopslap-structural-java.jar"), filepath.Join(structural, "java-runtime", "bin", "java"))
	case "rust":
		name := "slopslap-structural-rust"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		paths = append(paths, filepath.Join(structural, name))
	}
	return digestFiles(paths)
}

func digestFiles(paths []string) (analysiscache.Digest, error) {
	type identityFile struct {
		Path   string               `json:"path"`
		Digest analysiscache.Digest `json:"digest"`
	}
	files := make([]identityFile, 0, len(paths))
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		files = append(files, identityFile{Path: filepath.Base(path), Digest: analysiscache.DigestBytes(contents)})
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return analysiscache.DigestBytes(encoded), nil
}

func analyzerExecutable(root, language string) string {
	if language == "typescript" {
		return filepath.Join(root, "build", "typescript", "slopslap-typescript")
	}
	return filepath.Join(root, "analyzers", "structural", "slopslap-structural")
}
