import ts from "typescript";

import { ANALYZER_NAME, ANALYZER_VERSION, jsonInteger, type Measurement, type SourceEntry, type Subject } from "./model.js";

type FunctionNode =
  | ts.FunctionDeclaration
  | ts.MethodDeclaration
  | ts.ConstructorDeclaration
  | ts.GetAccessorDeclaration
  | ts.SetAccessorDeclaration
  | ts.FunctionExpression
  | ts.ArrowFunction;

interface FunctionFact {
  node: FunctionNode;
  name: string;
  subject: Subject;
}

interface Flow {
  next: bigint;
  returns: bigint;
  throws: bigint;
  breaks: bigint;
  continues: bigint;
  loopbacks: bigint;
}

interface ConditionPaths {
  end: bigint;
  whenTrue: bigint;
  whenFalse: bigint;
}

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

function sourcePosition(source: ts.SourceFile, offset: number): { line: number; column: number; offset: number } {
  const point = source.getLineAndCharacterOfPosition(offset);
  return { line: point.line + 1, column: point.character + 1, offset };
}

function nodeSubject(source: ts.SourceFile, node: ts.Node, name: string): Subject {
  const start = node.getStart(source);
  return {
    name,
    start: sourcePosition(source, start),
    end: sourcePosition(source, node.getEnd())
  };
}

function functionName(node: FunctionNode, source: ts.SourceFile): string {
  if ("name" in node && node.name !== undefined) return node.name.getText(source);
  const parent = node.parent;
  if (ts.isVariableDeclaration(parent) && ts.isIdentifier(parent.name)) return parent.name.text;
  if (ts.isPropertyAssignment(parent)) return parent.name.getText(source);
  const point = source.getLineAndCharacterOfPosition(node.getStart(source));
  return `<anonymous@${point.line + 1}:${point.character + 1}>`;
}

