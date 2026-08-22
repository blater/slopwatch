import ts from "typescript";

import {
  ANALYZER_NAME,
  ANALYZER_VERSION,
  type Measurement,
  type SourceEntry,
  type Subject,
} from "./model.js";
import type { TypedContext } from "./context.js";

interface Candidate {
  component: string;
  node: ts.Node;
  symbol: string;
  kind: string;
  precedence: number;
  attributes: Record<string, unknown>;
}

const PRECEDENCE: Record<string, number> = {
  call: 10,
  member: 20,
  argument: 10,
  assignment: 20,
  declaration: 30,
  return: 10,
  public_return: 20,
  public_parameter: 30,
  public_property: 40,
  non_null: 10,
  assertion: 20,
  explicit_any: 10,
  condition: 10,
  switch: 10,
};

function sourcePosition(
  source: ts.SourceFile,
  offset: number,
): { line: number; column: number; offset: number } {
  const point = source.getLineAndCharacterOfPosition(offset);
  return { line: point.line + 1, column: point.character + 1, offset };
}

function subject(source: ts.SourceFile, node: ts.Node, name: string): Subject {
  const start = node.getStart(source);
  return {
    name,
    start: sourcePosition(source, start),
    end: sourcePosition(source, node.getEnd()),
  };
}

function isAny(type: ts.Type): boolean {
  return (type.flags & ts.TypeFlags.Any) !== 0;
}

function isNullish(type: ts.Type): boolean {
  if ((type.flags & (ts.TypeFlags.Null | ts.TypeFlags.Undefined)) !== 0)
    return true;
  return type.isUnion() && type.types.some(isNullish);
}

function isBooleanOnly(type: ts.Type): boolean {
  const allowed =
    ts.TypeFlags.Boolean | ts.TypeFlags.BooleanLiteral | ts.TypeFlags.Never;
  const parts = type.isUnion() ? type.types : [type];
  return parts.every((part) => (part.flags & allowed) !== 0);
}

function normalizedText(node: ts.Node, source: ts.SourceFile): string {
  return node.getText(source).replace(/\s+/gu, " ").slice(0, 160);
}

function symbolName(
  checker: ts.TypeChecker,
  node: ts.Node,
  source: ts.SourceFile,
): string {
  let target: ts.Node | undefined = node;
  if (ts.isPropertyAccessExpression(node)) target = node.name;
  const symbol = checker.getSymbolAtLocation(target);
  if (symbol !== undefined) {
    let resolved = symbol;
    if ((resolved.flags & ts.SymbolFlags.Alias) !== 0) {
      try {
        resolved = checker.getAliasedSymbol(resolved);
      } catch {
        // An unresolved alias will already make compiler coverage unavailable.
      }
    }
    const declaration = resolved.valueDeclaration ?? resolved.declarations?.[0];
    const suffix =
      declaration === undefined ? "" : `@${declaration.getStart()}`;
    return `${checker.symbolToString(resolved)}${suffix}`;
  }
  return normalizedText(node, source);
}

function enclosingFunction(node: ts.Node): ts.SignatureDeclaration | undefined {
  for (
    let current = node.parent;
    current !== undefined;
    current = current.parent
  ) {
    if (ts.isFunctionLike(current)) return current;
  }
  return undefined;
}

function hasModifier(node: ts.Node, kind: ts.SyntaxKind): boolean {
  return (
    ts.canHaveModifiers(node) &&
    (ts.getModifiers(node)?.some((item) => item.kind === kind) ?? false)
  );
}

function isExported(node: ts.Node): boolean {
  if (
    hasModifier(node, ts.SyntaxKind.ExportKeyword) ||
    hasModifier(node, ts.SyntaxKind.DefaultKeyword)
  )
    return true;
  if (
    ts.isVariableDeclaration(node) &&
    ts.isVariableDeclarationList(node.parent)
  )
    return isExported(node.parent.parent);
  return false;
}

