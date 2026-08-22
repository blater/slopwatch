import ts from "typescript";
import type { Subject, SourceEntry, Measurement } from "./model.js";

export type FunctionNode =
  | ts.FunctionDeclaration
  | ts.MethodDeclaration
  | ts.ConstructorDeclaration
  | ts.GetAccessorDeclaration
  | ts.SetAccessorDeclaration
  | ts.FunctionExpression
  | ts.ArrowFunction;
export interface FunctionFact {
  node: FunctionNode;
  name: string;
  subject: Subject;
}
export interface PublicOperation {
  node: FunctionNode;
  typeCost: number;
  representationCost: number;
}
export type TypeDeclaration = ts.ClassDeclaration | ts.InterfaceDeclaration;

function isFunctionNode(node: ts.Node): node is FunctionNode {
  return (
    ts.isFunctionDeclaration(node) ||
    ts.isMethodDeclaration(node) ||
    ts.isConstructorDeclaration(node) ||
    ts.isGetAccessorDeclaration(node) ||
    ts.isSetAccessorDeclaration(node) ||
    ts.isFunctionExpression(node) ||
    ts.isArrowFunction(node)
  );
}
function functionName(node: FunctionNode, source: ts.SourceFile): string {
  if ("name" in node && node.name !== undefined)
    return node.name.getText(source);
  const parent = node.parent;
  if (ts.isVariableDeclaration(parent) && ts.isIdentifier(parent.name))
    return parent.name.text;
  if (ts.isPropertyAssignment(parent)) return parent.name.getText(source);
  const point = source.getLineAndCharacterOfPosition(node.getStart(source));
  return `<anonymous@${point.line + 1}:${point.character + 1}>`;
}
function position(source: ts.SourceFile, node: ts.Node): Subject {
  const start = source.getLineAndCharacterOfPosition(node.getStart(source));
  const end = source.getLineAndCharacterOfPosition(node.getEnd());
  return {
    name: functionName(node as FunctionNode, source),
    start: {
      line: start.line + 1,
      column: start.character + 1,
      offset: node.getStart(source),
    },
    end: {
      line: end.line + 1,
      column: end.character + 1,
      offset: node.getEnd(),
    },
  };
}
export function hasModifier(node: ts.Node, kind: ts.SyntaxKind): boolean {
  return (
    ts.canHaveModifiers(node) &&
    ts.getModifiers(node)?.some((modifier) => modifier.kind === kind) === true
  );
}
export function publicMember(node: ts.Node): boolean {
  return (
    !hasModifier(node, ts.SyntaxKind.PrivateKeyword) &&
    !hasModifier(node, ts.SyntaxKind.ProtectedKeyword)
  );
}
export function functionReturnType(
  node: FunctionNode,
): ts.TypeNode | undefined {
  return "type" in node ? node.type : undefined;
}
export function typeComplexity(
  node: ts.TypeNode | undefined,
  depth = 0,
): number {
  if (node === undefined || depth > 32) return 1;
  let total = 1;
  if (ts.isArrayTypeNode(node))
    total += typeComplexity(node.elementType, depth + 1);
  else if (ts.isTupleTypeNode(node))
    total += node.elements.reduce(
      (sum, child) => sum + typeComplexity(child, depth + 1),
      0,
    );
  else if (ts.isUnionTypeNode(node) || ts.isIntersectionTypeNode(node))
    total += node.types.reduce(
      (sum, child) => sum + typeComplexity(child, depth + 1),
      0,
    );
  else if (ts.isTypeReferenceNode(node) && node.typeArguments !== undefined)
    total += node.typeArguments.reduce(
      (sum, child) => sum + typeComplexity(child, depth + 1),
      0,
    );
  else if (ts.isTypeLiteralNode(node)) total += node.members.length;
  else if (ts.isFunctionTypeNode(node))
    total +=
      node.parameters.reduce(
        (sum, parameter) => sum + typeComplexity(parameter.type, depth + 1),
        0,
      ) + typeComplexity(node.type, depth + 1);
  else if (ts.isParenthesizedTypeNode(node))
    total += typeComplexity(node.type, depth + 1);
  return Math.min(32, total);
}
export function returnsValue(node: FunctionNode): boolean {
  const returnType = functionReturnType(node);
  if (
    returnType !== undefined &&
    returnType.kind !== ts.SyntaxKind.VoidKeyword &&
    returnType.kind !== ts.SyntaxKind.UndefinedKeyword
  )
    return true;
  let result = false;
  const visit = (child: ts.Node): void => {
    if (child !== node && isFunctionNode(child)) return;
    if (ts.isReturnStatement(child) && child.expression !== undefined)
      result = true;
    ts.forEachChild(child, visit);
  };
  if (node.body !== undefined) visit(node.body);
  return result;
}
export function typeRepresentationCost(
  node: ts.TypeNode | undefined,
  depth = 0,
): number {
  if (node === undefined || depth > 32) return 0;
  if (ts.isTypeLiteralNode(node))
    return (
      node.members.filter(
        (member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword),
      ).length +
      node.members.reduce(
        (sum, member) =>
          ts.isPropertySignature(member)
            ? sum + typeRepresentationCost(member.type, depth + 1)
            : sum,
        0,
      )
    );
  if (ts.isArrayTypeNode(node))
    return typeRepresentationCost(node.elementType, depth + 1);
  return 0;
}
export function operationFacts(node: FunctionNode): PublicOperation {
  const resultCost = returnsValue(node)
    ? typeComplexity(functionReturnType(node))
    : 0;
  return {
    node,
    typeCost:
      node.parameters.reduce(
        (sum, parameter) => sum + typeComplexity(parameter.type),
        0,
      ) + resultCost,
    representationCost:
      node.parameters.reduce(
        (sum, parameter) => sum + typeRepresentationCost(parameter.type),
        0,
      ) +
      (returnsValue(node)
        ? typeRepresentationCost(functionReturnType(node))
        : 0),
  };
}
export function isTypeDeclaration(node: ts.Node): node is TypeDeclaration {
  return ts.isClassDeclaration(node) || ts.isInterfaceDeclaration(node);
}
export function methodLike(node: ts.ClassElement | ts.TypeElement): boolean {
  return (
    ts.isMethodDeclaration(node) ||
    ts.isConstructorDeclaration(node) ||
    ts.isGetAccessorDeclaration(node) ||
    ts.isSetAccessorDeclaration(node) ||
    ts.isMethodSignature(node) ||
    ts.isCallSignatureDeclaration(node) ||
    ts.isConstructSignatureDeclaration(node)
  );
}
export function typeFunctionFact(
  node: FunctionNode,
  source: ts.SourceFile,
): FunctionFact {
  const name = functionName(node, source);
  return { node, name, subject: position(source, node) };
}
export function typeMembers(
  item: TypeDeclaration,
  source: ts.SourceFile,
): FunctionFact[] {
  const output: FunctionFact[] = [];
  for (const member of item.members) {
    if (
      !methodLike(member) ||
      !isFunctionNode(member) ||
      member.body === undefined
    )
      continue;
    output.push(typeFunctionFact(member, source));
  }
  return output;
}
export function typeMemberCount(item: TypeDeclaration): number {
  return item.members.filter((member) => methodLike(member)).length;
}
export function typeReferences(node: ts.Node): string[] {
  const output = new Set<string>();
  const visit = (current: ts.Node): void => {
    if (ts.isTypeReferenceNode(current)) output.add(current.typeName.getText());
    ts.forEachChild(current, visit);
  };
  visit(node);
  return [...output].sort();
}
export function fieldsUsedByMethod(fact: FunctionFact): Set<string> {
  return propertyNames(fact, true);
}
export function foreignFieldsUsedByMethod(fact: FunctionFact): Set<string> {
  return propertyNames(fact, false);
}
function propertyNames(fact: FunctionFact, own: boolean): Set<string> {
  const output = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (node !== fact.node && isFunctionNode(node)) return;
    if (
      ts.isPropertyAccessExpression(node) &&
      (node.expression.kind === ts.SyntaxKind.ThisKeyword) === own
    )
      output.add(node.name.text);
    ts.forEachChild(node, visit);
  };
  if (fact.node.body !== undefined) visit(fact.node.body);
  return output;
}
export function uniqueStrings(values: Iterable<string>): string[] {
  return [...new Set(values)].sort();
}
/* export function publicOperations(source: ts.SourceFile): { operations: PublicOperation[]; publicTypes: number; exposedState: number; exposedRepresentation: number } {
  const operations: PublicOperation[] = []; let publicTypes = 0; let exposedState = 0; let exposedRepresentation = 0;
  for (const statement of source.statements) {
    if (!hasModifier(statement, ts.SyntaxKind.ExportKeyword)) continue;
    if (ts.isFunctionDeclaration(statement) && statement.body !== undefined) operations.push(operationFacts(statement));
    else if (ts.isVariableStatement(statement)) for (const declaration of statement.declarationList.declarations) { if (declaration.initializer !== undefined && (ts.isArrowFunction(declaration.initializer) || ts.isFunctionExpression(declaration.initializer))) operations.push(operationFacts(declaration.initializer)); else { exposedState++; if (!(statement.declarationList.flags & ts.NodeFlags.Const)) exposedRepresentation++; } }
    else if (ts.isClassDeclaration(statement)) { publicTypes++; for (const member of statement.members) { if (!publicMember(member)) continue; if (ts.isMethodDeclaration(member) || ts.isGetAccessorDeclaration(member) || ts.isSetAccessorDeclaration(member)) { operations.push(operationFacts(member)); if ((ts.isGetAccessorDeclaration(member) || ts.isSetAccessorDeclaration(member)) && directAccessorExposure(member)) exposedRepresentation++; } else if (ts.isPropertyDeclaration(member)) { exposedState++; if (!hasModifier(member, ts.SyntaxKind.ReadonlyKeyword)) exposedRepresentation++; } } }
    else if (ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement)) { publicTypes++; if (ts.isInterfaceDeclaration(statement)) { exposedState += statement.members.length; exposedRepresentation += statement.members.filter((member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword)).length; } else exposedState += typeComplexity(statement.type); }
  }
  return { operations, publicTypes, exposedState, exposedRepresentation };
}
function directAccessorExposure(node: ts.GetAccessorDeclaration | ts.SetAccessorDeclaration): boolean { if (node.body === undefined) return false; let exposed = false; const visit = (child: ts.Node): void => { if (exposed || (child !== node && isFunctionNode(child))) return; if (ts.isPropertyAccessExpression(child) && child.expression.kind === ts.SyntaxKind.ThisKeyword) { exposed = true; return; } ts.forEachChild(child, visit); }; visit(node.body); return exposed; } */
