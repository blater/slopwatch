import ts from "typescript";

export type ScalarKind = "numeric" | "boolean" | "string" | "unknown";

export interface BoundaryIdentity {
  artifact: string;
  audience: string;
  view: string;
  symbol: string;
}

export interface DepthBoundary {
  identity: BoundaryIdentity;
  state: "measured" | "partial" | "not_applicable";
  knowledge: Record<string, { state: string; reason?: string; essential: boolean }>;
  burden: { O: number; T: number; A: number; E: number; P: number; S: number; L: number };
  concepts: Array<{ id: string; kind: string; children: string[] }>;
  slots: unknown[];
  route_families: Array<{ id: string; routes: unknown[] }>;
  family_alternatives?: unknown;
  obligations?: unknown;
  files: string[];
  evidence?: Array<Record<string, unknown>>;
}

export interface DepthFlowFunction {
  id: string;
  entry: string;
  formals: Array<{ id: string; path: string; type: string; concept: string; value_kind: ScalarKind }>;
  results: Array<{ id: string; type: string; concept: string; value_kind: ScalarKind }>;
  blocks: Array<{ id: string; instructions: unknown[]; edges: unknown[] }>;
}

export type FunctionLike = ts.FunctionDeclaration | ts.ArrowFunction | ts.FunctionExpression;

export interface LocalCallable {
  id: string;
  node: FunctionLike;
}

export interface DepthFlowArtifact {
  artifact: string;
  language: "typescript";
  functions: DepthFlowFunction[];
  public_routes: unknown[];
}

export interface FlowFamily {
  id: string;
  routes: unknown[];
}

export interface DepthFacts {
  boundaries: DepthBoundary[];
  flows: DepthFlowArtifact[];
  creations: unknown[];
  reasons: unknown[];
}

export interface EvaluatorResponse {
  schema_version: number;
  assessments: DepthBoundary[];
  scores: Array<Record<string, unknown>>;
}
