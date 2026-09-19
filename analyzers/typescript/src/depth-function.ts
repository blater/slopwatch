import ts from "typescript";
import type { SourceEntry } from "./model.js";
import type { ScalarKind, DepthFlowFunction, FunctionLike, LocalCallable } from "./depth-model.js";
import { scalarKind, scalarType, sourceKind, functionID } from "./depth-scalars.js";
import { lowerExpression } from "./depth-expression.js";
import { lowerConditionalReturnFunction, lowerBranchFunction } from "./depth-branches.js";

export function lowerFunction(entry: SourceEntry, checker: ts.TypeChecker, node: FunctionLike,
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
