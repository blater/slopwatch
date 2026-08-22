import ts from "typescript";
import {
  functionReturnType,
  hasModifier,
  operationFacts,
  publicMember,
  typeComplexity,
  type FunctionNode,
  type PublicOperation,
} from "./surface.js";

export function publicOperations(source: ts.SourceFile): {
  operations: PublicOperation[];
  publicTypes: number;
  exposedState: number;
  exposedRepresentation: number;
} {
  const operations: PublicOperation[] = [];
  let publicTypes = 0;
  let exposedState = 0;
  let exposedRepresentation = 0;
  for (const statement of source.statements) {
    if (!hasModifier(statement, ts.SyntaxKind.ExportKeyword)) continue;
    if (ts.isFunctionDeclaration(statement) && statement.body !== undefined)
      operations.push(operationFacts(statement));
    else if (ts.isVariableStatement(statement))
      handleVariables(
        statement,
        operations,
        (value) => {
          exposedState += value;
        },
        (value) => {
          exposedRepresentation += value;
        },
      );
    else if (ts.isClassDeclaration(statement)) {
      publicTypes++;
      handleClass(
        statement,
        operations,
        (value) => {
          exposedState += value;
        },
        (value) => {
          exposedRepresentation += value;
        },
      );
    } else if (
      ts.isInterfaceDeclaration(statement) ||
      ts.isTypeAliasDeclaration(statement)
    ) {
      publicTypes++;
      if (ts.isInterfaceDeclaration(statement)) {
        exposedState += statement.members.length;
        exposedRepresentation += statement.members.filter(
          (member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword),
        ).length;
      } else exposedState += typeComplexity(statement.type);
    }
  }
  return { operations, publicTypes, exposedState, exposedRepresentation };
}

function handleVariables(
  statement: ts.VariableStatement,
  operations: PublicOperation[],
  addState: (value: number) => void,
  addExposure: (value: number) => void,
): void {
  for (const declaration of statement.declarationList.declarations) {
    if (
      declaration.initializer !== undefined &&
      (ts.isArrowFunction(declaration.initializer) ||
        ts.isFunctionExpression(declaration.initializer))
    )
      operations.push(operationFacts(declaration.initializer as FunctionNode));
    else {
      addState(1);
      if (!(statement.declarationList.flags & ts.NodeFlags.Const))
        addExposure(1);
    }
  }
}

function handleClass(
  statement: ts.ClassDeclaration,
  operations: PublicOperation[],
  addState: (value: number) => void,
  addExposure: (value: number) => void,
): void {
  for (const member of statement.members) {
    if (!publicMember(member)) continue;
    if (
      ts.isMethodDeclaration(member) ||
      ts.isGetAccessorDeclaration(member) ||
      ts.isSetAccessorDeclaration(member)
    ) {
      operations.push(operationFacts(member));
      if (
        (ts.isGetAccessorDeclaration(member) ||
          ts.isSetAccessorDeclaration(member)) &&
        directAccessorExposure(member)
      )
        addExposure(1);
    } else if (ts.isPropertyDeclaration(member)) {
      addState(1);
      if (!hasModifier(member, ts.SyntaxKind.ReadonlyKeyword)) addExposure(1);
    }
  }
}

function directAccessorExposure(
  node: ts.GetAccessorDeclaration | ts.SetAccessorDeclaration,
): boolean {
  if (node.body === undefined) return false;
  let exposed = false;
  const visit = (child: ts.Node): void => {
    if (exposed || (child !== node && ts.isFunctionLike(child))) return;
    if (
      ts.isPropertyAccessExpression(child) &&
      child.expression.kind === ts.SyntaxKind.ThisKeyword
    ) {
      exposed = true;
      return;
    }
    ts.forEachChild(child, visit);
  };
  visit(node.body);
  return exposed;
}