function isPublicMember(node: ts.Node): boolean {
  if (!ts.isClassElement(node) && !ts.isTypeElement(node)) return false;
  if (
    hasModifier(node, ts.SyntaxKind.PrivateKeyword) ||
    hasModifier(node, ts.SyntaxKind.ProtectedKeyword)
  )
    return false;
  const owner = node.parent;
  if (ts.isInterfaceDeclaration(owner)) return isExported(owner);
  if (ts.isClassDeclaration(owner) || ts.isClassExpression(owner))
    return isExported(owner);
  if (ts.isTypeLiteralNode(owner)) {
    for (
      let current: ts.Node | undefined = owner.parent;
      current !== undefined;
      current = current.parent
    ) {
      if (
        ts.isTypeAliasDeclaration(current) ||
        ts.isInterfaceDeclaration(current)
      )
        return isExported(current);
      if (ts.isSourceFile(current) || ts.isFunctionLike(current)) return false;
    }
  }
  return false;
}

function isPublicFunction(node: ts.SignatureDeclaration): boolean {
  return isExported(node) || isPublicMember(node);
}

function parameterType(
  checker: ts.TypeChecker,
  symbol: ts.Symbol,
  at: ts.Node,
): ts.Type {
  const declaration = symbol.valueDeclaration ?? symbol.declarations?.[0];
  if (
    declaration !== undefined &&
    ts.isParameter(declaration) &&
    declaration.dotDotDotToken !== undefined
  ) {
    const rest = checker.getTypeOfSymbolAtLocation(symbol, at);
    const index = checker.getIndexTypeOfType(rest, ts.IndexKind.Number);
    return index ?? rest;
  }
  return checker.getTypeOfSymbolAtLocation(symbol, at);
}

function caseKey(
  checker: ts.TypeChecker,
  expression: ts.Expression,
): string | undefined {
  if (ts.isStringLiteralLike(expression)) return `string:${expression.text}`;
  if (ts.isNumericLiteral(expression)) return `number:${expression.text}`;
  if (expression.kind === ts.SyntaxKind.TrueKeyword) return "boolean:true";
  if (expression.kind === ts.SyntaxKind.FalseKeyword) return "boolean:false";
  if (expression.kind === ts.SyntaxKind.NullKeyword) return "null";
  if (ts.isIdentifier(expression) && expression.text === "undefined")
    return "undefined";
  if (
    ts.isPropertyAccessExpression(expression) ||
    ts.isElementAccessExpression(expression)
  ) {
    const value = checker.getConstantValue(expression);
    if (typeof value === "string") return `string:${value}`;
    if (typeof value === "number") return `number:${value}`;
    const symbol = checker.getSymbolAtLocation(
      ts.isPropertyAccessExpression(expression) ? expression.name : expression,
    );
    if (symbol !== undefined) return `enum:${checker.symbolToString(symbol)}`;
  }
  return undefined;
}

function typeCaseKey(
  checker: ts.TypeChecker,
  type: ts.Type,
): string | undefined {
  if ((type.flags & ts.TypeFlags.StringLiteral) !== 0) {
    return `string:${(type as ts.StringLiteralType).value}`;
  }
  if ((type.flags & ts.TypeFlags.NumberLiteral) !== 0) {
    return `number:${(type as ts.NumberLiteralType).value}`;
  }
  if ((type.flags & ts.TypeFlags.BooleanLiteral) !== 0) {
    return `boolean:${checker.typeToString(type)}`;
  }
  if ((type.flags & ts.TypeFlags.Null) !== 0) return "null";
  if ((type.flags & ts.TypeFlags.Undefined) !== 0) return "undefined";
  if (
    (type.flags & ts.TypeFlags.EnumLiteral) !== 0 &&
    type.symbol !== undefined
  ) {
    return `enum:${checker.symbolToString(type.symbol)}`;
  }
  return undefined;
}

