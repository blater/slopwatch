import ts from "typescript";

export interface PassiveCarrierProof {
  fields: number;
  getters: number;
  mutators: number;
  constructors: number;
  exposedFields: number;
}

export interface ExportedPassiveCarrier {
  node: ts.ClassDeclaration;
  proof: PassiveCarrierProof | undefined;
}

export function exportedPassiveCarriers(
  source: ts.SourceFile,
  checker: ts.TypeChecker,
): ExportedPassiveCarrier[] {
  const classes = source.statements.filter(ts.isClassDeclaration);
  const exported = new Set<ts.ClassDeclaration>();
  for (const declaration of classes) {
    if (hasExportModifier(declaration)) exported.add(declaration);
  }
  for (const statement of source.statements) {
    if (ts.isExportDeclaration(statement) && statement.exportClause !== undefined && ts.isNamedExports(statement.exportClause)) {
      for (const specifier of statement.exportClause.elements) {
        const symbol = resolveSymbol(checker, checker.getSymbolAtLocation(specifier.propertyName ?? specifier.name));
        addClassDeclaration(symbol, classes, exported);
      }
    }
    if (ts.isExportAssignment(statement) && ts.isIdentifier(statement.expression)) {
      const symbol = resolveSymbol(checker, checker.getSymbolAtLocation(statement.expression));
      addClassDeclaration(symbol, classes, exported);
    }
  }
  return [...exported].map((node) => ({ node, proof: inspectCarrier(node, checker) }));
}

export function passiveCarrierEvidence(
  source: ts.SourceFile,
  proof: PassiveCarrierProof,
): Record<string, unknown> {
  const name = proof.fields === 0 ? "carrier" : source.fileName;
  return {
    id: `${name}/passive-result-carrier`,
    kind: proof.exposedFields > 0 ? "passive-value-object-v1" : "passive-result-carrier-v1",
    status: "proven",
    details: {
      instance_fields: proof.fields,
      direct_getters: proof.getters,
      direct_mutators: proof.mutators,
      constructors: proof.constructors,
      exposed_fields: proof.exposedFields,
    },
  };
}

type CarrierField = ts.PropertyDeclaration | ts.ParameterDeclaration;

function inspectCarrier(node: ts.ClassDeclaration, checker: ts.TypeChecker): PassiveCarrierProof | undefined {
  if (node.heritageClauses !== undefined || hasDecorators(node)) return undefined;
  const fields = new Map<ts.Symbol, CarrierField>();
  for (const member of node.members) {
    if (!ts.isPropertyDeclaration(member)) continue;
    if (hasDecorators(member)) return undefined;
    if (hasModifier(member, ts.SyntaxKind.StaticKeyword)
      || ts.isComputedPropertyName(member.name)
      || member.initializer !== undefined && !allowedValue(member.initializer, undefined, checker, symbolFor(member, checker))) return undefined;
    const symbol = checker.getSymbolAtLocation(member.name);
    if (symbol === undefined) return undefined;
    fields.set(symbol, member);
  }
  if (!parameterProperties(node, checker, fields)) return undefined;
  if (fields.size === 0) return undefined;
  const getters = new Set<ts.Symbol>();
  for (const [symbol, field] of fields) {
    if (!hasModifier(field, ts.SyntaxKind.PrivateKeyword) && !hasModifier(field, ts.SyntaxKind.ProtectedKeyword) && !ts.isPrivateIdentifier(field.name)) getters.add(symbol);
  }
  const exposedFields = getters.size;
  let mutators = 0;
  let constructors = 0;
  for (const member of node.members) {
    if (ts.isPropertyDeclaration(member)) continue;
    if (hasDecorators(member) || hasModifier(member, ts.SyntaxKind.StaticKeyword) || member.name !== undefined && ts.isComputedPropertyName(member.name)) return undefined;
    if (ts.isConstructorDeclaration(member)) {
      if (member.body === undefined || !directAssignments(member.body, member.parameters, fields, checker, true)) return undefined;
      constructors++;
      continue;
    }
    if (ts.isMethodDeclaration(member) || ts.isGetAccessorDeclaration(member)) {
      if (isGetter(member, fields, checker)) {
        const field = returnedField(member, fields, checker);
        if (field !== undefined && !hasModifier(member, ts.SyntaxKind.PrivateKeyword)
          && !hasModifier(member, ts.SyntaxKind.ProtectedKeyword)) getters.add(field);
      } else if (isVoid(member, checker) && member.body !== undefined
        && directAssignments(member.body, member.parameters, fields, checker, false)) {
        mutators++;
      } else return undefined;
      continue;
    }
    if (ts.isSetAccessorDeclaration(member)) {
      if (member.body === undefined || !directAssignments(member.body, member.parameters, fields, checker, false)) return undefined;
      mutators++;
      continue;
    }
    return undefined;
  }
  if (getters.size !== fields.size) return undefined;
  return { fields: fields.size, getters: getters.size - exposedFields, mutators, constructors, exposedFields };
}

