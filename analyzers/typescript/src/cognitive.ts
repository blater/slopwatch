import ts from "typescript";

type FunctionNode =
  | ts.FunctionDeclaration
  | ts.MethodDeclaration
  | ts.ConstructorDeclaration
  | ts.GetAccessorDeclaration
  | ts.SetAccessorDeclaration
  | ts.FunctionExpression
  | ts.ArrowFunction;
type FunctionLike = { node: FunctionNode; name: string };

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

function logical(node: ts.BinaryExpression): boolean {
  return (
    node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken ||
    node.operatorToken.kind === ts.SyntaxKind.BarBarToken
  );
}

interface State {
  complexity: number;
  nesting: number;
  booleanOperation: "and" | "or" | undefined;
  methodStack: string[];
}

class Walker {
  private readonly state: State;
  constructor(
    private readonly fact: FunctionLike,
    private readonly source: ts.SourceFile,
  ) {
    this.state = {
      complexity: 0,
      nesting: 0,
      booleanOperation: undefined,
      methodStack: [fact.name],
    };
  }
  run(): number {
    if (this.fact.node.body !== undefined) this.walk(this.fact.node.body);
    return this.state.complexity;
  }
  private children(node: ts.Node): void {
    ts.forEachChild(node, (child) => this.walk(child));
  }
  private structural(node: ts.Node): void {
    this.state.complexity += 1 + this.state.nesting;
    this.state.nesting++;
    this.children(node);
    this.state.nesting--;
  }
  private ifStatement(node: ts.IfStatement, elseIf: boolean): void {
    this.walk(node.expression);
    if (!elseIf) {
      this.state.complexity += 1 + this.state.nesting;
      this.state.nesting++;
    }
    this.walk(node.thenStatement);
    if (!elseIf) this.state.nesting--;
    if (node.elseStatement === undefined) return;
    this.state.complexity++;
    this.state.nesting++;
    if (ts.isIfStatement(node.elseStatement))
      this.ifStatement(node.elseStatement, true);
    else this.walk(node.elseStatement);
    this.state.nesting--;
  }
  private nestedFunction(node: FunctionNode): void {
    this.state.nesting++;
    this.state.methodStack.push(functionName(node, this.source));
    if (node.body !== undefined) this.walk(node.body);
    this.state.methodStack.pop();
    this.state.nesting--;
  }
  private walkBlock(node: ts.Block): void {
    for (const statement of node.statements) {
      this.state.booleanOperation = undefined;
      this.walk(statement);
    }
  }
  private walkLogical(node: ts.BinaryExpression): void {
    const operation =
      node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken
        ? "and"
        : "or";
    if (this.state.booleanOperation !== operation) this.state.complexity++;
    this.state.booleanOperation = operation;
    this.walk(node.left);
    this.walk(node.right);
  }
  private walk(node: ts.Node): void {
    if (node !== this.fact.node && isFunctionNode(node)) {
      this.nestedFunction(node);
      return;
    }
    if (ts.isBlock(node)) {
      this.walkBlock(node);
      return;
    }
    if (ts.isIfStatement(node)) {
      this.ifStatement(node, false);
      return;
    }
    if (
      ts.isForStatement(node) ||
      ts.isForInStatement(node) ||
      ts.isForOfStatement(node) ||
      ts.isWhileStatement(node) ||
      ts.isDoStatement(node) ||
      ts.isSwitchStatement(node) ||
      ts.isCatchClause(node) ||
      ts.isConditionalExpression(node)
    ) {
      this.structural(node);
      return;
    }
    if (ts.isBreakStatement(node) || ts.isContinueStatement(node)) {
      if (node.label !== undefined) this.state.complexity++;
      return;
    }
    if (ts.isBinaryExpression(node) && logical(node)) {
      this.walkLogical(node);
      return;
    }
    if (
      ts.isPrefixUnaryExpression(node) &&
      node.operator === ts.SyntaxKind.ExclamationToken
    ) {
      this.state.booleanOperation = undefined;
      this.walk(node.operand);
      return;
    }
    if (ts.isCallExpression(node)) {
      if (
        ts.isIdentifier(node.expression) &&
        this.state.methodStack.includes(node.expression.text)
      )
        this.state.complexity++;
      this.children(node);
      return;
    }
    this.children(node);
  }
}

export function cognitive(fact: FunctionLike, source: ts.SourceFile): number {
  return new Walker(fact, source).run();
}
