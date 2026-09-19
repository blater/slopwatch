import ts from "typescript";
import { CarrierField, directAssignments, isGetter, returnedField, allowedValue, isVoid } from "./carrier-values.js";
import { hasExportModifier, hasModifier, hasDecorators, resolveSymbol, addClassDeclaration } from "./carrier-syntax.js";

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

function inspectCarrier(node: ts.ClassDeclaration, checker: ts.TypeChecker): PassiveCarrierProof | undefined {
  if (node.heritageClauses !== undefined || hasDecorators(node)) return undefined;
  const fields = carrierFields(node, checker);
  if (fields === undefined) return undefined;
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
    const kind = carrierMember(member, fields, getters, checker);
    if (kind === undefined) return undefined;
    if (kind === "constructor") constructors++;
    if (kind === "mutator") mutators++;
  }
  if (getters.size !== fields.size) return undefined;
  return { fields: fields.size, getters: getters.size - exposedFields, mutators, constructors, exposedFields };
}

function carrierFields(node: ts.ClassDeclaration, checker: ts.TypeChecker): Map<ts.Symbol, CarrierField> | undefined {
  const fields = new Map<ts.Symbol, CarrierField>();
  for (const member of node.members) {
    if (!ts.isPropertyDeclaration(member)) continue;
    if (hasDecorators(member) || hasModifier(member, ts.SyntaxKind.StaticKeyword) || ts.isComputedPropertyName(member.name)) return undefined;
    if (member.initializer !== undefined && !allowedValue(member.initializer, undefined, checker, symbolFor(member, checker))) return undefined;
    const symbol = checker.getSymbolAtLocation(member.name);
    if (symbol === undefined) return undefined;
    fields.set(symbol, member);
  }
  return fields;
}

function carrierMember(member: ts.ClassElement, fields: Map<ts.Symbol, CarrierField>, getters: Set<ts.Symbol>, checker: ts.TypeChecker): "constructor" | "getter" | "mutator" | undefined {
  if (hasDecorators(member) || hasModifier(member, ts.SyntaxKind.StaticKeyword) || member.name !== undefined && ts.isComputedPropertyName(member.name)) return undefined;
  if (ts.isConstructorDeclaration(member)) {
    return member.body !== undefined && directAssignments(member.body, member.parameters, fields, checker, true) ? "constructor" : undefined;
  }
  if (ts.isMethodDeclaration(member) || ts.isGetAccessorDeclaration(member)) {
    if (isGetter(member, fields, checker)) {
      const field = returnedField(member, fields, checker);
      if (field !== undefined && !hasModifier(member, ts.SyntaxKind.PrivateKeyword) && !hasModifier(member, ts.SyntaxKind.ProtectedKeyword)) getters.add(field);
      return "getter";
    }
    return isVoid(member, checker) && member.body !== undefined && directAssignments(member.body, member.parameters, fields, checker, false) ? "mutator" : undefined;
  }
  if (ts.isSetAccessorDeclaration(member)) {
    return member.body !== undefined && directAssignments(member.body, member.parameters, fields, checker, false) ? "mutator" : undefined;
  }
  return undefined;
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

export function localPassiveCarrier(node: ts.ClassDeclaration, checker: ts.TypeChecker): boolean {
  return !hasExportModifier(node) && inspectCarrier(node, checker) !== undefined;
}
