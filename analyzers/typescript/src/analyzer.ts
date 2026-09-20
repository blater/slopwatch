import ts from "typescript";

import {
  COMPONENT_BY_ID,
} from "./catalog.js";
import { AnalysisContext } from "./context.js";
import type { TypedContext } from "./context.js";
import { analyzeDepth } from "./depth.js";
import {
  prepareAnalysis,
  responseAnalysisProgress,
  responseDiagnostic,
  type AnalysisBuffers,
  type ResponseRecord,
} from "./analysis-preparation.js";
export type { ResponseRecord } from "./analysis-preparation.js";
import { analyzeStructural } from "./structural.js";
import { analyzeTypeSafety } from "./type-safety.js";
import {
  protocolEnvelope,
  type AnalyzerRequest,
  type Coverage,
  type Diagnostic,
  type Measurement,
  type TypeMode,
} from "./model.js";

function responseCoverage(
  invocationId: string,
  coverage: Coverage,
): ResponseRecord {
  return {
    type: "coverage",
    ...protocolEnvelope(invocationId),
    unit_id: coverage.unit_id,
    component_id: coverage.component_id,
    definition_version: coverage.definition_version,
    path: coverage.path,
    state: coverage.state,
    reason: coverage.reason,
  };
}

function responseMeasurement(
  invocationId: string,
  measurement: Measurement,
): ResponseRecord {
  return {
    type: "measurement",
    ...protocolEnvelope(invocationId),
    ...measurement,
  };
}


function syntaxDiagnostic(entry: {
  unitId: string;
  relativePath: string;
  sourceFile: ts.SourceFile;
  syntaxErrors: readonly ts.Diagnostic[];
}): Diagnostic[] {
  return entry.syntaxErrors.map((item) => ({
    unit_id: entry.unitId,
    path: entry.relativePath,
    code: `typescript.syntax.${item.code}`,
    severity: "error" as const,
    message: (() => {
      const position = item.start ?? 0;
      const line = entry.sourceFile.getLineAndCharacterOfPosition(position);
      return `${entry.relativePath}:${line.line + 1}:${line.character + 1}: ${ts.flattenDiagnosticMessageText(item.messageText, "\n")}`;
    })(),
  }));
}

interface TypedAnalysisResult {
	unavailableReason: string | undefined;
	context?: TypedContext;
}

export function analyzeSyntaxSources(
  context: AnalysisContext,
  invocationId: string,
  requestedStructural: ReadonlySet<string>,
  buffers: AnalysisBuffers,
  emitProgress?: (record: ResponseRecord) => void,
): void {
  const entries = context.sources.filter((entry) => !entry.isDeclaration);
  let completed = 0;
  for (const entry of entries) {
    if (entry.isDeclaration) continue;
    const measurements: Measurement[] = [];
    const coverage: Coverage[] = [];
    if (entry.syntaxErrors.length > 0) {
      for (const item of syntaxDiagnostic(entry))
        buffers.records.push(responseDiagnostic(invocationId, item));
      for (const component of requestedStructural) {
        coverage.push({
          unit_id: entry.unitId,
          path: entry.relativePath,
          component_id: component,
          definition_version:
            COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
          state: "failed",
          reason: "the TypeScript syntax parser reported errors",
        });
      }
      buffers.coverage.push(...coverage);
      emitFileProgress(emitProgress, invocationId, entry, measurements, coverage);
      completed++;
      emitProgress?.(responseAnalysisProgress(invocationId, entry.unitId, "typescript_syntax", completed, entries.length, entries.length));
      continue;
    }
    measurements.push(...analyzeStructural(entry, requestedStructural));
    for (const component of requestedStructural) {
      coverage.push({
        unit_id: entry.unitId,
        path: entry.relativePath,
        component_id: component,
        definition_version:
          COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
        state: "complete",
        reason: "syntax kernel completed",
      });
    }
    buffers.measurements.push(...measurements);
    buffers.coverage.push(...coverage);
    emitFileProgress(emitProgress, invocationId, entry, measurements, coverage);
    completed++;
    emitProgress?.(responseAnalysisProgress(invocationId, entry.unitId, "typescript_syntax", completed, entries.length, entries.length));
  }
}

export function emitFileProgress(
  emit: ((record: ResponseRecord) => void) | undefined,
  invocationId: string,
  entry: AnalysisContext["sources"][number],
  measurements: readonly Measurement[],
  coverage: readonly Coverage[],
  complete?: boolean,
): void {
  if (emit === undefined || (measurements.length === 0 && coverage.length === 0))
    return;
  for (const item of measurements) emit(responseMeasurement(invocationId, item));
  for (const item of coverage) emit(responseCoverage(invocationId, item));
}

