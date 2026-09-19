import ts from "typescript";
import { hasDecorators } from "./carrier-syntax.js";

export type CarrierField = ts.PropertyDeclaration | ts.ParameterDeclaration;

export function directAssignments(
  body: ts.Block,
  parameters: readonly ts.ParameterDeclaration[],
  fields: Map<ts.Symbol, CarrierField>,
  checker: ts.TypeChecker,
  constructor: boolean,
): boolean {
  const parameterSymbols = new Set<ts.Symbol>();
  for (const parameter of parameters) {
    if (hasDecorators(parameter) || !ts.isIdentifier(parameter.name) || parameter.initializer !== undefined || parameter.questionToken !== undefined || parameter.dotDotDotToken !== undefined) return false;
    const symbol = checker.getSymbolAtLocation(parameter.name);
    if (symbol === undefined) return false;
    parameterSymbols.add(symbol);
  }
  if (body.statements.length === 0) return constructor;
  for (const statement of body.statements) {
    if (!ts.isExpressionStatement(statement) || !ts.isBinaryExpression(statement.expression)
      || statement.expression.operatorToken.kind !== ts.SyntaxKind.EqualsToken) return false;
    const field = directField(statement.expression.left, fields, checker);
    if (field === undefined) return false;
    if (!allowedValue(statement.expression.right, parameterSymbols, checker, field)) return false;
  }
  return true;
}

export function isGetter(
  member: ts.MethodDeclaration | ts.GetAccessorDeclaration,
  fields: Map<ts.Symbol, CarrierField>,
  checker: ts.TypeChecker,
): boolean {
  if (member.parameters.length !== 0 || member.body === undefined || member.body.statements.length !== 1) return false;
  const statement = member.body.statements[0];
  if (statement === undefined) return false;
  return ts.isReturnStatement(statement) && statement.expression !== undefined
    && returnedField(member, fields, checker) !== undefined;
}

export function returnedField(
  member: ts.MethodDeclaration | ts.GetAccessorDeclaration,
  fields: Map<ts.Symbol, CarrierField>,
  checker: ts.TypeChecker,
): ts.Symbol | undefined {
  if (member.body === undefined || member.body.statements.length !== 1) return undefined;
  const statement = member.body.statements[0];
  if (statement === undefined) return undefined;
  if (!ts.isReturnStatement(statement) || statement.expression === undefined) return undefined;
  const field = directField(statement.expression, fields, checker);
  if (field === undefined) return undefined;
  const declaration = fields.get(field);
  if (declaration === undefined) return undefined;
  const fieldType = checker.getTypeAtLocation(declaration);
  const signature = checker.getSignatureFromDeclaration(member);
  return signature !== undefined && sameType(checker, fieldType, checker.getReturnTypeOfSignature(signature))
    ? field : undefined;
}

function directField(
  expression: ts.Expression,
  fields: Map<ts.Symbol, CarrierField>,
  checker: ts.TypeChecker,
): ts.Symbol | undefined {
  let symbol: ts.Symbol | undefined;
  if (ts.isIdentifier(expression)) symbol = checker.getSymbolAtLocation(expression);
  else if (ts.isPropertyAccessExpression(expression) && expression.expression.kind === ts.SyntaxKind.ThisKeyword) {
    symbol = checker.getSymbolAtLocation(expression.name);
  }
  return symbol !== undefined && fields.has(symbol) ? symbol : undefined;
}

export function allowedValue(
  expression: ts.Expression,
  parameters: Set<ts.Symbol> | undefined,
  checker: ts.TypeChecker,
  field?: ts.Symbol,
): boolean {
  const target = field === undefined ? undefined : checker.getTypeOfSymbolAtLocation(field, expression);
  if (parameters !== undefined && ts.isIdentifier(expression)) {
    const symbol = checker.getSymbolAtLocation(expression);
    if (symbol !== undefined && parameters.has(symbol) && field !== undefined) {
      const actual = checker.getTypeAtLocation(expression);
      return target !== undefined && sameType(checker, actual, target);
    }
  }
  if (expression.kind === ts.SyntaxKind.NullKeyword) {
    const primitive = ts.TypeFlags.NumberLike | ts.TypeFlags.BooleanLike | ts.TypeFlags.StringLike
      | ts.TypeFlags.BigIntLike | ts.TypeFlags.ESSymbolLike | ts.TypeFlags.Void | ts.TypeFlags.Undefined;
    return target !== undefined && (target.flags & primitive) === 0;
  }
  if (ts.isStringLiteral(expression) || ts.isNumericLiteral(expression) || ts.isBigIntLiteral(expression)
    || ts.isNoSubstitutionTemplateLiteral(expression) || expression.kind === ts.SyntaxKind.TrueKeyword
    || expression.kind === ts.SyntaxKind.FalseKeyword) {
    return target !== undefined && checker.isTypeAssignableTo(checker.getTypeAtLocation(expression), target);
  }
  const symbol = ts.isIdentifier(expression) || ts.isPropertyAccessExpression(expression)
    ? checker.getSymbolAtLocation(ts.isIdentifier(expression) ? expression : expression.name) : undefined;
  if (symbol === undefined || (symbol.flags & ts.SymbolFlags.EnumMember) === 0 || target === undefined) return false;
  return checker.isTypeAssignableTo(checker.getTypeAtLocation(expression), target);
}

export function isVoid(member: ts.MethodDeclaration | ts.GetAccessorDeclaration, checker: ts.TypeChecker): boolean {
  const signature = checker.getSignatureFromDeclaration(member);
  return signature !== undefined && (checker.getReturnTypeOfSignature(signature).flags & ts.TypeFlags.Void) !== 0;
}

function sameType(checker: ts.TypeChecker, left: ts.Type, right: ts.Type): boolean {
  return checker.typeToString(left) === checker.typeToString(right);
}
