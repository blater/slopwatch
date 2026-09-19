import { lowerBinaryPrimitive } from "./depth-primitives.js";
import ts from "typescript";
import type { ScalarKind, LocalCallable } from "./depth-model.js";
import { scalarKind, scalarType, sourceKind, constantInstruction, callableSymbol } from "./depth-scalars.js";

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

export function lowerExpression(
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
    return lowerBinaryPrimitive(left, right, expression.operatorToken.getText(), kind, type, next, instructions);
  }
  if (ts.isCallExpression(expression) && ts.isIdentifier(expression.expression)) {
    return lowerCall(checker, expression, expression.expression, values, next, instructions, helpers);
  }
  return undefined;
}

function lowerCall(
  checker: ts.TypeChecker,
  expression: ts.CallExpression,
  callee: ts.Identifier,
  values: Map<ts.Symbol, string>,
  next: { value: number },
  instructions: Record<string, unknown>[],
  helpers: Map<ts.Symbol, LocalCallable>,
): { id: string; kind: ScalarKind } | undefined {
  const symbol = checker.getSymbolAtLocation(callee);
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