export function analyzeTypedSources(
  request: AnalyzerRequest,
  context: AnalysisContext,
  invocationId: string,
  requestedTyped: ReadonlySet<string>,
  typeMode: TypeMode,
  buffers: AnalysisBuffers,
  emitProgress?: (record: ResponseRecord) => void,
  forceContextWhenOff = false,
): TypedAnalysisResult {
  const entries = context.sources.filter((entry) => !entry.isDeclaration);
  emitProgress?.(responseAnalysisProgress(invocationId, request.units[0]?.unit_id ?? "", "typescript_typed_context", 0, 0, entries.length));
  if (context.inventoryIssues.length > 0) {
    const reason =
      "typed analysis is unavailable because the requested source inventory is incomplete";
    buffers.records.push(
      responseDiagnostic(invocationId, {
        code: "typescript.typed_inventory_incomplete",
        severity: "error",
        message: reason,
        attributes: {
          rejected_paths: context.inventoryIssues.map((item) => item.path),
        },
      }),
    );
    markTypedAnalysisUnavailable(
      request,
      context,
      invocationId,
      requestedTyped,
      typeMode,
      buffers,
      reason,
      emitProgress,
    );
    return { unavailableReason: reason };
  }
  const typedResult = context.createTypedContext(
    request,
    forceContextWhenOff && typeMode === "off" ? "auto" : typeMode,
  );
  for (const item of typedResult.diagnostics)
    buffers.records.push(responseDiagnostic(invocationId, item));
  const unavailableReason = typedResult.unavailableReason;
  if (typedResult.context !== undefined) {
    if (typeMode !== "off") {
      analyzeAvailableTypedSources(
        context,
        typedResult.context,
        invocationId,
        requestedTyped,
        buffers,
        emitProgress,
      );
    }
    if (typeMode === "off" && requestedTyped.size > 0) {
      markTypedAnalysisUnavailable(
        request,
        context,
        invocationId,
        requestedTyped,
        typeMode,
        buffers,
        "typescript_types is off",
        emitProgress,
      );
    }
    return { unavailableReason, context: typedResult.context };
  }
  markTypedAnalysisUnavailable(request, context, invocationId, requestedTyped, typeMode, buffers, unavailableReason, emitProgress);
  return { unavailableReason };
}

function analyzeAvailableTypedSources(
	context: AnalysisContext,
	typedContext: NonNullable<ReturnType<AnalysisContext["createTypedContext"]>["context"]>,
	invocationId: string,
  requestedTyped: ReadonlySet<string>,
  buffers: AnalysisBuffers,
  emitProgress?: (record: ResponseRecord) => void,
): void {
	const entries = context.sources.filter((entry) => !entry.isDeclaration);
	let completed = 0;
	for (const entry of entries) {
		if (entry.isDeclaration) continue;
		if (entry.syntaxErrors.length > 0) {
			addTypedCoverage(entry, requestedTyped, buffers, "failed", "syntax errors prevent trustworthy typed analysis");
		emitFileProgress(emitProgress, invocationId, entry, [], requestedTyped.size === 0 ? [] : buffers.coverage.slice(-requestedTyped.size), true);
			completed++;
			emitProgress?.(responseAnalysisProgress(invocationId, entry.unitId, "typescript_typed", completed, entries.length, entries.length));
			continue;
		}
		const measurements = analyzeTypeSafety(entry, typedContext, requestedTyped);
		const coverage = requestedTyped.size;
		buffers.measurements.push(...measurements);
		addTypedCoverage(entry, requestedTyped, buffers, "complete", "compiler-aware kernel completed with a trustworthy type graph");
		emitFileProgress(emitProgress, invocationId, entry, measurements, coverage === 0 ? [] : buffers.coverage.slice(-coverage), true);
		completed++;
		emitProgress?.(responseAnalysisProgress(invocationId, entry.unitId, "typescript_typed", completed, entries.length, entries.length));
	}
}

function addTypedCoverage(
	entry: AnalysisContext["sources"][number],
	requestedTyped: ReadonlySet<string>,
	buffers: AnalysisBuffers,
	state: Coverage["state"],
	reason: string,
): void {
	for (const component of requestedTyped) {
		buffers.coverage.push({
			unit_id: entry.unitId,
			path: entry.relativePath,
			component_id: component,
			definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
			state,
			reason,
		});
	}
}