function isOutermostUnsafeMember(
  node: ts.PropertyAccessExpression | ts.ElementAccessExpression,
): boolean {
  const parent = node.parent;
  if (
    (ts.isPropertyAccessExpression(parent) ||
      ts.isElementAccessExpression(parent)) &&
    parent.expression === node
  ) {
    return false;
  }
  if (
    (ts.isCallExpression(parent) || ts.isNewExpression(parent)) &&
    parent.expression === node
  )
    return false;
  return true;
}

type AddCandidate = (
  component: string,
  node: ts.Node,
  kind: string,
  symbolNode?: ts.Node,
  attributes?: Record<string, unknown>,
) => void;

function explicitAnySubject(node: ts.Node): ts.Node {
  for (let parent = node.parent; parent !== undefined; parent = parent.parent) {
    if (
      (ts.isParameter(parent) ||
        ts.isVariableDeclaration(parent) ||
        ts.isPropertyDeclaration(parent)) &&
      parent.name !== undefined
    ) {
      return parent.name;
    }
    if (ts.isFunctionLike(parent) || ts.isSourceFile(parent)) break;
  }
  return node;
}

function collectExplicitAny(node: ts.Node, add: AddCandidate): void {
  if (node.kind === ts.SyntaxKind.AnyKeyword) {
    add("explicit_any", node, "explicit_any", explicitAnySubject(node));
  }
}

function collectUnsafeAssertions(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (ts.isAsExpression(node) || ts.isTypeAssertionExpression(node)) {
    const sourceType = checker.getTypeAtLocation(node.expression);
    const targetType = checker.getTypeAtLocation(node);
    if (
      isAny(sourceType) ||
      isAny(targetType) ||
      !checker.isTypeAssignableTo(sourceType, targetType)
    ) {
      add("unsafe_type_assertion", node, "assertion", node.expression, {
        source_type: checker.typeToString(sourceType),
        target_type: checker.typeToString(targetType),
      });
    }
  } else if (ts.isNonNullExpression(node)) {
    const sourceType = checker.getTypeAtLocation(node.expression);
    if (isAny(sourceType) || isNullish(sourceType)) {
      add("unsafe_type_assertion", node, "non_null", node.expression, {
        source_type: checker.typeToString(sourceType),
      });
    }
  }
}

function collectUnsafePropagation(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (ts.isVariableDeclaration(node) && node.initializer !== undefined) {
    const sourceType = checker.getTypeAtLocation(node.initializer);
    const targetType = checker.getTypeAtLocation(node.name);
    if (isAny(sourceType) && !isAny(targetType)) {
      add("unsafe_type_propagation", node, "declaration", node.name, {
        source_type: checker.typeToString(sourceType),
        target_type: checker.typeToString(targetType),
      });
    }
  } else if (
    ts.isBinaryExpression(node) &&
    node.operatorToken.kind === ts.SyntaxKind.EqualsToken
  ) {
    const sourceType = checker.getTypeAtLocation(node.right);
    const targetType = checker.getTypeAtLocation(node.left);
    if (isAny(sourceType) && !isAny(targetType)) {
      add("unsafe_type_propagation", node, "assignment", node.left, {
        source_type: checker.typeToString(sourceType),
        target_type: checker.typeToString(targetType),
      });
    }
  }
}

function collectUnsafeCalls(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (!ts.isCallExpression(node) && !ts.isNewExpression(node)) return;
  const signature = checker.getResolvedSignature(node);
  if (signature !== undefined) {
    const parameters = signature.getParameters();
    node.arguments?.forEach((argument, index) => {
      const parameter = parameters[Math.min(index, parameters.length - 1)];
      if (parameter === undefined) return;
      const sourceType = checker.getTypeAtLocation(argument);
      const targetType = parameterType(checker, parameter, node);
      if (isAny(sourceType) && !isAny(targetType)) {
        add("unsafe_type_propagation", argument, "argument", argument, {
          parameter: checker.symbolToString(parameter),
          source_type: checker.typeToString(sourceType),
          target_type: checker.typeToString(targetType),
        });
      }
    });
  }
  const calleeType = checker.getTypeAtLocation(node.expression);
  if (isAny(calleeType)) {
    add("unsafe_type_use", node, "call", node.expression, {
      receiver_type: checker.typeToString(calleeType),
    });
  }
}