function parameterProperties(node: ts.ClassDeclaration, checker: ts.TypeChecker,
                            fields: Map<ts.Symbol, CarrierField>): boolean {
  for (const member of node.members) {
    if (!ts.isConstructorDeclaration(member)) continue;
    for (const parameter of member.parameters) {
      if (!parameterProperty(parameter)) continue;
      if (!ts.isIdentifier(parameter.name) || parameter.initializer !== undefined
        || parameter.questionToken !== undefined || parameter.dotDotDotToken !== undefined
        || hasDecorators(parameter)) return false;
      const symbol = checker.getSymbolAtLocation(parameter.name);
      if (symbol === undefined) return false;
      fields.set(symbol, parameter);
    }
  }
  return true;
}

function parameterProperty(parameter: ts.ParameterDeclaration): boolean {
  return ts.canHaveModifiers(parameter) && (ts.getModifiers(parameter)?.some((item) =>
    item.kind === ts.SyntaxKind.PublicKeyword || item.kind === ts.SyntaxKind.ProtectedKeyword
    || item.kind === ts.SyntaxKind.PrivateKeyword || item.kind === ts.SyntaxKind.ReadonlyKeyword) ?? false);
}

function symbolFor(member: ts.PropertyDeclaration, checker: ts.TypeChecker): ts.Symbol | undefined {
  return checker.getSymbolAtLocation(member.name);
}

function directAssignments(
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

function isGetter(
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

function returnedField(
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

function allowedValue(
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

function isVoid(member: ts.MethodDeclaration | ts.GetAccessorDeclaration, checker: ts.TypeChecker): boolean {
  const signature = checker.getSignatureFromDeclaration(member);
  return signature !== undefined && (checker.getReturnTypeOfSignature(signature).flags & ts.TypeFlags.Void) !== 0;
}

function sameType(checker: ts.TypeChecker, left: ts.Type, right: ts.Type): boolean {
  return checker.typeToString(left) === checker.typeToString(right);
}

function hasExportModifier(node: ts.Node): boolean {
  return ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

function hasModifier(node: ts.Node, kind: ts.SyntaxKind): boolean {
  return ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((item) => item.kind === kind) === true;
}

function hasDecorators(node: ts.Node): boolean {
  return ts.canHaveDecorators(node) && (ts.getDecorators(node)?.length ?? 0) > 0;
}

function resolveSymbol(checker: ts.TypeChecker, symbol: ts.Symbol | undefined): ts.Symbol | undefined {
  return symbol !== undefined && (symbol.flags & ts.SymbolFlags.Alias) !== 0 ? checker.getAliasedSymbol(symbol) : symbol;
}

function addClassDeclaration(symbol: ts.Symbol | undefined, classes: readonly ts.ClassDeclaration[], output: Set<ts.ClassDeclaration>): void {
  for (const declaration of symbol?.declarations ?? []) {
    if (ts.isClassDeclaration(declaration) && classes.includes(declaration)) output.add(declaration);
  }
}

// Local data classes are supporting declarations, even in files exporting work.
export function localPassiveCarrier(node: ts.ClassDeclaration, checker: ts.TypeChecker): boolean {
  return !hasExportModifier(node) && inspectCarrier(node, checker) !== undefined;
}
