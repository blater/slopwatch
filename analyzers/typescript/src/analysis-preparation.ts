import {
  COMPONENT_BY_ID,
  STRUCTURAL_COMPONENTS,
  TYPED_COMPONENTS,
} from "./catalog.js";
import { AnalysisContext } from "./context.js";
import {
  PROTOCOL_VERSION,
  protocolEnvelope,
  type AnalyzerRequest,
  type Coverage,
  type Diagnostic,
  type Measurement,
  type TypeMode,
} from "./model.js";

export type ResponseRecord = Record<string, unknown>;

export interface AnalysisBuffers {
  measurements: Measurement[];
  coverage: Coverage[];
  failedUnits: Set<string>;
  records: ResponseRecord[];
}

export function responseAnalysisProgress(
  invocationId: string,
  unitId: string,
  stage: string,
  completed: number,
  total: number,
  files: number,
): ResponseRecord {
  return {
    type: "analysis_progress",
    ...protocolEnvelope(invocationId),
    unit_id: unitId,
    language: "typescript",
    stage,
    completed,
    total,
    files,
  };
}

export function responseDiagnostic(
  invocationId: string,
  diagnostic: Diagnostic,
): ResponseRecord {
  return {
    type: "diagnostic",
    ...protocolEnvelope(invocationId),
    severity: diagnostic.severity,
    code: diagnostic.code,
    message: diagnostic.message,
    unit_id: diagnostic.unit_id ?? null,
    path: diagnostic.path ?? null,
    ...(diagnostic.attributes === undefined ? {} : { attributes: diagnostic.attributes }),
  };
}

function inputInvocationId(input: unknown): string {
  if (typeof input !== "object" || input === null) return "unknown";
  const value = Reflect.get(input, "invocation_id");
  return typeof value === "string" ? value : "unknown";
}

function validateUnits(request: Partial<AnalyzerRequest>): void {
  if (!Array.isArray(request.units) || request.units.length === 0)
    throw new Error("units must contain at least one analysis unit");
  const unitIds = new Set<string>();
  for (const unit of request.units) validateUnit(unit, unitIds);
}

function validateUnit(
  unit: NonNullable<AnalyzerRequest["units"]>[number],
  unitIds: Set<string>,
): void {
  validateUnitID(unit.unit_id, unitIds);
  unitIds.add(unit.unit_id);
  if (unit.language !== "typescript")
    throw new Error(`unsupported language ${String(unit.language)}`);
  if (!validSourcePaths(unit.source_paths))
    throw new Error(`unit ${unit.unit_id} source_paths must be a string array`);
}

function validateUnitID(value: unknown, unitIds: ReadonlySet<string>): asserts value is string {
  if (typeof value !== "string" || value.length === 0)
    throw new Error("each unit_id must be a non-empty analysis unit");
  if (unitIds.has(value)) throw new Error(`duplicate unit_id ${value}`);
}

function validSourcePaths(value: unknown): value is string[] {
  return Array.isArray(value) && value.every((item) => typeof item === "string");
}

function validateComponents(request: Partial<AnalyzerRequest>): void {
  if (!Array.isArray(request.components)) throw new Error("components must be an array");
  const requested = new Set<string>();
  for (const component of request.components) validateComponent(component, requested);
}

function validateComponent(
  component: AnalyzerRequest["components"][number],
  requested: Set<string>,
): void {
  if (!validComponentShape(component))
    throw new Error("each component must contain component_id and definition_version");
  const definition = COMPONENT_BY_ID.get(component.component_id);
  if (definition === undefined) throw new Error(`unsupported component ${component.component_id}`);
  if (component.component_id === "module_shallowness" && component.definition_version === "responsibility-burden-v4") {
    if (requested.has(component.component_id)) throw new Error(`duplicate component ${component.component_id}`);
    requested.add(component.component_id);
    return;
  }
  if (definition.definition_version !== component.definition_version)
    throw new Error(`unsupported definition ${component.component_id}/${component.definition_version}; expected ${definition.definition_version}`);
  if (requested.has(component.component_id)) throw new Error(`duplicate component ${component.component_id}`);
  requested.add(component.component_id);
}

function validComponentShape(component: unknown): component is AnalyzerRequest["components"][number] {
  return component !== null && typeof component === "object" &&
    typeof Reflect.get(component, "component_id") === "string" &&
    typeof Reflect.get(component, "definition_version") === "string";
}