function collectUnsafeMembers(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (
    !ts.isPropertyAccessExpression(node) &&
    !ts.isElementAccessExpression(node)
  )
    return;
  if (!isOutermostUnsafeMember(node)) return;
  const receiverType = checker.getTypeAtLocation(node.expression);
  if (isAny(receiverType)) {
    add("unsafe_type_use", node, "member", node.expression, {
      receiver_type: checker.typeToString(receiverType),
    });
  }
}

function collectUnsafeReturn(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (!ts.isReturnStatement(node) || node.expression === undefined) return;
  const fn = enclosingFunction(node);
  const signature =
    fn === undefined ? undefined : checker.getSignatureFromDeclaration(fn);
  const sourceType = checker.getTypeAtLocation(node.expression);
  const targetType = signature?.getReturnType();
  if (targetType !== undefined && isAny(sourceType) && !isAny(targetType)) {
    add("unsafe_type_boundary", node, "return", node.expression, {
      source_type: checker.typeToString(sourceType),
      target_type: checker.typeToString(targetType),
    });
  }
}

function collectPublicFunctionBoundary(
  node: ts.SignatureDeclaration,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  const signature = checker.getSignatureFromDeclaration(node);
  if (signature !== undefined && isAny(signature.getReturnType())) {
    add("unsafe_type_boundary", node, "public_return", node.name ?? node, {
      boundary_type: "return",
      type: checker.typeToString(signature.getReturnType()),
    });
  }
  for (const parameter of node.parameters) {
    const parameterType = checker.getTypeAtLocation(parameter.name);
    if (isAny(parameterType)) {
      add(
        "unsafe_type_boundary",
        parameter,
        "public_parameter",
        parameter.name,
        {
          boundary_type: "parameter",
          type: checker.typeToString(parameterType),
        },
      );
    }
  }
}

function collectPublicPropertyBoundary(
  node: ts.PropertyDeclaration | ts.PropertySignature | ts.VariableDeclaration,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (!isPublicMember(node) && !isExported(node)) return;
  const valueType = checker.getTypeAtLocation(node.name);
  if (isAny(valueType)) {
    add("unsafe_type_boundary", node, "public_property", node.name, {
      boundary_type: "property",
      type: checker.typeToString(valueType),
    });
  }
}

function collectPublicBoundary(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (ts.isFunctionLike(node) && isPublicFunction(node)) {
    collectPublicFunctionBoundary(node, checker, add);
    return;
  }
  if (
    (ts.isPropertyDeclaration(node) ||
      ts.isPropertySignature(node) ||
      ts.isVariableDeclaration(node)) &&
    (isPublicMember(node) || isExported(node))
  ) {
    collectPublicPropertyBoundary(node, checker, add);
  }
}

function collectAmbiguousCondition(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  let condition: ts.Expression | undefined;
  if (
    ts.isIfStatement(node) ||
    ts.isWhileStatement(node) ||
    ts.isDoStatement(node)
  )
    condition = node.expression;
  else if (ts.isForStatement(node)) condition = node.condition;
  else if (ts.isConditionalExpression(node)) condition = node.condition;
  if (condition === undefined) return;
  const conditionType = checker.getTypeAtLocation(condition);
  if (!isBooleanOnly(conditionType)) {
    add("ambiguous_boolean_expression", condition, "condition", condition, {
      type: checker.typeToString(conditionType),
    });
  }
}

