import { spawnSync } from "node:child_process";
import ts from "typescript";
import type { AnalysisContext, TypedContext } from "./context.js";
import type { AnalyzerRequest, Coverage, Diagnostic, Measurement, SourceEntry, Subject } from "./model.js";
import type { BoundaryIdentity, DepthBoundary, DepthFacts, EvaluatorResponse } from "./depth-model.js";
import { buildFacts } from "./depth-facts.js";

const MAX_EVALUATOR_BYTES = 64 * 1024 * 1024;
const DEFINITION = "responsibility-burden-v4";

export interface DepthAnalysis {
  measurements: Measurement[];
  coverage: Coverage[];
  diagnostics: Diagnostic[];
}

function subject(entry: SourceEntry): Subject {
  const start = entry.sourceFile.getStart(entry.sourceFile);
  const point = entry.sourceFile.getLineAndCharacterOfPosition(start);
  const end = entry.sourceFile.getLineAndCharacterOfPosition(entry.sourceFile.end);
  return {
    name: entry.relativePath,
    symbol: entry.relativePath,
    start: { line: point.line + 1, column: point.character + 1, offset: start },
    end: { line: end.line + 1, column: end.character + 1, offset: entry.sourceFile.end },
  };
}

export function boundaryIdentityString(boundary: Partial<BoundaryIdentity>): string {
  const fields = [boundary.artifact, boundary.audience, boundary.view, boundary.symbol].map(
    (value) => typeof value === "string" ? value : "",
  );
  return fields.map((value) => `${Buffer.byteLength(value, "utf8")}:${value}`).join("");
}

function invokeEvaluator(executable: string, facts: DepthFacts): EvaluatorResponse {
  const result = spawnSync(executable, ["--evaluate-depth-v4"], {
    input: JSON.stringify({ schema_version: 1, depth: facts }),
    encoding: "utf8",
    maxBuffer: MAX_EVALUATOR_BYTES,
    timeout: 120_000,
    killSignal: "SIGTERM",
  });
  if (result.error !== undefined) throw new Error(`depth evaluator invocation failed: ${result.error.message}`);
  if (result.status !== 0) throw new Error(`depth evaluator exited ${String(result.status)}: ${result.stderr || "unknown error"}`);
  if (typeof result.stdout !== "string" || result.stdout.length > MAX_EVALUATOR_BYTES) throw new Error("depth evaluator output exceeds 64 MiB");
  let response: unknown;
  try { response = JSON.parse(result.stdout); } catch (error) { throw new Error(`depth evaluator returned invalid JSON: ${error instanceof Error ? error.message : String(error)}`); }
  if (response === null || typeof response !== "object" || Reflect.get(response, "schema_version") !== 1 || !Array.isArray(Reflect.get(response, "assessments")) || !Array.isArray(Reflect.get(response, "scores"))) throw new Error("depth evaluator returned an invalid response");
  return response as EvaluatorResponse;
}

export function depthMeasurement(entry: SourceEntry, score: Record<string, unknown>, policyRevision: string): Measurement {
  const boundary = (score.boundary ?? {}) as BoundaryIdentity;
  const state = typeof score.state === "string" ? score.state : "partial";
  const shallow = typeof score.shallow === "number" ? score.shallow : null;
  return {
    unit_id: entry.unitId,
    component_id: "module_shallowness",
    definition_version: DEFINITION,
    path: entry.relativePath,
    scope: "file",
    value: shallow,
    subject: subject(entry),
    attributes: { depth: score, boundary_id: boundaryIdentityString(boundary), knowledge_state: state, policy_revision: policyRevision, inventory_fingerprint: typeof score.inventory_fingerprint === "string" ? score.inventory_fingerprint : undefined },
    provenance: { analyzer: "slopslap-typescript", analyzer_version: "0.1.0", rule: `module_shallowness/${DEFINITION}` },
  };
}

export function analyzeDepth(
  request: AnalyzerRequest,
  context: AnalysisContext,
  typed: TypedContext | undefined,
): DepthAnalysis {
  const output: DepthAnalysis = { measurements: [], coverage: [], diagnostics: [] };
  const policyRevision = typeof request.options?.depth_policy_revision === "string"
    ? request.options.depth_policy_revision : "r20";
  const entries = context.sources.filter((entry) => !entry.isDeclaration);
  if (typed === undefined) {
    for (const entry of entries) {
      output.measurements.push(depthMeasurement(entry, { state: "unavailable", boundary: { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath } }, policyRevision));
      output.coverage.push({ unit_id: entry.unitId, path: entry.relativePath, component_id: "module_shallowness", definition_version: DEFINITION, state: "unavailable", reason: "TypeScript typed context is unavailable" });
    }
    return output;
  }
  const configured = request.options?.depth_evaluator_path;
  if (typeof configured !== "string" || configured.length === 0) {
    output.diagnostics.push({ code: "typescript.depth_evaluator_missing", severity: "error", message: "options.depth_evaluator_path is required for responsibility-burden-v4" });
    for (const entry of entries) {
      output.measurements.push(depthMeasurement(entry, { state: "unavailable", boundary: { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath } }, policyRevision));
      output.coverage.push({ unit_id: entry.unitId, path: entry.relativePath, component_id: "module_shallowness", definition_version: DEFINITION, state: "unavailable", reason: "SHALLOW v4 evaluator helper is not configured" });
    }
    return output;
  }
  const built = buildFacts(context, typed);
  if (built.facts.boundaries.length === 0) return output;
  let response: EvaluatorResponse;
  try {
    response = invokeEvaluator(context.canonicalConfigPath(configured), built.facts);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    output.diagnostics.push({ code: "typescript.depth_evaluator_failed", severity: "error", message: reason });
    for (const entry of built.entries) {
      output.measurements.push(depthMeasurement(entry, { state: "unavailable", boundary: { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath } }, policyRevision));
      output.coverage.push({ unit_id: entry.unitId, path: entry.relativePath, component_id: "module_shallowness", definition_version: DEFINITION, state: "unavailable", reason });
    }
    return output;
  }
  const entriesByArtifact = new Map(built.entries.map((entry) => [entry.relativePath, entry]));
  const scoredArtifacts = new Set<string>();
  for (const score of response.scores) {
    const boundary = (score.boundary ?? {}) as BoundaryIdentity;
    const entry = entriesByArtifact.get(boundary.artifact);
    if (entry === undefined) continue;
    scoredArtifacts.add(entry.relativePath);
    output.measurements.push(depthMeasurement(entry, score, policyRevision));
    const measured = score.state === "measured" && typeof score.shallow === "number";
    const notApplicable = score.state === "not_applicable";
    output.coverage.push({ unit_id: entry.unitId, path: entry.relativePath, component_id: "module_shallowness", definition_version: DEFINITION, state: measured || notApplicable ? "complete" : "unavailable", reason: measured ? "responsibility-burden-v4 evaluator completed" : notApplicable ? "responsibility-burden-v4 evaluator marked boundary not applicable" : "SHALLOW v4 boundary analysis is incomplete" });
  }
  for (const entry of built.entries) {
    if (scoredArtifacts.has(entry.relativePath)) continue;
    output.measurements.push(depthMeasurement(entry, { state: "unavailable", boundary: { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath } }, policyRevision));
    output.coverage.push({ unit_id: entry.unitId, path: entry.relativePath, component_id: "module_shallowness", definition_version: DEFINITION, state: "unavailable", reason: "SHALLOW v4 evaluator returned no boundary score" });
  }
  return output;
}
