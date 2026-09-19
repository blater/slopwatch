import ts from "typescript";

export function hasExportModifier(node: ts.Node): boolean {
  return ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((item) => item.kind === ts.SyntaxKind.ExportKeyword) === true;
}

export function hasModifier(node: ts.Node, kind: ts.SyntaxKind): boolean {
  return ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((item) => item.kind === kind) === true;
}

export function hasDecorators(node: ts.Node): boolean {
  return ts.canHaveDecorators(node) && (ts.getDecorators(node)?.length ?? 0) > 0;
}

export function resolveSymbol(checker: ts.TypeChecker, symbol: ts.Symbol | undefined): ts.Symbol | undefined {
  return symbol !== undefined && (symbol.flags & ts.SymbolFlags.Alias) !== 0 ? checker.getAliasedSymbol(symbol) : symbol;
}

export function addClassDeclaration(symbol: ts.Symbol | undefined, classes: readonly ts.ClassDeclaration[], output: Set<ts.ClassDeclaration>): void {
  for (const declaration of symbol?.declarations ?? []) {
    if (ts.isClassDeclaration(declaration) && classes.includes(declaration)) output.add(declaration);
  }
}

// Local data classes are supporting declarations, even in files exporting work.
