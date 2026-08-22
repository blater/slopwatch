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
        for (const member of statement.members) {
          if (isContractMember(member))
            operations.push(operationFacts(member as FunctionNode));
        }
        exposedState += statement.members.length;
        exposedRepresentation += statement.members.filter(
          (member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword),
        ).length;
      } else exposedState += typeComplexity(statement.type);
    }
  }
  return { operations, publicTypes, exposedState, exposedRepresentation };
}

export type InterfaceRole =
  "protocol" | "command" | "query" | "data" | "general" | "unknown";

export interface InterfaceRoleClassification {
  role: InterfaceRole;
  confidence: number;
  basis: string;
}

interface RoleFacts {
  operations: number;
  inputOperations: number;
  valueOperations: number;
  statusOperations: number;
  contractOperations: number;
  mutationOperations: number;
  stateMembers: number;
  publicEvidence: boolean;
}

/** Classify the exported syntax surface without using names or comments. */
export function classifyInterfaceRole(
  source: ts.SourceFile,
): InterfaceRoleClassification {
  const facts = collectRoleFacts(source);
  if (!facts.publicEvidence)
    return {
      role: "unknown",
      confidence: 0,
      basis: "insufficient-or-contradictory-evidence",
    };
  if (facts.contractOperations > 0)
    return {
      role: "protocol",
      confidence: 1,
      basis: "public-contract-operations",
    };
  if (facts.operations === 0 && facts.stateMembers > 0)
    return { role: "data", confidence: 1, basis: "public-state-surface" };
  const inputRatio = facts.inputOperations / facts.operations;
  const statusRatio = facts.statusOperations / facts.operations;
  const valueRatio = facts.valueOperations / facts.operations;
  if (facts.operations > 0 && inputRatio >= 0.5 && statusRatio === 1)
    return {
      role: "command",
      confidence: 0.9,
      basis: "operation-result-shape",
    };
  if (
    facts.operations > 0 &&
    valueRatio >= 2 / 3 &&
    facts.mutationOperations <= Math.max(1, Math.floor(facts.operations / 3))
  )
    return {
      role: "query",
      confidence: 0.85,
      basis: "operation-result-shape",
    };
  if (facts.operations > 0)
    return {
      role: "general",
      confidence: 0.6,
      basis: "mixed-operation-shape",
    };
  return {
    role: "unknown",
    confidence: 0,
    basis: "insufficient-or-contradictory-evidence",
  };
}

function collectRoleFacts(source: ts.SourceFile): RoleFacts {
  const facts: RoleFacts = {
    operations: 0,
    inputOperations: 0,
    valueOperations: 0,
    statusOperations: 0,
    contractOperations: 0,
    mutationOperations: 0,
    stateMembers: 0,
    publicEvidence: false,
  };
  for (const statement of source.statements) {
    if (!hasModifier(statement, ts.SyntaxKind.ExportKeyword)) continue;
    facts.publicEvidence = true;
    if (ts.isFunctionDeclaration(statement) && statement.body !== undefined) {
      addRoleOperation(facts, statement, false);
    } else if (ts.isVariableStatement(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        const initializer = declaration.initializer;
        if (
          initializer !== undefined &&
          (ts.isArrowFunction(initializer) ||
            ts.isFunctionExpression(initializer))
        )
          addRoleOperation(facts, initializer, false);
        else facts.stateMembers++;
      }
    } else if (ts.isClassDeclaration(statement)) {
      for (const member of statement.members) {
        if (!publicMember(member)) continue;
        if (
          ts.isMethodDeclaration(member) ||
          ts.isGetAccessorDeclaration(member) ||
          ts.isSetAccessorDeclaration(member)
        ) {
          addRoleOperation(
            facts,
            member,
            hasModifier(member, ts.SyntaxKind.AbstractKeyword),
          );
          if (writesThis(member)) facts.mutationOperations++;
        } else if (ts.isPropertyDeclaration(member)) facts.stateMembers++;
      }
    } else if (ts.isInterfaceDeclaration(statement)) {
      for (const member of statement.members) {
        if (isContractMember(member)) {
          addRoleOperation(facts, member as ts.SignatureDeclaration, true);
        } else if (ts.isPropertySignature(member)) facts.stateMembers++;
      }
    } else if (ts.isTypeAliasDeclaration(statement)) facts.stateMembers++;
  }
  return facts;
}

function isContractMember(member: ts.TypeElement): boolean {
  return (
    ts.isMethodSignature(member) ||
    ts.isCallSignatureDeclaration(member) ||
    ts.isConstructSignatureDeclaration(member)
  );
}

function addRoleOperation(
  facts: RoleFacts,
  node: FunctionNode | ts.SignatureDeclaration,
  contract: boolean,
): void {
  facts.operations++;
  if (node.parameters.length > 0) facts.inputOperations++;
  const returnType = "type" in node ? node.type : undefined;
  if (
    returnType === undefined ||
    returnType.kind === ts.SyntaxKind.VoidKeyword ||
    returnType.kind === ts.SyntaxKind.UndefinedKeyword ||
    returnType.kind === ts.SyntaxKind.BooleanKeyword
  )
    facts.statusOperations++;
  else facts.valueOperations++;
  if (contract) facts.contractOperations++;
}

function writesThis(node: ts.Node): boolean {
  let result = false;
  const visit = (child: ts.Node): void => {
    if (result || (child !== node && ts.isFunctionLike(child))) return;
    if (
      ts.isBinaryExpression(child) &&
      ts.isPropertyAccessExpression(child.left) &&
      child.left.expression.kind === ts.SyntaxKind.ThisKeyword
    )
      result = true;
    ts.forEachChild(child, visit);
  };
  const body = "body" in node ? (node as { body?: ts.Node }).body : undefined;
  if (body != null) visit(body);
  return result;
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