function markTypedAnalysisUnavailable(
	request: AnalyzerRequest,
	context: AnalysisContext,
	invocationId: string,
	requestedTyped: ReadonlySet<string>,
	typeMode: TypeMode,
  buffers: AnalysisBuffers,
  unavailableReason: string | undefined,
  emitProgress?: (record: ResponseRecord) => void,
): void {
  const reason = unavailableReason ?? "typed analysis is unavailable";
  const hasSyntaxUsableSource = context.sources.some(
    (entry) => !entry.isDeclaration && entry.syntaxErrors.length === 0,
  );
  if (
    (typeMode === "require" && hasSyntaxUsableSource) ||
    (typeMode === "off" && requestedTyped.size > 0)
  ) {
    for (const unit of request.units) buffers.failedUnits.add(unit.unit_id);
  }
  if (typeMode === "off" && requestedTyped.size > 0) {
    buffers.records.push(
      responseDiagnostic(invocationId, {
        code: "typescript.typed_components_requested_while_off",
        severity: "error",
        message:
          "Typed components were requested while options.typescript_types is off.",
      }),
    );
  }
  for (const entry of context.sources) {
	if (entry.isDeclaration) continue;
	if (entry.syntaxErrors.length > 0) {
	  addTypedCoverage(entry, requestedTyped, buffers, "failed", "syntax errors prevent trustworthy typed analysis");
	} else {
	  addTypedCoverage(entry, requestedTyped, buffers, "unavailable", reason);
	}
	emitFileProgress(emitProgress, invocationId, entry, [], requestedTyped.size === 0 ? [] : buffers.coverage.slice(-requestedTyped.size), true);
  }
}

export function finishAnalysis(
  request: AnalyzerRequest,
  context: AnalysisContext,
  invocationId: string,
  requested: ReadonlySet<string>,
  requestedTyped: ReadonlySet<string>,
  typeMode: TypeMode,
  typedUnavailableReason: string | undefined,
  requestedDepth: boolean,
  buffers: AnalysisBuffers,
): ResponseRecord[] {
  // Sources and each analyzer's findings are already produced deterministically.
  // Preserve that order instead of globally sorting repository-sized output.
  for (const item of buffers.measurements)
    buffers.records.push(responseMeasurement(invocationId, item));
  for (const item of buffers.coverage)
    buffers.records.push(responseCoverage(invocationId, item));
  const modes = ["typescript-syntax"];
  if (requestedDepth || (requestedTyped.size > 0 && typeMode !== "off"))
    modes.push("typescript-compiler");
  for (const unit of request.units) {
    const unitSources = context.sources.filter(
      (item) => item.unitId === unit.unit_id && !item.isDeclaration,
    );
    buffers.records.push({
      type: "execution_plan",
      ...protocolEnvelope(invocationId),
      unit_id: unit.unit_id,
      parser_modes: modes,
      kernels: [...requested].sort(),
      discovered_source_count: unitSources.length,
      parsed_source_count: unitSources.reduce(
        (sum, item) =>
          sum +
          (context.syntaxParseCounts.get(item.absolutePath) ?? 0) +
          (context.typedParseCounts.get(item.absolutePath) ?? 0),
        0,
      ),
    });
  }
  const allUnitIds = request.units.map((unit) => unit.unit_id);
  const failedUnitIds = allUnitIds.filter((item) =>
    buffers.failedUnits.has(item),
  );
  buffers.records.push({
    type: "terminal",
    ...protocolEnvelope(invocationId),
    status: failedUnitIds.length > 0 ? "failure" : "success",
    message:
      failedUnitIds.length > 0
        ? "one or more TypeScript analysis units failed"
        : typedUnavailableReason === undefined
          ? "analysis completed"
          : "structural analysis completed with typed coverage unavailable",
    analyzed_unit_ids: allUnitIds.filter(
      (item) => !buffers.failedUnits.has(item),
    ),
    failed_unit_ids: failedUnitIds,
    skipped_unit_ids: [],
  });
  return buffers.records;
}

export function analyze(
  input: unknown,
  emitProgress?: (record: ResponseRecord) => void,
): ResponseRecord[] {
  const prepared = prepareAnalysis(input, emitProgress);
  if (Array.isArray(prepared)) return prepared;
  const { request, context, invocationId, requested, requestedStructural,
    requestedTyped, requestedDepth, requestedSyntax, typeMode, buffers } = prepared;
  const progress = request.options?.stream_results === true ? emitProgress : undefined;
  analyzeSyntaxSources(context, invocationId, requestedSyntax, buffers, progress);
  let typedUnavailableReason: string | undefined;
  let typedContext: TypedContext | undefined;
  if (requestedTyped.size > 0 || requestedDepth) {
    const typedResult = analyzeTypedSources(
      request,
      context,
      invocationId,
      requestedTyped,
      typeMode,
      buffers,
      progress,
      requestedDepth,
    );
    typedUnavailableReason = typedResult.unavailableReason;
    typedContext = typedResult.context;
  }
  if (requestedDepth) {
    const depth = analyzeDepth(request, context, typedContext);
    for (const item of depth.diagnostics)
      buffers.records.push(responseDiagnostic(invocationId, item));
    buffers.measurements.push(...depth.measurements);
    buffers.coverage.push(...depth.coverage);
  }

  return finishAnalysis(
    request,
    context,
    invocationId,
    requested,
    requestedTyped,
    typeMode,
    typedUnavailableReason,
    requestedDepth,
    buffers,
  );
}
