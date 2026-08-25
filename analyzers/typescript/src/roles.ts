import ts from "typescript";

import {
  hasModifier,
  publicMember,
  type FunctionNode,
} from "./surface.js";
import {
  containsThisAssignment,
  isContractMember,
  isFunctionInitializer,
  isOperationMember,
} from "./syntax_helpers.js";

export type InterfaceRole =
  | "protocol"
  | "command"
  | "query"
  | "data"
  | "general"
  | "unknown";

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
  return classifyFacts(collectRoleFacts(source));
}

function classifyFacts(facts: RoleFacts): InterfaceRoleClassification {
  if (!facts.publicEvidence) return unknownRole();
  if (facts.contractOperations > 0)
    return role("protocol", 1, "public-contract-operations");
  if (facts.operations === 0 && facts.stateMembers > 0)
    return role("data", 1, "public-state-surface");
  if (isCommandSurface(facts)) return role("command", 0.9, "operation-result-shape");
  if (isQuerySurface(facts)) return role("query", 0.85, "operation-result-shape");
  if (facts.operations > 0)
    return role("general", 0.6, "mixed-operation-shape");
  return unknownRole();
}

function role(
  value: InterfaceRole,
  confidence: number,
  basis: string,
): InterfaceRoleClassification {
  return { role: value, confidence, basis };
}

function unknownRole(): InterfaceRoleClassification {
  return role("unknown", 0, "insufficient-or-contradictory-evidence");
}

function isCommandSurface(facts: RoleFacts): boolean {
  return (
    facts.operations > 0 &&
    facts.inputOperations / facts.operations >= 0.5 &&
    facts.statusOperations === facts.operations
  );
}

function isQuerySurface(facts: RoleFacts): boolean {
  return (
    facts.operations > 0 &&
    facts.valueOperations / facts.operations >= 2 / 3 &&
    facts.mutationOperations <= Math.max(1, Math.floor(facts.operations / 3))
  );
}

function collectRoleFacts(source: ts.SourceFile): RoleFacts {
  const facts = emptyRoleFacts();
  for (const statement of source.statements) {
    if (!hasModifier(statement, ts.SyntaxKind.ExportKeyword)) continue;
    facts.publicEvidence = true;
    collectRoleStatement(statement, facts);
  }
  return facts;
}

function emptyRoleFacts(): RoleFacts {
  return {
    operations: 0,
    inputOperations: 0,
    valueOperations: 0,
    statusOperations: 0,
    contractOperations: 0,
    mutationOperations: 0,
    stateMembers: 0,
    publicEvidence: false,
  };
}

function collectRoleStatement(statement: ts.Statement, facts: RoleFacts): void {
  if (ts.isFunctionDeclaration(statement) && statement.body !== undefined) {
    addRoleOperation(facts, statement, false);
    return;
  }
  if (ts.isVariableStatement(statement)) {
    collectRoleVariables(statement, facts);
    return;
  }
  if (ts.isClassDeclaration(statement)) {
    collectRoleClass(statement, facts);
    return;
  }
  if (ts.isInterfaceDeclaration(statement)) {
    collectRoleInterface(statement, facts);
    return;
  }
  if (ts.isTypeAliasDeclaration(statement)) facts.stateMembers++;
}

function collectRoleVariables(
  statement: ts.VariableStatement,
  facts: RoleFacts,
): void {
  for (const declaration of statement.declarationList.declarations) {
    if (isFunctionInitializer(declaration.initializer))
      addRoleOperation(facts, declaration.initializer, false);
    else facts.stateMembers++;
  }
}

function collectRoleClass(
  statement: ts.ClassDeclaration,
  facts: RoleFacts,
): void {
  for (const member of statement.members) collectRoleClassMember(member, facts);
}

function collectRoleClassMember(
  member: ts.ClassElement,
  facts: RoleFacts,
): void {
  if (!publicMember(member)) return;
  if (isOperationMember(member)) {
    addRoleOperation(
      facts,
      member,
      hasModifier(member, ts.SyntaxKind.AbstractKeyword),
    );
    if (containsThisAssignmentIn(member)) facts.mutationOperations++;
    return;
  }
  if (ts.isPropertyDeclaration(member)) facts.stateMembers++;
}

function collectRoleInterface(
  statement: ts.InterfaceDeclaration,
  facts: RoleFacts,
): void {
  for (const member of statement.members) {
    if (isContractMember(member))
      addRoleOperation(facts, member as ts.SignatureDeclaration, true);
    else if (ts.isPropertySignature(member)) facts.stateMembers++;
  }
}

function containsThisAssignmentIn(member: ts.Node): boolean {
  const body = "body" in member ? (member as { body?: ts.Node }).body : undefined;
  return body !== undefined && containsThisAssignment(body);
}

function addRoleOperation(
  facts: RoleFacts,
  node: FunctionNode | ts.SignatureDeclaration,
  contract: boolean,
): void {
  facts.operations++;
  facts.inputOperations += Number(node.parameters.length > 0);
  const returnType = "type" in node ? node.type : undefined;
  facts.statusOperations += Number(isStatusReturn(returnType));
  facts.valueOperations += Number(!isStatusReturn(returnType));
  facts.contractOperations += Number(contract);
}

function isStatusReturn(returnType: ts.TypeNode | undefined): boolean {
  return (
    returnType === undefined ||
    returnType.kind === ts.SyntaxKind.VoidKeyword ||
    returnType.kind === ts.SyntaxKind.UndefinedKeyword ||
    returnType.kind === ts.SyntaxKind.BooleanKeyword
  );
}
