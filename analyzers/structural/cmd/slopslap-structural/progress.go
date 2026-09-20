package main

import (
	"sort"

	"slopslap.dev/structural/internal/adapters"
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
)

// Local metrics use already linked facts. Compute them once per file and reuse
// the measurements in the final report.
func analyzeFileProgress(out emitter, item unit, program *facts.Program, components []component, registry *metrics.Registry) ([]metrics.Measurement, error) {
	local := map[string]bool{}
	var localComponents []component
	for _, component := range components {
		if component.ID == "module_shallowness" {
			continue
		} else {
			local[component.ID] = true
			localComponents = append(localComponents, component)
		}
	}
	files := indexProgressFiles(program)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var result []metrics.Measurement
	for _, path := range paths {
		file := files[path]
		measurements, err := registry.Analyze(file, local)
		if err != nil {
			return nil, err
		}
		result = append(result, measurements...)
		emitFileProgress(out, item, file, measurements, localComponents)
	}
	return result, nil
}

func indexProgressFiles(program *facts.Program) map[string]*facts.Program {
	files := map[string]*facts.Program{}
	ensure := func(path string) *facts.Program {
		if files[path] == nil {
			files[path] = &facts.Program{Files: []string{path}, Unavailable: program.Unavailable}
		}
		return files[path]
	}
	for _, path := range program.Files {
		ensure(path)
	}
	for _, function := range program.Functions {
		file := ensure(function.Location.Path)
		file.Functions = append(file.Functions, function)
	}
	for _, item := range program.Types {
		file := ensure(item.Location.Path)
		file.Types = append(file.Types, item)
	}
	for _, failure := range program.Failures {
		file := ensure(failure.Path)
		file.Failures = append(file.Failures, failure)
	}
	return files
}

func emitFileProgress(out emitter, item unit, file *facts.Program, measurements []metrics.Measurement, components []component) {
	path := file.Files[0]
	records := make([]map[string]any, 0, len(measurements)+len(components))
	for _, measurement := range measurements {
		records = append(records, measurementRecord(item, measurement))
	}
	for _, component := range components {
		state, reason := "complete", ""
		if len(file.Failures) > 0 {
			state, reason = "failed", file.Failures[0].Diagnostic
		} else if available, detail := file.Availability(path, component.ID); !available {
			state, reason = "unavailable", detail
		}
		records = append(records, map[string]any{
			"type": "coverage", "unit_id": item.ID, "component_id": component.ID,
			"definition_version": component.Version, "path": path, "state": state, "reason": reason,
		})
	}
	for _, record := range records {
		out.emit(record["type"].(string), record)
	}
}

// Adapters expose linked syntax and stable semantic groups as they complete.
// Shared flow contexts stay intact; each scored file is delivered immediately.
func analyzeProgressUnit(adapter adapters.Adapter, input request, item unit, out emitter, registry *metrics.Registry, options map[string]any) (*facts.Program, []metrics.Measurement, error) {
	var localErr error
	var local []metrics.Measurement
	var streamedDepth []metrics.Measurement
	depthDelivered := false
	options["analysis_progress"] = func(stage string, completed, total int) {
		out.emit("analysis_progress", map[string]any{"unit_id": item.ID, "language": item.Language, "stage": stage, "completed": completed, "total": total, "files": len(item.Paths)})
	}
	options["depth_progress"] = func(chunk *facts.DepthFacts, paths []string) {
		depthDelivered = true
		part := &facts.Program{Depth: chunk, Files: paths}
		measured := metrics.DepthV4MeasurementsProgress(part, func(path string, items []metrics.Measurement) {
			file := &facts.Program{Files: []string{path}, Unavailable: part.Unavailable}
			emitFileProgress(out, item, file, items, []component{{ID: "module_shallowness", Version: metrics.DepthV4Definition}})
		})
		streamedDepth = append(streamedDepth, measured...)
	}
	options["analysis_progress"].(func(string, int, int))("parsing", 0, 0)
	delivered := false
	emit := func(program *facts.Program) {
		delivered = true
		local, localErr = analyzeFileProgress(out, item, program, input.Components, registry)
	}
	var program *facts.Program
	var err error
	if streaming, ok := adapter.(interface {
		AnalyzeProgress(string, []string, map[string]any, func(*facts.Program)) (*facts.Program, error)
	}); ok {
		program, err = streaming.AnalyzeProgress(input.Workspace, item.Paths, options, emit)
	} else {
		program, err = adapter.Analyze(input.Workspace, item.Paths, options)
	}
	if err != nil {
		return program, nil, err
	}
	if !delivered {
		emit(program)
	}
	if localErr != nil {
		return program, nil, localErr
	}
	requested := map[string]bool{}
	for _, component := range input.Components {
		if component.ID == "module_shallowness" {
			requested[component.ID] = true
		}
	}
	depth := streamedDepth
	if !depthDelivered || program.Depth == nil {
		depth, err = registry.Analyze(program, requested)
		for _, measurement := range depth {
			emitMeasurement(out, item, measurement)
		}
	}
	// Preserve final coverage for streamed partial assessments.
	for _, measurement := range depth {
		if score, ok := measurement.Attributes["depth"].(metrics.DepthScore); ok && (score.State == facts.KnowledgePartial || score.State == facts.KnowledgeUnavailable) {
			if program.Unavailable == nil {
				program.Unavailable = map[string]map[string]string{}
			}
			if program.Unavailable[measurement.Location.Path] == nil {
				program.Unavailable[measurement.Location.Path] = map[string]string{}
			}
			program.Unavailable[measurement.Location.Path]["module_shallowness"] = "SHALLOW v4 boundary analysis is incomplete"
		}
	}
	result := append(local, depth...)
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.Location.Path != b.Location.Path {
			return a.Location.Path < b.Location.Path
		}
		if a.Location.Line != b.Location.Line {
			return a.Location.Line < b.Location.Line
		}
		if a.Location.Column != b.Location.Column {
			return a.Location.Column < b.Location.Column
		}
		return a.Component < b.Component
	})
	return program, result, err
}