function collectFunctions(source: ts.SourceFile): FunctionFact[] {
  const facts: FunctionFact[] = [];
  const visit = (node: ts.Node): void => {
    if (isFunctionNode(node) && node.body !== undefined) {
      const name = functionName(node, source);
      facts.push({ node, name, subject: nodeSubject(source, node, name) });
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return facts.sort((a, b) => a.node.getStart(source) - b.node.getStart(source));
}

function isLogical(node: ts.BinaryExpression): boolean {
  return (
    node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken ||
    node.operatorToken.kind === ts.SyntaxKind.BarBarToken
  );
}

/** PMD 7.26 CycloVisitor.booleanExpressionComplexity mapping. */
function booleanExpressionComplexity(expression: ts.Expression | undefined): number {
  if (expression === undefined) return 0;
  if (ts.isConditionalExpression(expression)) {
    return (
      booleanExpressionComplexity(expression.condition) +
      booleanExpressionComplexity(expression.whenTrue) +
      booleanExpressionComplexity(expression.whenFalse) +
      2
    );
  }
  let result = 0;
  const visit = (node: ts.Node): void => {
    if (ts.isBinaryExpression(node) && isLogical(node)) result += 1;
    ts.forEachChild(node, visit);
  };
  visit(expression);
  return result;
}

function cyclomatic(fact: FunctionFact): number {
  let value = 1;
  const top = fact.node;
  const visit = (node: ts.Node): void => {
    if (node !== top && isFunctionNode(node)) return;

    if (ts.isIfStatement(node)) {
      value += 1 + booleanExpressionComplexity(node.expression);
    } else if (ts.isWhileStatement(node) || ts.isDoStatement(node)) {
      value += 1 + booleanExpressionComplexity(node.expression);
    } else if (ts.isForStatement(node)) {
      value += 1 + booleanExpressionComplexity(node.condition);
    } else if (ts.isForInStatement(node) || ts.isForOfStatement(node)) {
      value += 1;
    } else if (ts.isCatchClause(node)) {
      value += 1;
    } else if (ts.isConditionalExpression(node)) {
      value += 1 + booleanExpressionComplexity(node.condition);
    } else if (ts.isThrowStatement(node) || ts.isBreakStatement(node) || ts.isContinueStatement(node)) {
      value += 1;
    } else if (ts.isSwitchStatement(node)) {
      value += booleanExpressionComplexity(node.expression);
      value += node.caseBlock.clauses.filter(ts.isCaseClause).length;
    }
    ts.forEachChild(node, visit);
  };
  if (fact.node.body !== undefined) visit(fact.node.body);
  return value;
}

interface CognitiveState {
  complexity: number;
  nesting: number;
  booleanOperation: "and" | "or" | undefined;
  methodStack: string[];
}

function cognitive(fact: FunctionFact, source: ts.SourceFile): number {
  const state: CognitiveState = {
    complexity: 0,
    nesting: 0,
    booleanOperation: undefined,
    methodStack: [fact.name]
  };

  const structural = (node: ts.Node): void => {
    state.complexity += 1 + state.nesting;
    state.nesting += 1;
    walkChildren(node);
    state.nesting -= 1;
  };

  const walkIf = (node: ts.IfStatement, isElseIf: boolean): void => {
    walk(node.expression);
    if (!isElseIf) {
      state.complexity += 1 + state.nesting;
      state.nesting += 1;
    }
    walk(node.thenStatement);
    if (!isElseIf) state.nesting -= 1;
    if (node.elseStatement !== undefined) {
      // PMD's hybrid increment: one point and one nesting level, with no nesting premium.
      state.complexity += 1;
      state.nesting += 1;
      if (ts.isIfStatement(node.elseStatement)) walkIf(node.elseStatement, true);
      else walk(node.elseStatement);
      state.nesting -= 1;
    }
  };

  const walkChildren = (node: ts.Node): void => {
    ts.forEachChild(node, walk);
  };

  const walk = (node: ts.Node): void => {
    if (node !== fact.node && isFunctionNode(node)) {
      state.nesting += 1;
      state.methodStack.push(functionName(node, source));
      if (node.body !== undefined) walk(node.body);
      state.methodStack.pop();
      state.nesting -= 1;
      return;
    }
    if (ts.isBlock(node)) {
      for (const statement of node.statements) {
        state.booleanOperation = undefined;
        walk(statement);
      }
      return;
    }
    if (ts.isIfStatement(node)) {
      walkIf(node, false);
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
      structural(node);
      return;
    }
    if (ts.isBreakStatement(node) || ts.isContinueStatement(node)) {
      if (node.label !== undefined) state.complexity += 1;
      return;
    }
    if (ts.isBinaryExpression(node) && isLogical(node)) {
      const operation =
        node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken ? "and" : "or";
      if (state.booleanOperation !== operation) state.complexity += 1;
      state.booleanOperation = operation;
      walk(node.left);
      walk(node.right);
      return;
    }
    if (ts.isPrefixUnaryExpression(node) && node.operator === ts.SyntaxKind.ExclamationToken) {
      state.booleanOperation = undefined;
      walk(node.operand);
      return;
    }
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression)) {
      if (state.methodStack.includes(node.expression.text)) state.complexity += 1;
    }
    walkChildren(node);
  };

  if (fact.node.body !== undefined) walk(fact.node.body);
  return state.complexity;
}

function emptyFlow(next: bigint): Flow {
  return { next, returns: 0n, throws: 0n, breaks: 0n, continues: 0n, loopbacks: 0n };
}

function combineFlow(left: Flow, right: Flow): Flow {
  return {
    next: left.next + right.next,
    returns: left.returns + right.returns,
    throws: left.throws + right.throws,
    breaks: left.breaks + right.breaks,
    continues: left.continues + right.continues,
    loopbacks: left.loopbacks + right.loopbacks
  };
}

function expressionPaths(expression: ts.Expression, incoming: bigint): ConditionPaths {
  if (ts.isParenthesizedExpression(expression)) return expressionPaths(expression.expression, incoming);
  if (ts.isPrefixUnaryExpression(expression) && expression.operator === ts.SyntaxKind.ExclamationToken) {
    const operand = expressionPaths(expression.operand, incoming);
    return { end: operand.end, whenTrue: operand.whenFalse, whenFalse: operand.whenTrue };
  }
  if (ts.isBinaryExpression(expression) && isLogical(expression)) {
    const left = expressionPaths(expression.left, incoming);
    if (expression.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) {
      const right = expressionPaths(expression.right, left.whenTrue);
      return {
        end: left.whenFalse + right.end,
        whenTrue: right.whenTrue,
        whenFalse: left.whenFalse + right.whenFalse
      };
    }
    const right = expressionPaths(expression.right, left.whenFalse);
    return {
      end: left.whenTrue + right.end,
      whenTrue: left.whenTrue + right.whenTrue,
      whenFalse: right.whenFalse
    };
  }
  if (ts.isConditionalExpression(expression)) {
    const condition = expressionPaths(expression.condition, incoming);
    const whenTrue = expressionPaths(expression.whenTrue, condition.whenTrue);
    const whenFalse = expressionPaths(expression.whenFalse, condition.whenFalse);
    return {
      end: whenTrue.end + whenFalse.end,
      whenTrue: whenTrue.whenTrue + whenFalse.whenTrue,
      whenFalse: whenTrue.whenFalse + whenFalse.whenFalse
    };
  }
  // Optional chaining and ?? have no Java equivalent and are deliberately linear in pmd-v1.
  return { end: incoming, whenTrue: incoming, whenFalse: incoming };
}

