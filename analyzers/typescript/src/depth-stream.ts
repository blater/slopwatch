import type { AnalysisContext, TypedContext } from "./context.js";
import { depthMeasurement, type DepthAnalysis } from "./depth.js";
import { buildFacts } from "./depth-facts.js";
import { invokeEvaluatorStreaming } from "./depth-stream-transport.js";
import type { AnalyzerRequest, Coverage, Diagnostic, Measurement, SourceEntry } from "./model.js";

type ProgressEmitter = (record: Record<string, unknown>) => void;
type FileProgressEmitter = (
  entry: SourceEntry,
  measurements: readonly Measurement[],
  coverage: readonly Coverage[],
  complete: boolean,
) => void;

function progressRecord(
  invocationId: string,
  unitId: string,
  stage: string,
  completed: number,
  total: number,
  files: number,
): Record<string, unknown> {
  return {
    type: "analysis_progress",
    protocol_version: 1,
    invocation_id: invocationId,
    unit_id: unitId,
    language: "typescript",
    stage,
    completed,
    total,
    files,
  };
}

export async function analyzeDepthStreaming(
  request: AnalyzerRequest,
  context: AnalysisContext,
  typed: TypedContext | undefined,
  invocationId: string,
  emitProgress?: ProgressEmitter,
  emitFile?: FileProgressEmitter,
): Promise<DepthAnalysis> {
  const output: DepthAnalysis = { measurements: [], coverage: [], diagnostics: [] };
  const policyRevision = typeof request.options?.depth_policy_revision === "string"
    ? request.options.depth_policy_revision : "r20";
  const entries = context.sources.filter((entry) => !entry.isDeclaration);
  const unitId = request.units[0]?.unit_id ?? "";
  const unavailable = (entry: SourceEntry, reason: string): void => {
    const measurement = depthMeasurement(entry, {
      state: "unavailable",
      boundary: { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath },
    }, policyRevision);
    const coverage: Coverage = {
      unit_id: entry.unitId,
      path: entry.relativePath,
      component_id: "module_shallowness",
      definition_version: "responsibility-burden-v4",
      state: "unavailable",
      reason,
    };
    output.measurements.push(measurement);
    output.coverage.push(coverage);
    emitFile?.(entry, [measurement], [coverage], true);
  };
  if (typed === undefined) {
    for (const entry of entries) unavailable(entry, "TypeScript typed context is unavailable");
    return output;
  }
  const configured = request.options?.depth_evaluator_path;
  if (typeof configured !== "string" || configured.length === 0) {
    output.diagnostics.push({ code: "typescript.depth_evaluator_missing", severity: "error", message: "options.depth_evaluator_path is required for responsibility-burden-v4" });
    for (const entry of entries) unavailable(entry, "SHALLOW v4 evaluator helper is not configured");
    return output;
  }
  const built = buildFacts(context, typed, (completed, total) => {
    emitProgress?.(progressRecord(invocationId, unitId, "typescript_depth_facts", completed, total, entries.length));
  });
  if (built.facts.boundaries.length === 0) return output;
  const entriesByArtifact = new Map(built.entries.map((entry) => [entry.relativePath, entry]));
  const expected = new Map<string, number>();
  for (const boundary of built.facts.boundaries)
    expected.set(boundary.identity.artifact, (expected.get(boundary.identity.artifact) ?? 0) + 1);
  const scored = new Map<string, number>();
  const measurementsByPath = new Map<string, Measurement[]>();
  const coverageByPath = new Map<string, Coverage[]>();
  let completed = 0;
  try {
    await invokeEvaluatorStreaming(
      context.canonicalConfigPath(configured),
      built.facts,
      (_assessment, score) => {
        const boundary = (score.boundary ?? {}) as { artifact?: string };
        const entry = entriesByArtifact.get(boundary.artifact ?? "");
        if (entry === undefined) return;
        const measurement = depthMeasurement(entry, score, policyRevision);
        const count = (scored.get(entry.relativePath) ?? 0) + 1;
        scored.set(entry.relativePath, count);
        const complete = count >= (expected.get(entry.relativePath) ?? 1);
        const measured = score.state === "measured" && typeof score.shallow === "number";
        const notApplicable = score.state === "not_applicable";
        const coverage: Coverage = {
          unit_id: entry.unitId,
          path: entry.relativePath,
          component_id: "module_shallowness",
          definition_version: "responsibility-burden-v4",
          state: measured || notApplicable ? "complete" : "unavailable",
          reason: measured
            ? "responsibility-burden-v4 evaluator completed"
            : notApplicable
              ? "responsibility-burden-v4 evaluator marked boundary not applicable"
              : "SHALLOW v4 boundary analysis is incomplete",
        };
        output.measurements.push(measurement);
        output.coverage.push(coverage);
        const measurements = measurementsByPath.get(entry.relativePath) ?? [];
        measurements.push(measurement);
        measurementsByPath.set(entry.relativePath, measurements);
        const coverages = coverageByPath.get(entry.relativePath) ?? [];
        coverages.push(coverage);
        coverageByPath.set(entry.relativePath, coverages);
        if (complete) emitFile?.(entry, measurements, coverages, true);
        completed++;
        emitProgress?.(progressRecord(invocationId, entry.unitId, "typescript_depth", completed, built.facts.boundaries.length, entries.length));
      },
    );
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    output.diagnostics.push({ code: "typescript.depth_evaluator_failed", severity: "error", message: reason });
    output.measurements.length = 0;
    output.coverage.length = 0;
    for (const entry of built.entries) {
      unavailable(entry, reason);
    }
    return output;
  }
  for (const entry of built.entries) {
    if (!scored.has(entry.relativePath)) unavailable(entry, "SHALLOW v4 evaluator returned no boundary score");
  }
  return output;
}
