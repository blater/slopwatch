package sourceestimate

import "path/filepath"

// AttributionProgress reports observed source-estimate work. Counts describe
// completed units in the named stage; they are deliberately independent of
// score or evidence quality.
type AttributionProgress struct {
	Stage     string
	Language  string
	Completed int
	Total     int
}

type attributionProgressFunc func(AttributionProgress)

func analyzeWithAttributionProgress(files []File, includeGoPackage bool, callback func(File, Result), progress attributionProgressFunc, profiles ...CalibrationProfile) (map[string]Result, map[string]Result) {
	profile := DefaultCalibration()
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	units := prepareAttributionUnitsProgress(files, profile, progress)
	prepareStage := func(stage string) {
		if progress != nil {
			progress(AttributionProgress{Stage: stage, Completed: 0, Total: 1})
		}
	}
	finishStage := func(stage string) {
		if progress != nil {
			progress(AttributionProgress{Stage: stage, Completed: 1, Total: 1})
		}
	}
	prepareStage("source_index")
	rustAnnotations := annotateRustAttribution(units)
	finishStage("source_index")
	prepareStage("source_typescript_imports")
	annotateTypeScriptImports(units)
	finishStage("source_typescript_imports")
	prepareStage("source_operation_index")
	byKey := attributionOperationIndex(units)
	finishStage("source_operation_index")
	prepareStage("source_go_inputs")
	annotateGoUnusedInputs(units)
	finishStage("source_go_inputs")
	prepareStage("source_surface_inputs")
	annotateSourceSurfaceInputs(units, byKey)
	finishStage("source_surface_inputs")
	prepareStage("source_caller_obligations")
	annotateCallerObligations(units)
	finishStage("source_caller_obligations")
	prepareStage("source_constraint_witnesses")
	annotateConstraintWitnesses(units)
	finishStage("source_constraint_witnesses")
	prepareStage("source_output_obligations")
	annotateOutputObligations(units, byKey)
	finishStage("source_output_obligations")
	prepareStage("source_go_methods")
	goMethods := indexGoMethods(units)
	finishStage("source_go_methods")
	prepareStage("source_call_graph")
	goGraph := goCallGraph(units, byKey)
	finishStage("source_call_graph")
	packageResults := make(map[string]Result, len(files))
	for index, unit := range units {
		if includeGoPackage {
			packageResults[unit.file.Path] = estimateUnit(index, unit, units, byKey, goMethods.supporting)
		}
	}
	if includeGoPackage {
		if progress != nil {
			progress(AttributionProgress{Stage: "source_evaluate_go", Language: "go", Completed: len(packageResults), Total: len(packageResults)})
			progress(AttributionProgress{Stage: "source_complete", Completed: 1, Total: 1})
		}
		return packageResults, nil
	}
	fileResults := estimateGoAttribution(units, byKey, goMethods, goGraph, progressCallback(units, "go", callback, progress))
	mergeAttributionResults(fileResults, analyzeTypeScriptUnits(units, byKey, progressCallback(units, "typescript", callback, progress)))
	mergeAttributionResults(fileResults, analyzeJavaUnits(units, byKey, progressCallback(units, "java", callback, progress)))
	rustResults := analyzeRustUnits(units, byKey, rustAnnotations, progressCallback(units, "rust", callback, progress))
	mergeAttributionResults(fileResults, rustResults.Results)
	if progress != nil {
		progress(AttributionProgress{Stage: "source_complete", Completed: 1, Total: 1})
	}
	return packageResults, fileResults
}

func prepareAttributionUnits(files []File, profile CalibrationProfile) []unit {
	return prepareAttributionUnitsProgress(files, profile, nil)
}

func prepareAttributionUnitsProgress(files []File, profile CalibrationProfile, progress attributionProgressFunc) []unit {
	units := make([]unit, len(files))
	for i, file := range files {
		tokens, limited, lexicallyValid := lex(file.Source)
		pkg := packageName(file.Language, tokens)
		if normalizeLanguage(file.Language, file.Path) == "go" {
			pkg = filepath.ToSlash(filepath.Dir(file.Path)) + "@" + pkg
		}
		units[i] = unit{inventory: &unitInventory{}, calibration: profile, index: i, file: file, tokens: tokens, pkg: pkg, limited: limited, lexicallyValid: lexicallyValid}
		units[i].ops = findOperations(file, i, tokens, units[i].pkg)
		if !hasExternalOperation(units[i].ops) && isJavaOrRust(file) {
			for _, op := range units[i].ops {
				op.exposed = op.packageVisible
			}
		}
		if len(units[i].ops) >= maxOperationsPerFile {
			units[i].limited = true
		}
		if progress != nil {
			progress(AttributionProgress{Stage: "source_lex", Language: normalizeLanguage(file.Language, file.Path), Completed: i + 1, Total: len(files)})
		}
	}
	return units
}

func progressCallback(units []unit, language string, callback func(File, Result), progress attributionProgressFunc) func(File, Result) {
	total := 0
	for _, unit := range units {
		if normalizeLanguage(unit.file.Language, unit.file.Path) == language {
			total++
		}
	}
	completed := 0
	return func(file File, result Result) {
		completed++
		if progress != nil {
			progress(AttributionProgress{Stage: "source_evaluate_" + language, Language: language, Completed: completed, Total: total})
		}
		if callback != nil {
			callback(file, result)
		}
	}
}

func hasExternalOperation(operations []*operation) bool {
	for _, op := range operations {
		if op.exposed {
			return true
		}
	}
	return false
}

func isJavaOrRust(file File) bool {
	language := normalizeLanguage(file.Language, file.Path)
	return language == "java" || language == "rust"
}

func attributionOperationIndex(units []unit) map[string][]*operation {
	all := make([]*operation, 0)
	for _, unit := range units {
		all = append(all, unit.ops...)
	}
	byKey := make(map[string][]*operation, len(all))
	for _, op := range all {
		indexOperation(byKey, op)
	}
	return byKey
}

func mergeAttributionResults(dst, src map[string]Result) {
	for path, result := range src {
		dst[path] = result
	}
}