function sequence(statements: readonly ts.Statement[], incoming: bigint): Flow {
  let flow = emptyFlow(incoming);
  for (const statement of statements) {
    const current = statementFlow(statement, flow.next);
    flow = {
      next: current.next,
      returns: flow.returns + current.returns,
      throws: flow.throws + current.throws,
      breaks: flow.breaks + current.breaks,
      continues: flow.continues + current.continues,
      loopbacks: flow.loopbacks + current.loopbacks
    };
  }
  return flow;
}

function statementFlow(statement: ts.Statement, incoming: bigint): Flow {
  if (incoming === 0n) return emptyFlow(0n);
  if (ts.isBlock(statement)) return sequence(statement.statements, incoming);
  if (ts.isIfStatement(statement)) {
    const condition = expressionPaths(statement.expression, incoming);
    const thenFlow = statementFlow(statement.thenStatement, condition.whenTrue);
    const elseFlow =
      statement.elseStatement === undefined
        ? emptyFlow(condition.whenFalse)
        : statementFlow(statement.elseStatement, condition.whenFalse);
    return combineFlow(thenFlow, elseFlow);
  }
  if (ts.isReturnStatement(statement)) {
    const exits = statement.expression === undefined ? incoming : expressionPaths(statement.expression, incoming).end;
    return { ...emptyFlow(0n), returns: exits };
  }
  if (ts.isThrowStatement(statement)) {
    const exits = expressionPaths(statement.expression, incoming).end;
    return { ...emptyFlow(0n), throws: exits };
  }
  if (ts.isBreakStatement(statement)) return { ...emptyFlow(0n), breaks: incoming };
  if (ts.isContinueStatement(statement)) return { ...emptyFlow(0n), continues: incoming };
  if (ts.isExpressionStatement(statement)) {
    return emptyFlow(expressionPaths(statement.expression, incoming).end);
  }
  if (ts.isVariableStatement(statement)) {
    let next = incoming;
    for (const declaration of statement.declarationList.declarations) {
      if (declaration.initializer !== undefined) next = expressionPaths(declaration.initializer, next).end;
    }
    return emptyFlow(next);
  }
  if (ts.isWhileStatement(statement) || ts.isForStatement(statement)) {
    const expression = ts.isForStatement(statement) ? statement.condition : statement.expression;
    const condition =
      expression === undefined
        ? { end: incoming, whenTrue: incoming, whenFalse: 0n }
        : expressionPaths(expression, incoming);
    const body = statementFlow(statement.statement, condition.whenTrue);
    return {
      next: condition.whenFalse + body.breaks,
      returns: body.returns,
      throws: body.throws,
      breaks: 0n,
      continues: 0n,
      loopbacks: body.loopbacks + body.next + body.continues
    };
  }
  if (ts.isForInStatement(statement) || ts.isForOfStatement(statement)) {
    const body = statementFlow(statement.statement, incoming);
    return {
      next: incoming + body.breaks,
      returns: body.returns,
      throws: body.throws,
      breaks: 0n,
      continues: 0n,
      loopbacks: body.loopbacks + body.next + body.continues
    };
  }
  if (ts.isDoStatement(statement)) {
    const body = statementFlow(statement.statement, incoming);
    const condition = expressionPaths(statement.expression, body.next + body.continues);
    return {
      next: body.breaks + condition.whenFalse,
      returns: body.returns,
      throws: body.throws,
      breaks: 0n,
      continues: 0n,
      loopbacks: body.loopbacks + condition.whenTrue
    };
  }
  if (ts.isSwitchStatement(statement)) return switchFlow(statement, incoming);
  if (ts.isTryStatement(statement)) {
    let total = statementFlow(statement.tryBlock, incoming);
    for (const clause of statement.catchClause === undefined ? [] : [statement.catchClause]) {
      total = combineFlow(total, statementFlow(clause.block, incoming));
    }
    if (statement.finallyBlock !== undefined) {
      const finallyFlow = statementFlow(statement.finallyBlock, total.next);
      total = {
        next: finallyFlow.next,
        returns: total.returns + finallyFlow.returns,
        throws: total.throws + finallyFlow.throws,
        breaks: total.breaks + finallyFlow.breaks,
        continues: total.continues + finallyFlow.continues,
        loopbacks: total.loopbacks + finallyFlow.loopbacks
      };
    }
    return total;
  }
  return emptyFlow(incoming);
}

