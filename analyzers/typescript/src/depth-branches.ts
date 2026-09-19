import ts from "typescript";
import type { SourceEntry } from "./model.js";
import type { ScalarKind, DepthFlowFunction, FunctionLike, LocalCallable } from "./depth-model.js";
import { scalarType, constantInstruction } from "./depth-scalars.js";
import { lowerExpression } from "./depth-expression.js";

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

export function lowerConditionalReturnFunction(
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

export function lowerBranchFunction(
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