function validateOptions(request: Partial<AnalyzerRequest>): void {
  const mode = request.options?.typescript_types;
  if (mode !== undefined && mode !== "auto" && mode !== "require" && mode !== "off")
    throw new Error("options.typescript_types must be auto, require, or off");
  if (request.options?.depth_evaluator_path !== undefined && typeof request.options.depth_evaluator_path !== "string")
    throw new Error("options.depth_evaluator_path must be a string");
}

function validateRequest(value: unknown): AnalyzerRequest {
  if (typeof value !== "object" || value === null) throw new Error("request must be a JSON object");
  const request = value as Partial<AnalyzerRequest>;
  if (request.type !== "request") throw new Error("record type must be request");
  if (request.protocol_version !== PROTOCOL_VERSION)
    throw new Error(`unsupported protocol version ${String(request.protocol_version)}`);
  if (typeof request.invocation_id !== "string" || request.invocation_id.length === 0)
    throw new Error("invocation_id must be a non-empty string");
  if (typeof request.workspace !== "string" || request.workspace.length === 0)
    throw new Error("workspace must be a non-empty string");
  validateUnits(request);
  validateComponents(request);
  validateOptions(request);
  return request as AnalyzerRequest;
}

export interface PreparedAnalysis {
  request: AnalyzerRequest;
  context: AnalysisContext;
  invocationId: string;
  requested: Set<string>;
  requestedStructural: Set<string>;
  requestedTyped: Set<string>;
  requestedDepth: boolean;
  requestedSyntax: Set<string>;
  typeMode: TypeMode;
  buffers: AnalysisBuffers;
}

export function prepareAnalysis(
  input: unknown,
  emitProgress?: (record: ResponseRecord) => void,
): PreparedAnalysis | ResponseRecord[] {
  let invocationId = inputInvocationId(input);
  let request: AnalyzerRequest;
  try {
    request = validateRequest(input);
    invocationId = request.invocation_id;
  } catch (error) {
    return [responseDiagnostic(invocationId, {
      code: "protocol.invalid_request",
      severity: "error",
      message: error instanceof Error ? error.message : String(error),
    }), {
      type: "terminal", ...protocolEnvelope(invocationId), status: "failure",
      message: "invalid analyzer request", analyzed_unit_ids: [], failed_unit_ids: [], skipped_unit_ids: [],
    }];
  }
  const progress = request.options?.stream_results === true ? emitProgress : undefined;
  let context: AnalysisContext;
  try {
    context = AnalysisContext.create(request, (completed, total) => {
      progress?.(responseAnalysisProgress(invocationId, request.units[0]?.unit_id ?? "", "typescript_discovery", completed, total, total));
    });
  } catch (error) {
    return [responseDiagnostic(invocationId, {
      code: "typescript.source_inventory_failed",
      severity: "error",
      message: error instanceof Error ? error.message : String(error),
    }), {
      type: "terminal", ...protocolEnvelope(invocationId), status: "failure",
      message: "source inventory failed", analyzed_unit_ids: [],
      failed_unit_ids: request.units.map((unit) => unit.unit_id), skipped_unit_ids: [],
    }];
  }
  const requested = new Set(request.components.map((item) => item.component_id));
  const requestedStructural = new Set([...requested].filter((item) => STRUCTURAL_COMPONENTS.has(item)));
  const requestedTyped = new Set([...requested].filter((item) => TYPED_COMPONENTS.has(item)));
  const requestedDepth = request.components.some((item) => item.component_id === "module_shallowness" && item.definition_version === "responsibility-burden-v4");
  const requestedSyntax = new Set([...requestedStructural].filter((item) => !requestedDepth || item !== "module_shallowness"));
  const records: ResponseRecord[] = [];
  if (requestedStructural.size > 0) records.push(responseDiagnostic(invocationId, {
    code: "typescript.structural_semantic_exceptions",
    severity: "info",
    message: "TypeScript pmd-v1 exceptions are active: optional chaining, nullish coalescing, async suspension, and generator yield are linear; nested functions are separate cyclomatic/NPath subjects.",
  }));
  const buffers: AnalysisBuffers = { measurements: [], coverage: [], failedUnits: new Set<string>(), records };
  for (const issue of context.inventoryIssues) {
    records.push(responseDiagnostic(invocationId, {
      unit_id: issue.unitId, path: issue.path, code: "typescript.source_inventory_failed",
      severity: "error", message: issue.message,
    }));
    for (const component of requested) buffers.coverage.push({
      unit_id: issue.unitId, path: issue.path, component_id: component,
      definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
      state: "failed", reason: issue.message,
    });
  }
  return { request, context, invocationId, requested, requestedStructural,
    requestedTyped, requestedDepth, requestedSyntax,
    typeMode: request.options?.typescript_types ?? "auto", buffers };
}