function collectNonExhaustiveSwitch(
  node: ts.Node,
  checker: ts.TypeChecker,
  add: AddCandidate,
): void {
  if (
    !ts.isSwitchStatement(node) ||
    node.caseBlock.clauses.some(ts.isDefaultClause)
  )
    return;
  const discriminant = checker.getTypeAtLocation(node.expression);
  const parts = discriminant.isUnion() ? discriminant.types : [discriminant];
  const expected = parts.map((part) => typeCaseKey(checker, part));
  if (
    parts.length <= 1 ||
    !expected.every((item): item is string => item !== undefined)
  )
    return;
  const present = new Set(
    node.caseBlock.clauses
      .filter(ts.isCaseClause)
      .map((clause) => caseKey(checker, clause.expression))
      .filter((item): item is string => item !== undefined),
  );
  const missing = (expected as string[])
    .filter((item) => !present.has(item))
    .sort();
  if (missing.length > 0) {
    add("non_exhaustive_union", node.expression, "switch", node.expression, {
      discriminant_type: checker.typeToString(discriminant),
      missing,
    });
  }
}

export function analyzeTypeSafety(
  entry: SourceEntry,
  typed: TypedContext,
  requested: ReadonlySet<string>,
): Measurement[] {
  const source = typed.sourceFiles.get(entry.absolutePath);
  if (source === undefined) return [];
  const checker = typed.checker;
  const candidates: Candidate[] = [];

  const add = (
    component: string,
    node: ts.Node,
    kind: string,
    symbolNode: ts.Node = node,
    attributes: Record<string, unknown> = {},
  ): void => {
    if (!requested.has(component)) return;
    candidates.push({
      component,
      node,
      symbol: symbolName(checker, symbolNode, source),
      kind,
      precedence: PRECEDENCE[kind] ?? 100,
      attributes: { kind, ...attributes },
    });
  };

  const visit = (node: ts.Node): void => {
    collectExplicitAny(node, add);
    collectUnsafeAssertions(node, checker, add);
    collectUnsafePropagation(node, checker, add);
    collectUnsafeCalls(node, checker, add);
    collectUnsafeMembers(node, checker, add);
    collectUnsafeReturn(node, checker, add);
    collectPublicBoundary(node, checker, add);
    collectAmbiguousCondition(node, checker, add);
    collectNonExhaustiveSwitch(node, checker, add);

    ts.forEachChild(node, visit);
  };
  visit(source);

  const deduplicated = new Map<string, Candidate>();
  for (const candidate of candidates) {
    const key = [
      candidate.component,
      entry.relativePath,
      candidate.node.getStart(source),
      candidate.node.getEnd(),
      candidate.symbol,
    ].join("|");
    const previous = deduplicated.get(key);
    if (
      previous === undefined ||
      candidate.precedence < previous.precedence ||
      (candidate.precedence === previous.precedence &&
        candidate.kind.localeCompare(previous.kind) < 0)
    ) {
      deduplicated.set(key, candidate);
    }
  }

  return [...deduplicated.values()]
    .sort(
      (a, b) =>
        a.node.getStart(source) - b.node.getStart(source) ||
        a.component.localeCompare(b.component) ||
        a.symbol.localeCompare(b.symbol),
    )
    .map((candidate) => ({
      unit_id: entry.unitId,
      component_id: candidate.component,
      definition_version: "typescript-local-sink-v1",
      path: entry.relativePath,
      scope: "expression" as const,
      value: 1,
      subject: subject(source, candidate.node, candidate.symbol),
      attributes: {
        ...candidate.attributes,
        normalized_symbol: candidate.symbol,
        deduplication_key:
          "component,canonical-file,start,end,normalized-symbol",
      },
      provenance: {
        analyzer: ANALYZER_NAME,
        analyzer_version: ANALYZER_VERSION,
        rule: `${candidate.component}/typescript-local-sink-v1`,
      },
    }));
}
