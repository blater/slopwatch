import type ts from "typescript";

export const PROTOCOL_VERSION = 1 as const;
export const ANALYZER_NAME = "slopslap-typescript";
export const ANALYZER_VERSION = "0.1.0";

export type CoverageState =
  "complete" | "unavailable" | "failed" | "not_requested";
export type TypeMode = "auto" | "require" | "off";

export interface ComponentRequest {
  component_id: string;
  definition_version: string;
}

export interface AnalysisUnitRequest {
  unit_id: string;
  language: "typescript";
  source_paths: string[];
  metadata?: Record<string, unknown>;
}

export interface AnalyzerRequest {
  type: "request";
  protocol_version: 1;
  invocation_id: string;
  workspace: string;
  units: AnalysisUnitRequest[];
  components: ComponentRequest[];
  options?: {
    typescript_types?: TypeMode;
    tsconfig?: string;
    [key: string]: unknown;
  };
  limits?: Record<string, unknown>;
}

export interface Position {
  line: number;
  column: number;
  offset: number;
}

export interface Subject {
  name: string;
  symbol: string;
  routine?: string;
  start: Position;
  end: Position;
}

export type JsonInteger = number | { $integer: string };

export interface Measurement {
  unit_id: string;
  component_id: string;
  definition_version: string;
  path: string;
  scope: "file" | "function" | "type" | "expression";
  value: JsonInteger;
  subject: Subject;
  attributes: Record<string, unknown>;
  provenance: {
    analyzer: string;
    analyzer_version: string;
    rule: string;
  };
}

export interface Diagnostic {
  unit_id?: string;
  path?: string;
  component_id?: string;
  code: string;
  severity: "error" | "warning" | "info";
  message: string;
  attributes?: Record<string, unknown>;
}

export interface Coverage {
  unit_id: string;
  path: string;
  component_id: string;
  definition_version: string;
  state: CoverageState;
  reason: string;
}

export interface SourceEntry {
  unitId: string;
  absolutePath: string;
  relativePath: string;
  sourceFile: ts.SourceFile;
  syntaxErrors: readonly ts.Diagnostic[];
}

export function protocolEnvelope(invocationId: string): {
  protocol_version: 1;
  invocation_id: string;
} {
  return { protocol_version: PROTOCOL_VERSION, invocation_id: invocationId };
}

export function jsonInteger(value: bigint): JsonInteger {
  const maxSafe = BigInt(Number.MAX_SAFE_INTEGER);
  return value <= maxSafe ? Number(value) : { $integer: value.toString(10) };
}
