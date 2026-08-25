package native

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

type languageResult struct {
	inputs map[string]scoreInputs
	err    error
}

func runMissingUnits(analyzer *analysisEngine, parent context.Context, workspace string, catalog catalogDocument, misses []plannedCacheUnit, options Options) (map[string]scoreInputs, error) {
	byLanguage, result := missingUnitsByLanguage(catalog, misses)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan languageResult, len(byLanguage))
	var workers sync.WaitGroup
	for language, units := range byLanguage {
		workers.Add(1)
		go func() {
			defer workers.Done()
			inputs, err := runMissingLanguage(analyzer, ctx, workspace, catalog, language, units, options)
			if err != nil {
				cancel()
			}
			results <- languageResult{inputs: inputs, err: err}
		}()
	}
	workers.Wait()
	close(results)
	for item := range results {
		if item.err != nil {
			return nil, item.err
		}
		for id, inputs := range item.inputs {
			result[id] = inputs
		}
	}
	return result, nil
}

func missingUnitsByLanguage(catalog catalogDocument, misses []plannedCacheUnit) (map[string][]plannedCacheUnit, map[string]scoreInputs) {
	byLanguage := make(map[string][]plannedCacheUnit)
	result := make(map[string]scoreInputs, len(misses))
	for _, unit := range misses {
		language := string(unit.plan.Language)
		if len(componentsForLanguage(catalog, language)) == 0 {
			result[unit.plan.ID] = newScoreInputs()
			continue
		}
		byLanguage[language] = append(byLanguage[language], unit)
	}
	return byLanguage, result
}

func runMissingLanguage(analyzer *analysisEngine, ctx context.Context, workspace string, catalog catalogDocument, language string, units []plannedCacheUnit, options Options) (map[string]scoreInputs, error) {
	invocation, err := invocationID()
	if err != nil {
		return nil, err
	}
	combinedID := language + "-unit"
	request := missingLanguageRequest(workspace, catalog, language, units, options, invocation, combinedID)
	runner := analyzer.runUnits
	if runner == nil {
		runner = runAnalyzerUnits
	}
	inputs, err := runner(ctx, analyzerExecutable(analyzer.root, language), request)
	if err != nil {
		return nil, fmt.Errorf("%s analyzer failed: %w", language, err)
	}
	combined, exists := inputs[combinedID]
	if !exists {
		return nil, fmt.Errorf("%s analyzer omitted combined batch %s", language, combinedID)
	}
	partitioned := partitionCombinedUnitInputs(combined, units)
	for _, unit := range units {
		if !hasOwnedCoverage(partitioned[unit.plan.ID], unit.owned) {
			return nil, fmt.Errorf("%s analyzer omitted owned paths for unit %s", language, unit.plan.ID)
		}
	}
	return partitioned, nil
}

func missingLanguageRequest(workspace string, catalog catalogDocument, language string, units []plannedCacheUnit, options Options, invocation, combinedID string) analyzerRequest {
	combinedPaths := make(map[string]bool)
	for _, unit := range units {
		for _, path := range unit.analysisPaths {
			if sourceLanguages[strings.ToLower(filepath.Ext(path))] == language {
				combinedPaths[path] = true
			}
		}
	}
	typeMode := "off"
	if options.TypeScriptTypes {
		typeMode = "auto"
	}
	return analyzerRequest{
		Type: "request", Version: 1, Invocation: invocation, Workspace: workspace,
		Units:      []protocolUnit{{ID: combinedID, Language: language, Paths: mapKeys(combinedPaths), Metadata: map[string]any{"batched_units": len(units)}}},
		Components: componentsForLanguage(catalog, language),
		Options:    map[string]any{"include_tests": options.IncludeTests, "typescript_types": typeMode},
		Limits:     map[string]int{},
	}
}
