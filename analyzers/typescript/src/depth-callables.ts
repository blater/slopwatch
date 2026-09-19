import ts from "typescript";
import type { SourceEntry } from "./model.js";
import { localPassiveCarrier } from "./passive-result-carrier.js";
import type { LocalCallable } from "./depth-model.js";
import { functionID } from "./depth-scalars.js";

function exportedFunction(node: ts.Statement): node is ts.FunctionDeclaration {
  return ts.isFunctionDeclaration(node) && node.name !== undefined &&
    ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

function exportedStatement(node: ts.Statement): boolean {
  return ts.canHaveModifiers(node) &&
    ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

export function hasExportModifier(node: ts.Node): boolean {
  return ts.canHaveModifiers(node) &&
    ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

export function sourceCallables(entry: SourceEntry, source: ts.SourceFile, checker: ts.TypeChecker): LocalCallable[] {
  const callables: LocalCallable[] = [];
  for (const statement of source.statements) {
    if (ts.isFunctionDeclaration(statement) && statement.name !== undefined && statement.body !== undefined) {
      callables.push({ id: functionID(entry, statement), node: statement });
      continue;
    }
    if (!ts.isVariableStatement(statement)) continue;
    for (const declaration of statement.declarationList.declarations) {
      const initializer = declaration.initializer;
      if (!ts.isIdentifier(declaration.name) || (statement.declarationList.flags & ts.NodeFlags.Const) === 0 || initializer === undefined ||
          (!ts.isArrowFunction(initializer) && !ts.isFunctionExpression(initializer))) continue;
      callables.push({ id: `${entry.relativePath}#${declaration.name.text}`, node: initializer });
    }
  }
  for (const statement of source.statements) {
    if (!ts.isExportAssignment(statement) || (!ts.isArrowFunction(statement.expression) && !ts.isFunctionExpression(statement.expression))) continue;
    callables.push({ id: `${entry.relativePath}#default`, node: statement.expression });
  }
  return callables;
}

export function exportedCallableSymbols(source: ts.SourceFile, checker: ts.TypeChecker): Set<ts.Symbol> {
  const result = new Set<ts.Symbol>();
  for (const statement of source.statements) {
    if (ts.isFunctionDeclaration(statement) && hasExportModifier(statement) && statement.name !== undefined) {
      const symbol = checker.getSymbolAtLocation(statement.name);
      if (symbol !== undefined) result.add(symbol);
    }
    if (ts.isVariableStatement(statement) && hasExportModifier(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        if (!ts.isIdentifier(declaration.name)) continue;
        const symbol = checker.getSymbolAtLocation(declaration.name);
        if (symbol !== undefined) result.add(symbol);
      }
    }
    if (ts.isExportDeclaration(statement) && statement.exportClause !== undefined && ts.isNamedExports(statement.exportClause)) {
      for (const specifier of statement.exportClause.elements) {
        const symbol = checker.getSymbolAtLocation(specifier.propertyName ?? specifier.name);
        if (symbol === undefined) continue;
        result.add(symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol);
      }
    }
    if (ts.isExportAssignment(statement) && ts.isIdentifier(statement.expression)) {
      const symbol = checker.getSymbolAtLocation(statement.expression);
      if (symbol !== undefined) result.add(symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol);
    }
  }
  return result;
}

export function harmlessUnexportedClass(statement: ts.ClassDeclaration, checker: ts.TypeChecker): boolean {
  return !hasExportModifier(statement) && (statement.members.length === 0 || localPassiveCarrier(statement, checker));
}

export function simpleEnum(statement: ts.EnumDeclaration): boolean {
  return statement.members.every((member) => {
    const initializer = member.initializer;
    return initializer === undefined || ts.isNumericLiteral(initializer) || ts.isStringLiteral(initializer);
  });
}