function switchFlow(statement: ts.SwitchStatement, incoming: bigint): Flow {
  let fallthrough = 0n;
  let result = emptyFlow(0n);
  let hasDefault = false;
  for (const clause of statement.caseBlock.clauses) {
    const branchStart = incoming;
    if (ts.isDefaultClause(clause)) hasDefault = true;
    const branch = sequence(clause.statements, fallthrough + branchStart);
    fallthrough = branch.next;
    result = {
      next: result.next + branch.breaks,
      returns: result.returns + branch.returns,
      throws: result.throws + branch.throws,
      breaks: 0n,
      continues: result.continues + branch.continues,
      loopbacks: result.loopbacks + branch.loopbacks
    };
  }
  result.next += fallthrough;
  if (!hasDefault) result.next += incoming;
  return result;
}

function npath(fact: FunctionFact): bigint {
  const body = fact.node.body;
  if (body === undefined) return 1n;
  let flow: Flow;
  if (ts.isBlock(body)) flow = sequence(body.statements, 1n);
  else flow = emptyFlow(expressionPaths(body, 1n).end);
  return flow.next + flow.returns + flow.throws + flow.loopbacks;
}

function deepIfMeasurements(entry: SourceEntry, fact: FunctionFact): Measurement[] {
  const measurements: Measurement[] = [];
  const visit = (node: ts.Node, depth: number): void => {
    if (node !== fact.node && isFunctionNode(node)) return;
    if (ts.isIfStatement(node)) {
      if (depth + 1 === 3) {
        measurements.push(
          measurement(entry, "deeply_nested_if", "pmd-v1", "expression", 1, nodeSubject(entry.sourceFile, node, "if"), {
            problem_depth: 3
          })
        );
        return; // PMD stops this chain so it is reported once.
      }
      visit(node.expression, depth);
      visit(node.thenStatement, depth + 1);
      if (node.elseStatement !== undefined) {
        visit(node.elseStatement, ts.isIfStatement(node.elseStatement) ? depth : depth + 1);
      }
      return;
    }
    ts.forEachChild(node, (child) => visit(child, depth));
  };
  if (fact.node.body !== undefined) visit(fact.node.body, 0);
  return measurements;
}

function measurement(
  entry: SourceEntry,
  component: string,
  definition: string,
  scope: "function" | "expression",
  value: number | bigint,
  subject: Subject,
  attributes: Record<string, unknown> = {}
): Measurement {
  return {
    unit_id: entry.unitId,
    component_id: component,
    definition_version: definition,
    path: entry.relativePath,
    scope,
    value: typeof value === "bigint" ? jsonInteger(value) : value,
    subject,
    attributes,
    provenance: {
      analyzer: ANALYZER_NAME,
      analyzer_version: ANALYZER_VERSION,
      rule: `${component}/${definition}`
    }
  };
}

export function analyzeStructural(
  entry: SourceEntry,
  requestedComponents: ReadonlySet<string>
): Measurement[] {
  const output: Measurement[] = [];
  const functions = collectFunctions(entry.sourceFile);
  for (const fact of functions) {
    if (requestedComponents.has("cognitive_complexity")) {
      output.push(
        measurement(entry, "cognitive_complexity", "pmd-sonar-v1", "function", cognitive(fact, entry.sourceFile), fact.subject)
      );
    }
    if (requestedComponents.has("cyclomatic_method_complexity")) {
      output.push(
        measurement(entry, "cyclomatic_method_complexity", "pmd-v1", "function", cyclomatic(fact), fact.subject)
      );
    }
    if (requestedComponents.has("npath_complexity")) {
      output.push(measurement(entry, "npath_complexity", "pmd-v1", "function", npath(fact), fact.subject));
    }
    if (requestedComponents.has("deeply_nested_if")) {
      output.push(...deepIfMeasurements(entry, fact));
    }
  }
  return output;
}
