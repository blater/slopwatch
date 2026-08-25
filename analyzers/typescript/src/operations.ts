import ts from "typescript";

import {
  hasModifier,
  operationFacts,
  publicMember,
  typeComplexity,
  type FunctionNode,
  type PublicOperation,
} from "./surface.js";
import {
  isContractMember,
  isFunctionInitializer,
  isOperationMember,
} from "./syntax_helpers.js";

export { classifyInterfaceRole } from "./roles.js";
export type { InterfaceRole, InterfaceRoleClassification } from "./roles.js";

export interface PublicSurface {
  operations: PublicOperation[];
  publicTypes: number;
  exposedState: number;
  exposedRepresentation: number;
}

export function publicOperations(source: ts.SourceFile): PublicSurface {
  const surface = emptySurface();
  for (const statement of source.statements) {
    if (hasModifier(statement, ts.SyntaxKind.ExportKeyword))
      collectExportedStatement(statement, surface);
  }
  return surface;
}

function emptySurface(): PublicSurface {
  return {
    operations: [],
    publicTypes: 0,
    exposedState: 0,
    exposedRepresentation: 0,
  };
}

function collectExportedStatement(
  statement: ts.Statement,
  surface: PublicSurface,
): void {
  if (ts.isFunctionDeclaration(statement) && statement.body !== undefined) {
    surface.operations.push(operationFacts(statement));
    return;
  }
  if (ts.isVariableStatement(statement)) {
    collectVariables(statement, surface);
    return;
  }
  if (ts.isClassDeclaration(statement)) {
    surface.publicTypes++;
    collectClass(statement, surface);
    return;
  }
  if (ts.isInterfaceDeclaration(statement)) {
    surface.publicTypes++;
    collectInterface(statement, surface);
    return;
  }
  if (ts.isTypeAliasDeclaration(statement)) {
    surface.publicTypes++;
    surface.exposedState += typeComplexity(statement.type);
  }
}

function collectVariables(
  statement: ts.VariableStatement,
  surface: PublicSurface,
): void {
  for (const declaration of statement.declarationList.declarations) {
    if (isFunctionInitializer(declaration.initializer)) {
      surface.operations.push(
        operationFacts(declaration.initializer as FunctionNode),
      );
      continue;
    }
    surface.exposedState++;
    if (!(statement.declarationList.flags & ts.NodeFlags.Const))
      surface.exposedRepresentation++;
  }
}

function collectClass(
  statement: ts.ClassDeclaration,
  surface: PublicSurface,
): void {
  for (const member of statement.members) {
    if (!publicMember(member)) continue;
    if (isOperationMember(member)) {
      surface.operations.push(operationFacts(member));
      if (hasDirectAccessorExposure(member)) surface.exposedRepresentation++;
      continue;
    }
    if (ts.isPropertyDeclaration(member)) {
      surface.exposedState++;
      if (!hasModifier(member, ts.SyntaxKind.ReadonlyKeyword))
        surface.exposedRepresentation++;
    }
  }
}

function collectInterface(
  statement: ts.InterfaceDeclaration,
  surface: PublicSurface,
): void {
  for (const member of statement.members) {
    if (isContractMember(member))
      surface.operations.push(operationFacts(member as FunctionNode));
  }
  surface.exposedState += statement.members.length;
  surface.exposedRepresentation += statement.members.filter(
    (member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword),
  ).length;
}

function hasDirectAccessorExposure(
  node: ts.MethodDeclaration | ts.GetAccessorDeclaration | ts.SetAccessorDeclaration,
): boolean {
  if (!ts.isGetAccessorDeclaration(node) && !ts.isSetAccessorDeclaration(node))
    return false;
  return node.body !== undefined && containsThisPropertyAccess(node.body);
}

function containsThisPropertyAccess(node: ts.Node): boolean {
  let found = false;
  const visit = (child: ts.Node): void => {
    if (found || (child !== node && ts.isFunctionLike(child))) return;
    if (
      ts.isPropertyAccessExpression(child) &&
      child.expression.kind === ts.SyntaxKind.ThisKeyword
    ) {
      found = true;
      return;
    }
    ts.forEachChild(child, visit);
  };
  visit(node);
  return found;
}
