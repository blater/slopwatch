import ts from "typescript";
import type { SourceEntry } from "./model.js";
import { exportedPassiveCarriers } from "./passive-result-carrier.js";
import { callableSymbol } from "./depth-scalars.js";
import { sourceCallables, exportedCallableSymbols, hasExportModifier, simpleEnum } from "./depth-callables.js";

export function analyzeSourceSurface(entry: SourceEntry, source: ts.SourceFile, checker: ts.TypeChecker) {
  const callables = sourceCallables(entry, source, checker);
  const symbols = exportedCallableSymbols(source, checker);
  const passiveEnums = source.statements.filter((statement): statement is ts.EnumDeclaration => ts.isEnumDeclaration(statement) && hasExportModifier(statement) && simpleEnum(statement));
  const passiveCarriers = exportedPassiveCarriers(source, checker);
  const provenPassive = passiveCarriers.filter((carrier) => carrier.proof !== undefined);
  const provenPassiveNodes = new Set(provenPassive.map((carrier) => carrier.node));
  const passiveClassSymbol = (symbol: ts.Symbol | undefined): boolean => {
    const resolved = symbol !== undefined && (symbol.flags & ts.SymbolFlags.Alias) !== 0
      ? checker.getAliasedSymbol(symbol) : symbol;
    return passiveCarriers.some((carrier) => carrier.proof !== undefined
      && carrier.node.name !== undefined
      && checker.getSymbolAtLocation(carrier.node.name) === resolved);
  };
  const hasDefaultCallable = source.statements.some((statement) => ts.isExportAssignment(statement) && (ts.isArrowFunction(statement.expression) || ts.isFunctionExpression(statement.expression)));
  const exported = callables.filter((callable) => {
    if (callable.id.endsWith("#default")) return hasDefaultCallable;
    const symbol = callableSymbol(checker, callable.node) ??
      (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
        ? checker.getSymbolAtLocation(callable.node.parent.name) : undefined);
    return symbol !== undefined && symbols.has(symbol);
  });
  const unsupportedExport = source.statements.some((statement) => unsupportedStatement(statement, checker, exported, passiveEnums, provenPassiveNodes, hasDefaultCallable, passiveClassSymbol));

  return { callables, symbols, passiveEnums, provenPassive, provenPassiveNodes, hasDefaultCallable, exported, unsupportedExport };
}

function unsupportedStatement(statement: ts.Statement, checker: ts.TypeChecker, exported: ReturnType<typeof sourceCallables>, passiveEnums: ts.EnumDeclaration[], provenPassiveNodes: Set<ts.ClassDeclaration>, hasDefaultCallable: boolean, passiveClassSymbol: (symbol: ts.Symbol | undefined) => boolean): boolean {
  if (ts.isExportAssignment(statement)) return !ts.isIdentifier(statement.expression) && !hasDefaultCallable
    || ts.isIdentifier(statement.expression) && exported.length === 0 && !passiveClassSymbol(checker.getSymbolAtLocation(statement.expression));
  if (ts.isExportDeclaration(statement)) {
    if (statement.exportClause === undefined || !ts.isNamedExports(statement.exportClause)) return true;
    return statement.exportClause.elements.some((specifier) => {
      const symbol = checker.getSymbolAtLocation(specifier.propertyName ?? specifier.name);
      const resolved = symbol !== undefined && (symbol.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(symbol) : symbol);
      return !resolved || (!exported.some((callable) =>
        (callableSymbol(checker, callable.node) ??
          (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
            ? checker.getSymbolAtLocation(callable.node.parent.name) : undefined)) === resolved)
        && !passiveClassSymbol(resolved));
    });
  }
  if (ts.isClassDeclaration(statement) && hasExportModifier(statement)) return !provenPassiveNodes.has(statement);
  if (ts.isEnumDeclaration(statement) && hasExportModifier(statement)) return !passiveEnums.includes(statement);
  if (ts.isVariableStatement(statement) && hasExportModifier(statement)) {
    return statement.declarationList.declarations.some((declaration) => {
      const initializer = declaration.initializer;
      return (statement.declarationList.flags & ts.NodeFlags.Const) === 0 || !ts.isIdentifier(declaration.name) || initializer === undefined || (!ts.isArrowFunction(initializer) && !ts.isFunctionExpression(initializer));
    });
  }
  return hasExportModifier(statement) && (ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement));

}
