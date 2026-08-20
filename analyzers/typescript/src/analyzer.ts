import ts from "typescript";

import { COMPONENT_BY_ID, STRUCTURAL_COMPONENTS, TYPED_COMPONENTS } from "./catalog.js";
import { AnalysisContext } from "./context.js";
import { analyzeStructural } from "./structural.js";
import { analyzeTypeSafety } from "./type-safety.js";
import {
  PROTOCOL_VERSION,
  protocolEnvelope,
  type AnalyzerRequest,
  type Coverage,
  type Diagnostic,
  type Measurement,
  type TypeMode
} from "./model.js";

export type ResponseRecord = Record<string, unknown>;

function responseDiagnostic(invocationId: string, diagnostic: Diagnostic): ResponseRecord {
  return {
    type: "diagnostic",
    ...protocolEnvelope(invocationId),
    severity: diagnostic.severity,
    code: diagnostic.code,
    message: diagnostic.message,
    unit_id: diagnostic.unit_id ?? null,
    path: diagnostic.path ?? null
  };
}

function responseCoverage(invocationId: string, coverage: Coverage): ResponseRecord {
  return {
    type: "coverage",
    ...protocolEnvelope(invocationId),
    unit_id: coverage.unit_id,
    component_id: coverage.component_id,
    definition_version: coverage.definition_version,
    path: coverage.path,
    state: coverage.state,
    reason: coverage.reason
  };
}

function responseMeasurement(invocationId: string, measurement: Measurement): ResponseRecord {
  return { type: "measurement", ...protocolEnvelope(invocationId), ...measurement };
}

function validateRequest(value: unknown): AnalyzerRequest {
  if (typeof value !== "object" || value === null) throw new Error("request must be a JSON object");
  const request = value as Partial<AnalyzerRequest>;
  if (request.type !== "request") throw new Error("record type must be request");
  if (request.protocol_version !== PROTOCOL_VERSION) {
    throw new Error(`unsupported protocol version ${String(request.protocol_version)}`);
  }
  if (typeof request.invocation_id !== "string" || request.invocation_id.length === 0) {
    throw new Error("invocation_id must be a non-empty string");
  }
  if (typeof request.workspace !== "string" || request.workspace.length === 0) {
    throw new Error("workspace must be a non-empty string");
  }
  if (!Array.isArray(request.units) || request.units.length === 0) {
    throw new Error("units must contain at least one analysis unit");
  }
  const unitIds = new Set<string>();
  for (const unit of request.units) {
    if (typeof unit.unit_id !== "string" || unit.unit_id.length === 0) {
      throw new Error("each unit_id must be a non-empty string");
    }
    if (unitIds.has(unit.unit_id)) throw new Error(`duplicate unit_id ${unit.unit_id}`);
    unitIds.add(unit.unit_id);
    if (unit.language !== "typescript") throw new Error(`unsupported language ${String(unit.language)}`);
    if (!Array.isArray(unit.source_paths) || !unit.source_paths.every((item) => typeof item === "string")) {
      throw new Error(`unit ${unit.unit_id} source_paths must be a string array`);
    }
  }
  if (!Array.isArray(request.components)) throw new Error("components must be an array");
  const requested = new Set<string>();
  for (const component of request.components) {
    if (
      component === null ||
      typeof component !== "object" ||
      typeof component.component_id !== "string" ||
      typeof component.definition_version !== "string"
    ) {
      throw new Error("each component must contain component_id and definition_version");
    }
    const definition = COMPONENT_BY_ID.get(component.component_id);
    if (definition === undefined) throw new Error(`unsupported component ${component.component_id}`);
    if (definition.definition_version !== component.definition_version) {
      throw new Error(
        `unsupported definition ${component.component_id}/${component.definition_version}; expected ${definition.definition_version}`
      );
    }
    if (requested.has(component.component_id)) throw new Error(`duplicate component ${component.component_id}`);
    requested.add(component.component_id);
  }
  const mode = request.options?.typescript_types;
  if (mode !== undefined && mode !== "auto" && mode !== "require" && mode !== "off") {
    throw new Error("options.typescript_types must be auto, require, or off");
  }
  return request as AnalyzerRequest;
}

function syntaxDiagnostic(entry: {
  unitId: string;
  relativePath: string;
  syntaxErrors: readonly ts.Diagnostic[];
}): Diagnostic[] {
  return entry.syntaxErrors.map((item) => ({
    unit_id: entry.unitId,
    path: entry.relativePath,
    code: `typescript.syntax.${item.code}`,
    severity: "error" as const,
    message: ts.flattenDiagnosticMessageText(item.messageText, "\n")
  }));
}

