import { spawnSync } from "node:child_process";
import ts from "typescript";

import type { AnalysisContext, TypedContext } from "./context.js";
import type {
  AnalyzerRequest,
  Coverage,
  Diagnostic,
  Measurement,
  SourceEntry,
  Subject,
} from "./model.js";
import { exportedPassiveCarriers, passiveCarrierEvidence, localPassiveCarrier } from "./passive-result-carrier.js";

const MAX_EVALUATOR_BYTES = 64 * 1024 * 1024;
const DEFINITION = "responsibility-burden-v4";

type ScalarKind = "numeric" | "boolean" | "string" | "unknown";

interface BoundaryIdentity {
  artifact: string;
  audience: string;
  view: string;
  symbol: string;
}

interface DepthBoundary {
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

interface DepthFlowFunction {
  id: string;
  entry: string;
  formals: Array<{ id: string; path: string; type: string; concept: string; value_kind: ScalarKind }>;
  results: Array<{ id: string; type: string; concept: string; value_kind: ScalarKind }>;
  blocks: Array<{ id: string; instructions: unknown[]; edges: unknown[] }>;
}

type FunctionLike = ts.FunctionDeclaration | ts.ArrowFunction | ts.FunctionExpression;

interface LocalCallable {
  id: string;
  node: FunctionLike;
}

interface DepthFlowArtifact {
  artifact: string;
  language: "typescript";
  functions: DepthFlowFunction[];
  public_routes: unknown[];
}

interface FlowFamily {
  id: string;
  routes: unknown[];
}

interface DepthFacts {
  boundaries: DepthBoundary[];
  flows: DepthFlowArtifact[];
  creations: unknown[];
  reasons: unknown[];
}

interface EvaluatorResponse {
  schema_version: number;
  assessments: DepthBoundary[];
  scores: Array<Record<string, unknown>>;
}

export interface DepthAnalysis {
  measurements: Measurement[];
  coverage: Coverage[];
  diagnostics: Diagnostic[];
}

function scalarKind(checker: ts.TypeChecker, type: ts.Type): ScalarKind {
  if (type.isUnion()) {
    const members = type.types.map((member) => scalarKind(checker, member));
    if (members.length > 0 && members.every((member) => member === "numeric")) return "numeric";
    if (members.length > 0 && members.every((member) => member === "boolean")) return "boolean";
    if (members.length > 0 && members.every((member) => member === "string")) return "string";
    return "unknown";
  }
  if (
    (type.flags & (ts.TypeFlags.Number | ts.TypeFlags.NumberLiteral)) !== 0
  )
    return "numeric";
  if (
    (type.flags & (ts.TypeFlags.Boolean | ts.TypeFlags.BooleanLiteral)) !== 0
  )
    return "boolean";
  if ((type.flags & (ts.TypeFlags.String | ts.TypeFlags.StringLiteral)) !== 0)
    return "string";
  return "unknown";
}

function scalarType(kind: ScalarKind): string {
  return kind === "boolean" ? "boolean" : kind === "numeric" ? "number" : kind === "string" ? "string" : "unknown";
}

function sourceSignature(checker: ts.TypeChecker, node: FunctionLike): string | null {
  const signature = checker.getSignatureFromDeclaration(node);
  if (signature === undefined) return null;
  const typeText = (type: ts.Type, location: ts.Node): string => checker.typeToString(type, location, ts.TypeFormatFlags.NoTruncation);
  const parameters = node.parameters.map((parameter) => typeText(checker.getTypeAtLocation(parameter), parameter));
  return `(${parameters.join(",")})->${typeText(checker.getReturnTypeOfSignature(signature), node)}`;
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

function boundaryIdentityString(boundary: Partial<BoundaryIdentity>): string {
  const fields = [boundary.artifact, boundary.audience, boundary.view, boundary.symbol].map(
    (value) => typeof value === "string" ? value : "",
  );
  return fields.map((value) => `${Buffer.byteLength(value, "utf8")}:${value}`).join("");
}

function sourceKind(checker: ts.TypeChecker, node: ts.Node): ScalarKind {
  return scalarKind(checker, checker.getTypeAtLocation(node));
}

function functionID(entry: SourceEntry, node: FunctionLike, fallback = "<anonymous>"): string {
  const name = "name" in node && node.name !== undefined && ts.isIdentifier(node.name) ? node.name.text : fallback;
  return `${entry.relativePath}#${name}`;
}

function constantInstruction(id: string, type: string, kind: ScalarKind, value: string): Record<string, unknown> {
  return {
    id,
    opcode: "constant",
    type,
    value_kind: kind,
    value: { type, value_kind: kind, constant: value },
    operands: [],
    results: [id],
  };
}

function lowerInlineCallable(
  checker: ts.TypeChecker,
  helper: LocalCallable,
  actuals: Array<{ id: string; kind: ScalarKind }>,
  next: { value: number },
  instructions: Record<string, unknown>[],
  helpers: Map<ts.Symbol, LocalCallable>,
): { id: string; kind: ScalarKind } | undefined {
  if (next.value >= 128 || instructions.length >= 128) return undefined;
  const values = new Map<ts.Symbol, string>();
  for (const [index, parameter] of helper.node.parameters.entries()) {
    const symbol = ts.isIdentifier(parameter.name) ? checker.getSymbolAtLocation(parameter.name) : undefined;
    const actual = actuals[index];
    if (symbol === undefined || actual === undefined) return undefined;
    values.set(symbol, actual.id);
  }
  const nestedHelpers = new Map(helpers);
  const helperSymbol = callableSymbol(checker, helper.node) ??
    (ts.isVariableDeclaration(helper.node.parent) && ts.isIdentifier(helper.node.parent.name)
      ? checker.getSymbolAtLocation(helper.node.parent.name) : undefined);
  if (helperSymbol !== undefined) nestedHelpers.delete(helperSymbol);
  if (helper.node.body !== undefined && !ts.isBlock(helper.node.body)) {
    return lowerExpression(checker, helper.node.body, values, next, instructions, nestedHelpers);
  }
  if (helper.node.body === undefined || !ts.isBlock(helper.node.body)) return undefined;
  for (const statement of helper.node.body.statements.slice(0, -1)) {
    if (!ts.isVariableStatement(statement) || statement.declarationList.declarations.length !== 1) return undefined;
    const declaration = statement.declarationList.declarations[0];
    if (declaration === undefined || !ts.isIdentifier(declaration.name) || declaration.initializer === undefined) return undefined;
    const value = lowerExpression(checker, declaration.initializer, values, next, instructions, nestedHelpers);
    const symbol = checker.getSymbolAtLocation(declaration.name);
    if (value === undefined || symbol === undefined) return undefined;
    values.set(symbol, value.id);
  }
  const last = helper.node.body.statements.at(-1);
  if (last === undefined || !ts.isReturnStatement(last) || last.expression === undefined) return undefined;
  return lowerExpression(checker, last.expression, values, next, instructions, nestedHelpers);
}

function lowerExpression(
  checker: ts.TypeChecker,
  expression: ts.Expression,
  values: Map<ts.Symbol, string>,
  next: { value: number },
  instructions: Record<string, unknown>[],
  helpers: Map<ts.Symbol, LocalCallable> = new Map(),
): { id: string; kind: ScalarKind } | undefined {
  if (next.value >= 128 || instructions.length >= 128) return undefined;
  if (ts.isParenthesizedExpression(expression))
    return lowerExpression(checker, expression.expression, values, next, instructions, helpers);
  const kind = sourceKind(checker, expression);
  const type = scalarType(kind);
  if (ts.isIdentifier(expression)) {
    const symbol = checker.getSymbolAtLocation(expression);
    const id = symbol === undefined ? undefined : values.get(symbol);
    return id === undefined ? undefined : { id, kind };
  }
  if (ts.isNumericLiteral(expression) || expression.kind === ts.SyntaxKind.TrueKeyword || expression.kind === ts.SyntaxKind.FalseKeyword) {
    const id = `n${++next.value}`;
    instructions.push(constantInstruction(id, type, kind, expression.getText()));
    return { id, kind };
  }
  if (ts.isStringLiteral(expression) || ts.isNoSubstitutionTemplateLiteral(expression)) {
    const id = `n${++next.value}`;
    instructions.push(constantInstruction(id, "string", "string", JSON.stringify(expression.text)));
    return { id, kind: "string" };
  }
  if (ts.isPrefixUnaryExpression(expression) && expression.operator === ts.SyntaxKind.MinusToken && ts.isNumericLiteral(expression.operand)) {
    const id = `n${++next.value}`;
    instructions.push(constantInstruction(id, type, kind, expression.getText()));
    return { id, kind };
  }
  if (ts.isPrefixUnaryExpression(expression) && expression.operator === ts.SyntaxKind.ExclamationToken) {
    const operand = lowerExpression(checker, expression.operand, values, next, instructions, helpers);
    if (operand === undefined || operand.kind !== "boolean" || kind !== "boolean") return undefined;
    const id = `n${++next.value}`;
    instructions.push({ id, opcode: "primitive", type: "boolean", value_kind: "boolean", arithmetic_mode: "boolean", operator: "!", operands: [operand.id], results: [id] });
    return { id, kind: "boolean" };
  }
  if (ts.isBinaryExpression(expression)) {
    const left = lowerExpression(checker, expression.left, values, next, instructions, helpers);
    const right = lowerExpression(checker, expression.right, values, next, instructions, helpers);
    const operator = expression.operatorToken.getText();
    const supported = new Set(["+", "-", "*", "/", "%", "<", "<=", ">", ">=", "==", "===", "!=", "!=="]);
    if (left === undefined || right === undefined || !supported.has(operator)) return undefined;
    if (kind === "string" && left.kind === "string" && right.kind === "string" && operator === "+") {
      const id = `n${++next.value}`;
      instructions.push({ id, opcode: "primitive", type: "string", value_kind: "string", arithmetic_mode: "text", operator, operands: [left.id, right.id], results: [id] });
      return { id, kind };
    }
    if (kind === "unknown" || (left.kind !== "numeric" && left.kind !== "boolean") || (right.kind !== "numeric" && right.kind !== "boolean")) return undefined;
    const id = `n${++next.value}`;
    const booleanComparison =
      kind === "boolean" &&
      left.kind === "boolean" &&
      right.kind === "boolean" &&
      ["==", "===", "!=", "!=="].includes(operator);
    instructions.push({
      id,
      opcode: "primitive",
      type,
      value_kind: kind,
      arithmetic_mode: booleanComparison ? "boolean" : "js_number",
      operator: operator === "===" ? "==" : operator === "!==" ? "!=" : operator,
      operands: [left.id, right.id],
      results: [id],
    });
    return { id, kind };
  }
  if (ts.isCallExpression(expression) && ts.isIdentifier(expression.expression)) {
    const symbol = checker.getSymbolAtLocation(expression.expression);
    const helper = symbol === undefined ? undefined : helpers.get(symbol);
    if (helper === undefined || expression.arguments.length !== helper.node.parameters.length) return undefined;
    const actuals: Array<{ id: string; kind: ScalarKind }> = [];
    for (const argument of expression.arguments) {
      if (!ts.isExpression(argument)) return undefined;
      const actual = lowerExpression(checker, argument, values, next, instructions, helpers);
      if (actual === undefined) return undefined;
      actuals.push(actual);
    }
    const signature = checker.getSignatureFromDeclaration(helper.node);
    const resultKind = signature === undefined ? "unknown" : scalarKind(checker, checker.getReturnTypeOfSignature(signature));
    if (signature === undefined || resultKind === "unknown") return undefined;
    const inlined = lowerInlineCallable(checker, helper, actuals, next, instructions, helpers);
    if (inlined === undefined || inlined.kind !== resultKind) return undefined;
    return inlined;
  }
  return undefined;
}

function exportedFunction(node: ts.Statement): node is ts.FunctionDeclaration {
  return ts.isFunctionDeclaration(node) && node.name !== undefined &&
    ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

function exportedStatement(node: ts.Statement): boolean {
  return ts.canHaveModifiers(node) &&
    ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

function hasExportModifier(node: ts.Node): boolean {
  return ts.canHaveModifiers(node) &&
    ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

function callableSymbol(checker: ts.TypeChecker, node: FunctionLike): ts.Symbol | undefined {
  if ("name" in node && node.name !== undefined && ts.isIdentifier(node.name)) return checker.getSymbolAtLocation(node.name);
  return undefined;
}

function sourceCallables(entry: SourceEntry, source: ts.SourceFile, checker: ts.TypeChecker): LocalCallable[] {
  const callables: LocalCallable[] = [];
  for (const statement of source.statements) {
    if (ts.isFunctionDeclaration(statement) && statement.name !== undefined && statement.body !== undefined) {
      callables.push({ id: functionID(entry, statement), node: statement });
      continue;
    }
    if (!ts.isVariableStatement(statement)) continue;
    for (const declaration of statement.declarationList.declarations) {
      const initializer = declaration.initializer;
      if (!ts.isIdentifier(declaration.name) || (statement.declarationList.flags & ts.NodeFlags.Const) === 0 || initializer === undefined ||
          (!ts.isArrowFunction(initializer) && !ts.isFunctionExpression(initializer))) continue;
      callables.push({ id: `${entry.relativePath}#${declaration.name.text}`, node: initializer });
    }
  }
  for (const statement of source.statements) {
    if (!ts.isExportAssignment(statement) || (!ts.isArrowFunction(statement.expression) && !ts.isFunctionExpression(statement.expression))) continue;
    callables.push({ id: `${entry.relativePath}#default`, node: statement.expression });
  }
  return callables;
}

function exportedCallableSymbols(source: ts.SourceFile, checker: ts.TypeChecker): Set<ts.Symbol> {
  const result = new Set<ts.Symbol>();
  for (const statement of source.statements) {
    if (ts.isFunctionDeclaration(statement) && hasExportModifier(statement) && statement.name !== undefined) {
      const symbol = checker.getSymbolAtLocation(statement.name);
      if (symbol !== undefined) result.add(symbol);
    }
    if (ts.isVariableStatement(statement) && hasExportModifier(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        if (!ts.isIdentifier(declaration.name)) continue;
        const symbol = checker.getSymbolAtLocation(declaration.name);
        if (symbol !== undefined) result.add(symbol);
      }
    }
    if (ts.isExportDeclaration(statement) && statement.exportClause !== undefined && ts.isNamedExports(statement.exportClause)) {
      for (const specifier of statement.exportClause.elements) {
        const symbol = checker.getSymbolAtLocation(specifier.propertyName ?? specifier.name);
        if (symbol === undefined) continue;
        result.add(symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol);
      }
    }
    if (ts.isExportAssignment(statement) && ts.isIdentifier(statement.expression)) {
      const symbol = checker.getSymbolAtLocation(statement.expression);
      if (symbol !== undefined) result.add(symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol);
    }
  }
  return result;
}

function singleThrow(statement: ts.Statement): ts.ThrowStatement | undefined {
  if (ts.isThrowStatement(statement)) return statement;
  const only = ts.isBlock(statement) && statement.statements.length === 1 ? statement.statements[0] : undefined;
  if (only !== undefined && ts.isThrowStatement(only)) return only;
  return undefined;
}

function singleReturn(statement: ts.Statement): ts.ReturnStatement | undefined {
  if (ts.isReturnStatement(statement)) return statement;
  const only = ts.isBlock(statement) && statement.statements.length === 1 ? statement.statements[0] : undefined;
  if (only !== undefined && ts.isReturnStatement(only)) return only;
  return undefined;
}

function isPureBuiltinError(checker: ts.TypeChecker, statement: ts.ThrowStatement): boolean {
  const expression = statement.expression;
  if (!ts.isNewExpression(expression) || !ts.isIdentifier(expression.expression)) return false;
  if (expression.expression.text !== "Error") return false;
  const symbol = checker.getSymbolAtLocation(expression.expression);
  if (symbol === undefined || symbol.declarations === undefined || symbol.declarations.length === 0) return false;
  if (symbol.declarations.some((declaration) => !declaration.getSourceFile().isDeclarationFile)) return false;
  return (expression.arguments ?? []).every((argument) => ts.isStringLiteral(argument) || ts.isNoSubstitutionTemplateLiteral(argument));
}

function lowerConditionalReturnFunction(
  checker: ts.TypeChecker,
  node: FunctionLike,
  id: string,
  formals: DepthFlowFunction["formals"],
  returnKind: ScalarKind,
  values: Map<ts.Symbol, string>,
  next: { value: number },
  helpers: Map<ts.Symbol, LocalCallable>,
): { flow: DepthFlowFunction; supported: boolean } | undefined {
  if (node.body === undefined || !ts.isBlock(node.body) || node.body.statements.length !== 1) return undefined;
  const statement = node.body.statements[0];
  if (statement === undefined || !ts.isReturnStatement(statement) || statement.expression === undefined || !ts.isConditionalExpression(statement.expression)) return undefined;
  const conditional = statement.expression;
  const entryInstructions: Record<string, unknown>[] = [];
  const condition = lowerExpression(checker, conditional.condition, values, next, entryInstructions, helpers);
  if (condition === undefined || condition.kind !== "boolean") return { flow: { id, entry: "entry", formals, results: [], blocks: [] }, supported: false };
  let branchCondition = condition;
  if (ts.isIdentifier(conditional.condition)) {
    const truth = `n${++next.value}`;
    entryInstructions.push(constantInstruction(truth, "boolean", "boolean", "true"));
    const normalized = `n${++next.value}`;
    entryInstructions.push({ id: normalized, opcode: "primitive", type: "boolean", value_kind: "boolean", arithmetic_mode: "boolean", operator: "==", operands: [condition.id, truth], results: [normalized] });
    branchCondition = { id: normalized, kind: "boolean" };
  }
  const trueInstructions: Record<string, unknown>[] = [];
  const falseInstructions: Record<string, unknown>[] = [];
  const trueValue = lowerExpression(checker, conditional.whenTrue, new Map(values), next, trueInstructions, helpers);
  const falseValue = lowerExpression(checker, conditional.whenFalse, new Map(values), next, falseInstructions, helpers);
  if (trueValue === undefined || falseValue === undefined || trueValue.kind !== returnKind || falseValue.kind !== returnKind) return { flow: { id, entry: "entry", formals, results: [], blocks: [] }, supported: false };
  entryInstructions.push({ id: `branch${next.value + 1}`, opcode: "branch", operands: [branchCondition.id], results: [] });
  const joined = `n${++next.value}`;
  const joinInstructions: Record<string, unknown>[] = [
    { id: joined, opcode: "phi", type: scalarType(returnKind), value_kind: returnKind, results: [joined], operands: [], phi_inputs: [
      { predecessor: "then", value: trueValue.id }, { predecessor: "else", value: falseValue.id },
    ] },
    { id: `return${next.value + 1}`, opcode: "return", operands: [joined], results: [] },
  ];
  const result = { id: "result0", type: scalarType(returnKind), concept: returnKind === "boolean" ? "boolean" : returnKind === "numeric" ? "number" : "unknown", value_kind: returnKind };
  return { flow: { id, entry: "entry", formals, results: [result], blocks: [
    { id: "entry", instructions: entryInstructions, edges: [
      { from: "entry", to: "then", kind: "true", guard: branchCondition.id, guard_polarity: "true" },
      { from: "entry", to: "else", kind: "false", guard: branchCondition.id, guard_polarity: "false" },
    ] },
    { id: "then", instructions: trueInstructions, edges: [{ from: "then", to: "join", kind: "normal" }] },
    { id: "else", instructions: falseInstructions, edges: [{ from: "else", to: "join", kind: "normal" }] },
    { id: "join", instructions: joinInstructions, edges: [] },
  ] }, supported: true };
}

function lowerBranchFunction(
  entry: SourceEntry,
  checker: ts.TypeChecker,
  node: FunctionLike,
  id: string,
  formals: DepthFlowFunction["formals"],
  returnKind: ScalarKind,
  values: Map<ts.Symbol, string>,
  next: { value: number },
  helpers: Map<ts.Symbol, LocalCallable>,
): { flow: DepthFlowFunction; supported: boolean } | undefined {
  if (node.body === undefined || !ts.isBlock(node.body)) return undefined;
  const statements = [...node.body.statements];
  const first = statements[0];
  if (first === undefined || !ts.isIfStatement(first)) return undefined;
  const branch = first;
  const thenThrow = singleThrow(branch.thenStatement);
  const thenReturn = singleReturn(branch.thenStatement);
  const elseReturn = branch.elseStatement === undefined ? undefined : singleReturn(branch.elseStatement);
  const tail = statements.slice(1);
  const entryInstructions: Record<string, unknown>[] = [];
  const condition = lowerExpression(checker, branch.expression, values, next, entryInstructions, helpers);
  if (condition === undefined || condition.kind !== "boolean") return { flow: { id, entry: "entry", formals, results: [], blocks: [] }, supported: false };
  entryInstructions.push({ id: `branch${next.value + 1}`, opcode: "branch", operands: [condition.id], results: [] });
  const trueInstructions: Record<string, unknown>[] = [];
  const falseInstructions: Record<string, unknown>[] = [];
  const blocks: DepthFlowFunction["blocks"] = [];
  const trueID = "then";
  const falseID = "else";
  const resultMeta = { id: "result0", type: scalarType(returnKind), concept: returnKind === "boolean" ? "boolean" : returnKind === "numeric" ? "number" : "unknown", value_kind: returnKind };
  if (thenThrow !== undefined && branch.elseStatement === undefined && !isPureBuiltinError(checker, thenThrow)) return { flow: { id, entry: "entry", formals, results: [], blocks: [] }, supported: false };
  if (thenThrow !== undefined && branch.elseStatement === undefined && thenReturn === undefined) {
    const falseValues = new Map(values);
    let result: { id: string; kind: ScalarKind } | undefined;
    for (const statement of tail) {
      if (ts.isVariableStatement(statement) && statement.declarationList.declarations.length === 1) {
        const declaration = statement.declarationList.declarations[0];
        if (declaration !== undefined && ts.isIdentifier(declaration.name) && declaration.initializer !== undefined) {
          const value = lowerExpression(checker, declaration.initializer, falseValues, next, falseInstructions, helpers);
          const symbol = checker.getSymbolAtLocation(declaration.name);
          if (value !== undefined && symbol !== undefined) { falseValues.set(symbol, value.id); continue; }
        }
      }
      if (ts.isReturnStatement(statement) && statement.expression !== undefined) {
        result = lowerExpression(checker, statement.expression, falseValues, next, falseInstructions, helpers);
        break;
      }
      return { flow: { id, entry: "entry", formals, results: [resultMeta], blocks: [] }, supported: false };
    }
    if (result === undefined || result.kind !== returnKind) return { flow: { id, entry: "entry", formals, results: [resultMeta], blocks: [] }, supported: false };
    trueInstructions.push({ id: `throw${next.value + 1}`, opcode: "throw", operands: [], results: [] });
    falseInstructions.push({ id: `return${next.value + 1}`, opcode: "return", operands: [result.id], results: [] });
    blocks.push({ id: "entry", instructions: entryInstructions, edges: [
      { from: "entry", to: trueID, kind: "true", guard: condition.id, guard_polarity: "true" },
      { from: "entry", to: falseID, kind: "false", guard: condition.id, guard_polarity: "false" },
    ] }, { id: trueID, instructions: trueInstructions, edges: [] }, { id: falseID, instructions: falseInstructions, edges: [] });
    return { flow: { id, entry: "entry", formals, results: [resultMeta], blocks }, supported: true };
  }
  if (thenReturn !== undefined && elseReturn !== undefined && thenReturn.expression !== undefined && elseReturn.expression !== undefined && tail.length === 0) {
    const thenValue = lowerExpression(checker, thenReturn.expression, new Map(values), next, trueInstructions, helpers);
    const elseValue = lowerExpression(checker, elseReturn.expression, new Map(values), next, falseInstructions, helpers);
    if (thenValue === undefined || elseValue === undefined || thenValue.kind !== returnKind || elseValue.kind !== returnKind) return { flow: { id, entry: "entry", formals, results: [resultMeta], blocks: [] }, supported: false };
    trueInstructions.push({ id: `return${next.value + 1}`, opcode: "return", operands: [thenValue.id], results: [] });
    falseInstructions.push({ id: `return${next.value + 1}`, opcode: "return", operands: [elseValue.id], results: [] });
    blocks.push({ id: "entry", instructions: entryInstructions, edges: [
      { from: "entry", to: trueID, kind: "true", guard: condition.id, guard_polarity: "true" },
      { from: "entry", to: falseID, kind: "false", guard: condition.id, guard_polarity: "false" },
    ] }, { id: trueID, instructions: trueInstructions, edges: [] }, { id: falseID, instructions: falseInstructions, edges: [] });
    return { flow: { id, entry: "entry", formals, results: [resultMeta], blocks }, supported: true };
  }
  return undefined;
}

function lowerFunction(entry: SourceEntry, checker: ts.TypeChecker, node: FunctionLike,
  helpers: Map<ts.Symbol, LocalCallable>, callableID = functionID(entry, node)): { flow: DepthFlowFunction; supported: boolean; concepts: Set<string> } {
  const id = callableID;
  const signature = checker.getSignatureFromDeclaration(node);
  const returnKind = signature === undefined ? "unknown" : scalarKind(checker, checker.getReturnTypeOfSignature(signature));
  const values = new Map<ts.Symbol, string>();
  const formals: DepthFlowFunction["formals"] = [];
  let supported = signature !== undefined && returnKind !== "unknown" && node.body !== undefined;
  const concepts = new Set<string>();
  for (const [index, parameter] of node.parameters.entries()) {
    const kind = sourceKind(checker, parameter);
    const symbol = ts.isIdentifier(parameter.name) ? checker.getSymbolAtLocation(parameter.name) : undefined;
    if (symbol !== undefined) values.set(symbol, `arg${index}`);
    if (
      kind === "unknown" ||
      !ts.isIdentifier(parameter.name) ||
      parameter.questionToken !== undefined ||
      parameter.dotDotDotToken !== undefined ||
      parameter.initializer !== undefined
    ) supported = false;
    else concepts.add(scalarType(kind));
    formals.push({ id: `arg${index}`, path: `${id}/arg${index}`, type: scalarType(kind), concept: scalarType(kind), value_kind: kind });
  }
  concepts.add(scalarType(returnKind));
  const conditional = lowerConditionalReturnFunction(checker, node, id, formals, returnKind, values, { value: 0 }, helpers);
  if (conditional !== undefined) return { ...conditional, supported: supported && conditional.supported, concepts };
  const structured = lowerBranchFunction(entry, checker, node, id, formals, returnKind, values, { value: 0 }, helpers);
  if (structured !== undefined) return { ...structured, supported: supported && structured.supported, concepts };
  const instructions: Record<string, unknown>[] = [];
  const next = { value: 0 };
  let result: { id: string; kind: ScalarKind } | undefined;
  const statements = node.body !== undefined && ts.isBlock(node.body) ? [...node.body.statements] : [];
  const finalStatement = statements.at(-1);
  if (node.body !== undefined && !ts.isBlock(node.body)) {
    result = lowerExpression(checker, node.body, values, next, instructions, helpers);
    if (result === undefined || result.kind !== returnKind) supported = false;
  } else if (finalStatement === undefined || !ts.isReturnStatement(finalStatement) || finalStatement.expression === undefined) {
    supported = false;
  } else {
    for (const statement of statements.slice(0, -1)) {
      if (!ts.isVariableStatement(statement) || statement.declarationList.declarations.length !== 1) {
        supported = false;
        break;
      }
      const declaration = statement.declarationList.declarations[0];
      if (declaration === undefined || !ts.isIdentifier(declaration.name) || declaration.initializer === undefined) {
        supported = false;
        break;
      }
      const value = lowerExpression(checker, declaration.initializer, values, next, instructions, helpers);
      const symbol = checker.getSymbolAtLocation(declaration.name);
      if (value === undefined || symbol === undefined) {
        supported = false;
        break;
      }
      values.set(symbol, value.id);
    }
    if (supported) {
      result = lowerExpression(checker, finalStatement.expression, values, next, instructions, helpers);
      if (result === undefined || result.kind !== returnKind) supported = false;
    }
  }
  if (!supported) {
    instructions.length = 0;
    instructions.push({ id: "unknown", opcode: "unknown", type: "unknown", value_kind: "unknown", operands: [], results: ["unknown"] });
    result = { id: "unknown", kind: "unknown" };
  }
  instructions.push({ id: `return${instructions.length + 1}`, opcode: "return", operands: [result?.id ?? "unknown"] });
  return {
    flow: {
      id,
      entry: "entry",
      formals,
      results: [{ id: "result0", type: scalarType(returnKind), concept: returnKind === "boolean" ? "boolean" : returnKind === "numeric" ? "number" : "unknown", value_kind: returnKind }],
      blocks: [{ id: "entry", instructions, edges: [] }],
    },
    supported,
    concepts,
  };
}

function flowFamilyKey(flow: DepthFlowFunction): string {
  if (flow.blocks.length === 0 || flow.results.length === 0) return `unique:${flow.id}`;
  const validationGuard = simpleValidationGuardFlow(flow);
  if (!validationGuard && (flow.blocks.length !== 1 || flow.blocks[0]?.edges.length !== 0)) return `unique:${flow.id}`;
  const definitions = new Map<string, Record<string, unknown>>();
  const returns: string[] = [];
  for (const block of flow.blocks) {
    for (const raw of block.instructions) {
      if (typeof raw !== "object" || raw === null) return `unique:${flow.id}`;
      const instruction = raw as Record<string, unknown>;
      if (typeof instruction.id !== "string" || typeof instruction.opcode !== "string") return `unique:${flow.id}`;
      if (instruction.opcode === "call" || instruction.opcode === "phi" || instruction.opcode === "unknown") return `unique:${flow.id}`;
      if (instruction.opcode === "branch" && !validationGuard) return `unique:${flow.id}`;
      if (instruction.opcode === "throw" && !validationGuard) return `unique:${flow.id}`;
      definitions.set(instruction.id, instruction);
      if (instruction.opcode === "return") {
        const operands = instruction.operands;
        if (!Array.isArray(operands) || operands.length !== 1 || typeof operands[0] !== "string") return `unique:${flow.id}`;
        returns.push(operands[0]);
      }
    }
  }
  // The bounded canonical form is intentionally a single returned SSA
  // outcome. Multiple return outcomes need selector/path information; treating
  // their set as unordered would merge conditionally distinct computations.
  if (returns.length !== 1) return `unique:${flow.id}`;
  const visiting = new Set<string>();
  const memo = new Map<string, string>();
  let budget = 512;
  const expression = (id: string): string | undefined => {
    const cached = memo.get(id);
    if (cached !== undefined) return cached;
    if (--budget < 0 || visiting.size >= 64) return undefined;
    if (id.startsWith("arg") && /^arg\d+$/.test(id)) return id;
    const instruction = definitions.get(id);
    if (instruction === undefined || visiting.has(id)) return undefined;
    visiting.add(id);
    let result: string | undefined;
    switch (instruction.opcode) {
      case "constant": {
        const value = instruction.value;
        if (typeof value !== "object" || value === null || !("constant" in value)) break;
        result = `constant:${JSON.stringify((value as Record<string, unknown>).constant)}`;
        break;
      }
      case "primitive": {
        if (typeof instruction.operator !== "string" || typeof instruction.type !== "string" || !Array.isArray(instruction.operands)) break;
        const operands = instruction.operands.map((operand) => typeof operand === "string" ? expression(operand) : undefined);
        if (operands.some((operand) => operand === undefined)) break;
        if (operands.reduce((size, operand) => size + (operand?.length ?? 0), 0) > 8192) break;
        result = `primitive:${instruction.operator}:${instruction.type}(${operands.join(",")})`;
        break;
      }
      default:
        break;
    }
    visiting.delete(id);
    if (result !== undefined && result.length <= 16384) memo.set(id, result);
    else result = undefined;
    return result;
  };
  const outcomes = returns.map(expression);
  if (outcomes.some((outcome) => outcome === undefined)) return `unique:${flow.id}`;
  return JSON.stringify({
    parameters: flow.formals.map((formal) => formal.value_kind),
    results: flow.results.map((result) => result.value_kind),
    outcomes: [...new Set(outcomes)].sort(),
  });
}

function simpleValidationGuardFlow(flow: DepthFlowFunction): boolean {
  if (flow.blocks.length !== 3) return false;
  const entry = flow.blocks[0];
  const edgeKind = (edge: unknown): unknown =>
    typeof edge === "object" && edge !== null ? (edge as Record<string, unknown>).kind : undefined;
  if (entry === undefined || entry.edges.length !== 2 || !entry.edges.every((edge) => edgeKind(edge) === "true" || edgeKind(edge) === "false")) return false;
  const branches = flow.blocks.slice(1);
  if (branches.some((block) => block.edges.length !== 0)) return false;
  const throwing = branches.filter((block) => block.instructions.length === 1 && block.instructions[0] !== undefined && typeof block.instructions[0] === "object" && block.instructions[0] !== null && (block.instructions[0] as Record<string, unknown>).opcode === "throw");
  if (throwing.length !== 1) return false;
  const normal = branches.find((block) => block !== throwing[0]);
  return normal !== undefined && normal.instructions.some((instruction) => typeof instruction === "object" && instruction !== null && (instruction as Record<string, unknown>).opcode === "return") && !normal.instructions.some((instruction) => typeof instruction === "object" && instruction !== null && ["branch", "phi", "throw", "call", "unknown"].includes(String((instruction as Record<string, unknown>).opcode)));
}

function buildFacts(context: AnalysisContext, typed: TypedContext): { facts: DepthFacts; entries: SourceEntry[] } {
  const boundaries: DepthBoundary[] = [];
  const flows: DepthFlowArtifact[] = [];
  const entries: SourceEntry[] = [];
  for (const entry of context.sources) {
    if (entry.isDeclaration || entry.syntaxErrors.length > 0) continue;
    const source = typed.sourceFiles.get(entry.absolutePath);
    if (source === undefined) continue;
    const callables = sourceCallables(entry, source, typed.checker);
    const symbols = exportedCallableSymbols(source, typed.checker);
    const passiveEnums = source.statements.filter((statement): statement is ts.EnumDeclaration => ts.isEnumDeclaration(statement) && hasExportModifier(statement) && simpleEnum(statement));
    const passiveCarriers = exportedPassiveCarriers(source, typed.checker);
    const provenPassive = passiveCarriers.filter((carrier) => carrier.proof !== undefined);
    const provenPassiveNodes = new Set(provenPassive.map((carrier) => carrier.node));
    const passiveClassSymbol = (symbol: ts.Symbol | undefined): boolean => {
      const resolved = symbol !== undefined && (symbol.flags & ts.SymbolFlags.Alias) !== 0
        ? typed.checker.getAliasedSymbol(symbol) : symbol;
      return passiveCarriers.some((carrier) => carrier.proof !== undefined
        && carrier.node.name !== undefined
        && typed.checker.getSymbolAtLocation(carrier.node.name) === resolved);
    };
    const hasDefaultCallable = source.statements.some((statement) => ts.isExportAssignment(statement) && (ts.isArrowFunction(statement.expression) || ts.isFunctionExpression(statement.expression)));
    const exported = callables.filter((callable) => {
      if (callable.id.endsWith("#default")) return hasDefaultCallable;
      const symbol = callableSymbol(typed.checker, callable.node) ??
        (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
          ? typed.checker.getSymbolAtLocation(callable.node.parent.name) : undefined);
      return symbol !== undefined && symbols.has(symbol);
    });
    const unsupportedExport = source.statements.some((statement) => {
      if (ts.isExportAssignment(statement)) return !ts.isIdentifier(statement.expression) && !hasDefaultCallable
        || ts.isIdentifier(statement.expression) && exported.length === 0 && !passiveClassSymbol(typed.checker.getSymbolAtLocation(statement.expression));
      if (ts.isExportDeclaration(statement)) {
        if (statement.exportClause === undefined || !ts.isNamedExports(statement.exportClause)) return true;
        return statement.exportClause.elements.some((specifier) => {
          const symbol = typed.checker.getSymbolAtLocation(specifier.propertyName ?? specifier.name);
          const resolved = symbol !== undefined && (symbol.flags & ts.SymbolFlags.Alias ? typed.checker.getAliasedSymbol(symbol) : symbol);
          return !resolved || (!exported.some((callable) =>
            (callableSymbol(typed.checker, callable.node) ??
              (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
                ? typed.checker.getSymbolAtLocation(callable.node.parent.name) : undefined)) === resolved)
            && !passiveClassSymbol(resolved));
        });
      }
      if (ts.isClassDeclaration(statement) && hasExportModifier(statement)) return !provenPassiveNodes.has(statement);
      if (ts.isEnumDeclaration(statement) && hasExportModifier(statement)) return !passiveEnums.includes(statement);
      if (ts.isVariableStatement(statement) && hasExportModifier(statement)) {
        return statement.declarationList.declarations.some((declaration) => {
          const initializer = declaration.initializer;
          return (statement.declarationList.flags & ts.NodeFlags.Const) === 0 || !ts.isIdentifier(declaration.name) || initializer === undefined || (!ts.isArrowFunction(initializer) && !ts.isFunctionExpression(initializer));
        });
      }
      return hasExportModifier(statement) && (ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement));
    });
    if (exported.length === 0 && !unsupportedExport && provenPassive.length === 0 && passiveEnums.length === 0) continue;
    entries.push(entry);
    const identity: BoundaryIdentity = { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath };
    const flowFunctions: DepthFlowFunction[] = [];
    const families: DepthBoundary["route_families"] = [];
    const familyByKey = new Map<string, FlowFamily>();
    const concepts = new Set<string>();
    let complete = !unsupportedExport;
    for (const statement of source.statements) {
      if (
        (ts.isImportDeclaration(statement) && statement.importClause !== undefined) ||
        ts.isImportEqualsDeclaration(statement) ||
        ts.isFunctionDeclaration(statement) ||
        ts.isVariableStatement(statement) ||
        ts.isInterfaceDeclaration(statement) ||
        ts.isTypeAliasDeclaration(statement) ||
        ts.isEnumDeclaration(statement) && simpleEnum(statement) ||
        ts.isClassDeclaration(statement) && (provenPassiveNodes.has(statement) || harmlessUnexportedClass(statement, typed.checker)) ||
        (ts.isExportDeclaration(statement) && statement.exportClause !== undefined && ts.isNamedExports(statement.exportClause)) ||
        (ts.isExportAssignment(statement) && (ts.isIdentifier(statement.expression) || hasDefaultCallable))
      )
        continue;
      complete = false;
    }
    const helperMap = new Map<ts.Symbol, LocalCallable>();
    for (const callable of callables) {
      const symbol = callableSymbol(typed.checker, callable.node) ??
        (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
          ? typed.checker.getSymbolAtLocation(callable.node.parent.name) : undefined);
      if (symbol !== undefined) helperMap.set(symbol, callable);
    }
    if ((exported.length === 0 && provenPassive.length === 0 && passiveEnums.length === 0) || unsupportedExport) {
      const id = `${entry.relativePath}#<unsupported-export>`;
      flowFunctions.push({
        id,
        entry: "entry",
        formals: [],
        results: [{ id: "result0", type: "unknown", concept: "unknown", value_kind: "unknown" }],
        blocks: [{ id: "entry", instructions: [{ id: "unknown", opcode: "unknown", type: "unknown", value_kind: "unknown", operands: [], results: ["unknown"] }, { id: "return", opcode: "return", operands: ["unknown"] }], edges: [] }],
      });
      families.push({ id, routes: [{ id, family: id, target_function_id: id, boundary: identity, signature: null, required_slots: [], exposed_slots: [] }] });
      concepts.add("unknown");
      complete = false;
    }
    for (const callable of callables) {
      const lowered = lowerFunction(entry, typed.checker, callable.node, helperMap, callable.id);
      flowFunctions.push(lowered.flow);
      const symbol = callableSymbol(typed.checker, callable.node) ??
        (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
          ? typed.checker.getSymbolAtLocation(callable.node.parent.name) : undefined);
      const isExported = callable.id.endsWith("#default") || (symbol !== undefined && symbols.has(symbol));
      if (isExported) complete &&= lowered.supported;
      if (!isExported) continue;
      for (const concept of lowered.concepts) concepts.add(concept);
      const signature = sourceSignature(typed.checker, callable.node);
      const key = flowFamilyKey(lowered.flow);
      let family = familyByKey.get(key);
      if (family === undefined) {
        family = { id: lowered.flow.id, routes: [] };
        familyByKey.set(key, family);
        families.push(family);
      }
      // Routes in one proven family must expose the same canonical slots. Using
      // the per-flow id here makes an otherwise-equivalent checked/raw pair
      // look like two independent exposed responsibilities to the evaluator.
      const slots = callable.node.parameters.map((_, index) => `${family.id}/arg${index}`);
      family.routes.push({ id: lowered.flow.id, family: family.id, target_function_id: lowered.flow.id, boundary: identity, signature, required_slots: slots, exposed_slots: slots });
    }
    for (const carrier of provenPassive) {
      const name = carrier.node.name?.text ?? "<anonymous-class>";
      const id = `${entry.relativePath}#${name}`;
      families.push({ id, routes: [{ id, family: id, target_function_id: id, boundary: identity, signature: null, required_slots: [], exposed_slots: [] }] });
    }
    for (const enumeration of passiveEnums) {
      const id = `${entry.relativePath}#${enumeration.name.text}`;
      families.push({ id, routes: [{ id, family: id, target_function_id: id, boundary: identity, signature: null, required_slots: [], exposed_slots: [] }] });
    }
    const passiveOnly = (provenPassive.length > 0 || passiveEnums.length > 0) && exported.length === 0 && !unsupportedExport && complete
      && !source.statements.some(ts.isVariableStatement);
    const knowledgeState = complete ? "measured" : "partial";
    const knowledge = Object.fromEntries(["inventory", "burden", "behavior", "alias_effects"].map((key) => [key, { state: knowledgeState, essential: true }]));
    const boundary: DepthBoundary = {
      identity,
      state: knowledgeState,
      knowledge,
      burden: { O: families.length, T: concepts.size, A: 0, E: 0, P: 0, S: 0, L: 0 },
      concepts: [...concepts].sort().map((id) => ({ id, kind: id, children: [] })),
      slots: [],
      route_families: families,
      files: [entry.relativePath],
    };
    if (passiveOnly) boundary.evidence = [
      ...provenPassive.map((carrier) => passiveCarrierEvidence(source, carrier.proof!)),
      ...passiveEnums.map((enumeration) => ({ id: `${entry.relativePath}#${enumeration.name.text}/passive-enum`, kind: "passive-enum-v1", status: "proven", details: { variants: enumeration.members.length }, provenance: [] })),
    ];
    boundaries.push(boundary);
    flows.push({ artifact: entry.relativePath, language: "typescript", functions: flowFunctions, public_routes: [] });
  }
  return { facts: { boundaries, flows, creations: [], reasons: [] }, entries };
}

function harmlessUnexportedClass(statement: ts.ClassDeclaration, checker: ts.TypeChecker): boolean {
  return !hasExportModifier(statement) && (statement.members.length === 0 || localPassiveCarrier(statement, checker));
}

function simpleEnum(statement: ts.EnumDeclaration): boolean {
  return statement.members.every((member) => {
    const initializer = member.initializer;
    return initializer === undefined || ts.isNumericLiteral(initializer) || ts.isStringLiteral(initializer);
  });
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

function depthMeasurement(entry: SourceEntry, score: Record<string, unknown>, policyRevision: string): Measurement {
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