export function analyze(input: unknown): ResponseRecord[] {
  let invocationId =
    typeof input === "object" && input !== null && "invocation_id" in input &&
    typeof (input as { invocation_id?: unknown }).invocation_id === "string"
      ? (input as { invocation_id: string }).invocation_id
      : "unknown";
  let request: AnalyzerRequest;
  try {
    request = validateRequest(input);
    invocationId = request.invocation_id;
  } catch (error) {
    return [
      responseDiagnostic(invocationId, {
        code: "protocol.invalid_request",
        severity: "error",
        message: error instanceof Error ? error.message : String(error)
      }),
      {
        type: "terminal",
        ...protocolEnvelope(invocationId),
        status: "failure",
        message: "invalid analyzer request",
        analyzed_unit_ids: [],
        failed_unit_ids: [],
        skipped_unit_ids: []
      }
    ];
  }

  const records: ResponseRecord[] = [];
  let context: AnalysisContext;
  try {
    context = AnalysisContext.create(request);
  } catch (error) {
    records.push(
      responseDiagnostic(invocationId, {
        code: "typescript.source_inventory_failed",
        severity: "error",
        message: error instanceof Error ? error.message : String(error)
      })
    );
    records.push({
      type: "terminal",
      ...protocolEnvelope(invocationId),
      status: "failure",
      message: "source inventory failed",
      analyzed_unit_ids: [],
      failed_unit_ids: request.units.map((unit) => unit.unit_id),
      skipped_unit_ids: []
    });
    return records;
  }

  const requested = new Set(request.components.map((item) => item.component_id));
  const requestedStructural = new Set([...requested].filter((item) => STRUCTURAL_COMPONENTS.has(item)));
  const requestedTyped = new Set([...requested].filter((item) => TYPED_COMPONENTS.has(item)));
  const measurements: Measurement[] = [];
  const coverage: Coverage[] = [];
  const failedUnits = new Set<string>();

  if (requestedStructural.size > 0) {
    records.push(
      responseDiagnostic(invocationId, {
        code: "typescript.structural_semantic_exceptions",
        severity: "info",
        message:
          "TypeScript pmd-v1 exceptions are active: optional chaining, nullish coalescing, async suspension, and generator yield are linear; nested functions are separate cyclomatic/NPath subjects."
      })
    );
  }

  for (const entry of context.sources) {
    if (entry.syntaxErrors.length > 0) {
      failedUnits.add(entry.unitId);
      for (const item of syntaxDiagnostic(entry)) records.push(responseDiagnostic(invocationId, item));
      for (const component of requestedStructural) {
        coverage.push({
          unit_id: entry.unitId,
          path: entry.relativePath,
          component_id: component,
          definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
          state: "failed",
          reason: "the TypeScript syntax parser reported errors"
        });
      }
    } else {
      measurements.push(...analyzeStructural(entry, requestedStructural));
      for (const component of requestedStructural) {
        coverage.push({
          unit_id: entry.unitId,
          path: entry.relativePath,
          component_id: component,
          definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
          state: "complete",
          reason: "syntax kernel completed"
        });
      }
    }
  }

  const typeMode: TypeMode = request.options?.typescript_types ?? "auto";
  let typedUnavailableReason: string | undefined;
  if (requestedTyped.size > 0) {
    const typedResult = context.createTypedContext(request, typeMode);
    for (const item of typedResult.diagnostics) records.push(responseDiagnostic(invocationId, item));
    typedUnavailableReason = typedResult.unavailableReason;
    if (typedResult.context !== undefined) {
      for (const entry of context.sources) {
        if (entry.syntaxErrors.length > 0) {
          for (const component of requestedTyped) {
            coverage.push({
              unit_id: entry.unitId,
              path: entry.relativePath,
              component_id: component,
              definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
              state: "failed",
              reason: "syntax errors prevent trustworthy typed analysis"
            });
          }
          continue;
        }
        measurements.push(...analyzeTypeSafety(entry, typedResult.context, requestedTyped));
        for (const component of requestedTyped) {
          coverage.push({
            unit_id: entry.unitId,
            path: entry.relativePath,
            component_id: component,
            definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
            state: "complete",
            reason: "compiler-aware kernel completed with a trustworthy type graph"
          });
        }
      }
    } else {
      const reason = typedUnavailableReason ?? "typed analysis is unavailable";
      if (typeMode === "require") {
        for (const unit of request.units) failedUnits.add(unit.unit_id);
      }
      if (typeMode === "off") {
        records.push(
          responseDiagnostic(invocationId, {
            code: "typescript.typed_components_requested_while_off",
            severity: "error",
            message: "Typed components were requested while options.typescript_types is off."
          })
        );
        for (const unit of request.units) failedUnits.add(unit.unit_id);
      }
      for (const entry of context.sources) {
        for (const component of requestedTyped) {
          coverage.push({
            unit_id: entry.unitId,
            path: entry.relativePath,
            component_id: component,
            definition_version: COMPONENT_BY_ID.get(component)?.definition_version ?? "unknown",
            state: "unavailable",
            reason
          });
        }
      }
    }
  }

  measurements.sort(
    (a, b) =>
      a.path.localeCompare(b.path) ||
      a.component_id.localeCompare(b.component_id) ||
      a.subject.start.offset - b.subject.start.offset ||
      a.subject.name.localeCompare(b.subject.name)
  );
  for (const item of measurements) records.push(responseMeasurement(invocationId, item));

  coverage.sort(
    (a, b) => a.path.localeCompare(b.path) || a.component_id.localeCompare(b.component_id)
  );
  for (const item of coverage) records.push(responseCoverage(invocationId, item));

  const modes = ["typescript-syntax"];
  if (requestedTyped.size > 0 && typeMode !== "off") modes.push("typescript-compiler");
  for (const unit of request.units) {
    const unitSources = context.sources.filter((item) => item.unitId === unit.unit_id);
    records.push({
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
        0
      )
    });
  }

  const allUnitIds = request.units.map((unit) => unit.unit_id);
  const failedUnitIds = allUnitIds.filter((item) => failedUnits.has(item));
  const analyzedUnitIds = allUnitIds.filter((item) => !failedUnits.has(item));
  records.push({
    type: "terminal",
    ...protocolEnvelope(invocationId),
    status: failedUnitIds.length > 0 ? "failure" : "success",
    message:
      failedUnitIds.length > 0
        ? "one or more TypeScript analysis units failed"
        : typedUnavailableReason === undefined
          ? "analysis completed"
          : "structural analysis completed with typed coverage unavailable",
    analyzed_unit_ids: analyzedUnitIds,
    failed_unit_ids: failedUnitIds,
    skipped_unit_ids: []
  });
  return records;
}
