import ts from "typescript";

import {
  ANALYZER_NAME,
  ANALYZER_VERSION,
  type Measurement,
  type SourceEntry,
  type Subject,
} from "./model.js";
import { cognitive } from "./cognitive.js";
import { classifyInterfaceRole, publicOperations } from "./operations.js";
import { typeMetricMeasurements as collectTypeMetrics } from "./type-metrics.js";
import {
  fieldsUsedByMethod,
  foreignFieldsUsedByMethod,
  functionReturnType,
  isTypeDeclaration,
  methodLike,
  qualifiedFunctionName,
  type PublicOperation,
  publicMember,
  returnsValue,
  typeComplexity,
  typeFunctionFact,
  typeMemberCount,
  typeMembers,
  typeReferences,
  uniqueStrings,
} from "./surface.js";

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
  next: number;
  returns: number;
  throws: number;
  breaks: number;
  continues: number;
  loopbacks: number;
}

interface ConditionPaths {
  end: number;
  whenTrue: number;
  whenFalse: number;
}

const MAX_NPATH = 9999;

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

function sourcePosition(
  source: ts.SourceFile,
  offset: number,
): { line: number; column: number; offset: number } {
  const point = source.getLineAndCharacterOfPosition(offset);
  return { line: point.line + 1, column: point.character + 1, offset };
}

function nodeSubject(
  source: ts.SourceFile,
  node: ts.Node,
  name: string,
  routine?: string,
): Subject {
  const start = node.getStart(source);
  return {
    name,
    symbol: name,
    ...(routine === undefined ? {} : { routine }),
    start: sourcePosition(source, start),
    end: sourcePosition(source, node.getEnd()),
  };
}

function collectFunctions(source: ts.SourceFile): FunctionFact[] {
  const facts: FunctionFact[] = [];
  const visit = (node: ts.Node): void => {
    if (isFunctionNode(node) && node.body !== undefined) {
      const name = qualifiedFunctionName(node, source);
      facts.push({ node, name, subject: nodeSubject(source, node, name, name) });
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return facts.sort(
    (a, b) => a.node.getStart(source) - b.node.getStart(source),
  );
}

function isLogical(node: ts.BinaryExpression): boolean {
  return (
    node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken ||
    node.operatorToken.kind === ts.SyntaxKind.BarBarToken
  );
}

/** PMD 7.26 CycloVisitor.booleanExpressionComplexity mapping. */
function booleanExpressionComplexity(
  expression: ts.Expression | undefined,
): number {
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
    } else if (
      ts.isThrowStatement(node) ||
      ts.isBreakStatement(node) ||
      ts.isContinueStatement(node)
    ) {
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

/* interface CognitiveState {
  complexity: number;
  nesting: number;
  booleanOperation: "and" | "or" | undefined;
  methodStack: string[];
}


class CognitiveWalker {
  private readonly state: CognitiveState;
  constructor(private readonly fact: FunctionFact, private readonly source: ts.SourceFile) {
    this.state = { complexity: 0, nesting: 0, booleanOperation: undefined, methodStack: [fact.name] };
  }
  run(): number {
    if (this.fact.node.body !== undefined) this.walk(this.fact.node.body);
    return this.state.complexity;
  }
  private structural(node: ts.Node): void {
    this.state.complexity += 1 + this.state.nesting;
    this.state.nesting += 1;
    this.children(node);
    this.state.nesting -= 1;
  }
  private ifStatement(node: ts.IfStatement, elseIf: boolean): void {
    this.walk(node.expression);
    if (!elseIf) { this.state.complexity += 1 + this.state.nesting; this.state.nesting += 1; }
    this.walk(node.thenStatement);
    if (!elseIf) this.state.nesting -= 1;
    if (node.elseStatement === undefined) return;
    this.state.complexity += 1;
    this.state.nesting += 1;
    if (ts.isIfStatement(node.elseStatement)) this.ifStatement(node.elseStatement, true);
    else this.walk(node.elseStatement);
    this.state.nesting -= 1;
  }
  private children(node: ts.Node): void { ts.forEachChild(node, (child) => this.walk(child)); }
  private nestedFunction(node: FunctionNode): void {
    this.state.nesting += 1;
    this.state.methodStack.push(functionName(node, this.source));
    if (node.body !== undefined) this.walk(node.body);
    this.state.methodStack.pop();
    this.state.nesting -= 1;
  }
  private walkBlock(node: ts.Block): void {
    for (const statement of node.statements) { this.state.booleanOperation = undefined; this.walk(statement); }
  }
  private walkLogical(node: ts.BinaryExpression): void {
    const operation = node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken ? "and" : "or";
    if (this.state.booleanOperation !== operation) this.state.complexity += 1;
    this.state.booleanOperation = operation;
    this.walk(node.left); this.walk(node.right);
  }
  private walkUnary(node: ts.PrefixUnaryExpression): boolean {
    if (node.operator !== ts.SyntaxKind.ExclamationToken) return false;
    this.state.booleanOperation = undefined; this.walk(node.operand); return true;
  }
  private walkBreak(node: ts.BreakStatement | ts.ContinueStatement): void {
    if (node.label !== undefined) this.state.complexity += 1;
  }
  private walkCall(node: ts.CallExpression): void {
    if (ts.isIdentifier(node.expression) && this.state.methodStack.includes(node.expression.text)) this.state.complexity += 1;
    this.children(node);
  }
  private walk(node: ts.Node): void {
    if (node !== this.fact.node && isFunctionNode(node)) { this.nestedFunction(node); return; }
    if (ts.isBlock(node)) { this.walkBlock(node); return; }
    if (ts.isIfStatement(node)) { this.ifStatement(node, false); return; }
    if (isCognitiveStructural(node)) { this.structural(node); return; }
    if (ts.isBreakStatement(node) || ts.isContinueStatement(node)) { this.walkBreak(node); return; }
    if (ts.isBinaryExpression(node) && isLogical(node)) { this.walkLogical(node); return; }
    if (ts.isPrefixUnaryExpression(node) && this.walkUnary(node)) return;
    if (ts.isCallExpression(node)) { this.walkCall(node); return; }
    this.children(node);
  }
}

function isCognitiveStructural(node: ts.Node): boolean {
  return ts.isForStatement(node) || ts.isForInStatement(node) || ts.isForOfStatement(node) || ts.isWhileStatement(node) || ts.isDoStatement(node) || ts.isSwitchStatement(node) || ts.isCatchClause(node) || ts.isConditionalExpression(node);
}

function oldCognitive(fact: FunctionFact, source: ts.SourceFile): number {
  return new CognitiveWalker(fact, source).run();
}
*/

function emptyFlow(next: number): Flow {
  return { next, returns: 0, throws: 0, breaks: 0, continues: 0, loopbacks: 0 };
}

function combineFlow(left: Flow, right: Flow): Flow {
  return {
    next: left.next + right.next,
    returns: left.returns + right.returns,
    throws: left.throws + right.throws,
    breaks: left.breaks + right.breaks,
    continues: left.continues + right.continues,
    loopbacks: left.loopbacks + right.loopbacks,
  };
}

function expressionPaths(
  expression: ts.Expression,
  incoming: number,
): ConditionPaths {
  if (ts.isParenthesizedExpression(expression))
    return expressionPaths(expression.expression, incoming);
  if (
    ts.isPrefixUnaryExpression(expression) &&
    expression.operator === ts.SyntaxKind.ExclamationToken
  ) {
    const operand = expressionPaths(expression.operand, incoming);
    return {
      end: operand.end,
      whenTrue: operand.whenFalse,
      whenFalse: operand.whenTrue,
    };
  }
  if (ts.isBinaryExpression(expression) && isLogical(expression)) {
    const left = expressionPaths(expression.left, incoming);
    if (
      expression.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken
    ) {
      const right = expressionPaths(expression.right, left.whenTrue);
      return {
        end: left.whenFalse + right.end,
        whenTrue: right.whenTrue,
        whenFalse: left.whenFalse + right.whenFalse,
      };
    }
    const right = expressionPaths(expression.right, left.whenFalse);
    return {
      end: left.whenTrue + right.end,
      whenTrue: left.whenTrue + right.whenTrue,
      whenFalse: right.whenFalse,
    };
  }
  if (ts.isConditionalExpression(expression)) {
    const condition = expressionPaths(expression.condition, incoming);
    const whenTrue = expressionPaths(expression.whenTrue, condition.whenTrue);
    const whenFalse = expressionPaths(
      expression.whenFalse,
      condition.whenFalse,
    );
    return {
      end: whenTrue.end + whenFalse.end,
      whenTrue: whenTrue.whenTrue + whenFalse.whenTrue,
      whenFalse: whenTrue.whenFalse + whenFalse.whenFalse,
    };
  }
  // Optional chaining and ?? have no Java equivalent and are deliberately linear in pmd-v1.
  return { end: incoming, whenTrue: incoming, whenFalse: incoming };
}

function sequence(statements: readonly ts.Statement[], incoming: number): Flow {
  let flow = emptyFlow(incoming);
  for (const statement of statements) {
    const current = statementFlow(statement, flow.next);
    flow = {
      next: current.next,
      returns: flow.returns + current.returns,
      throws: flow.throws + current.throws,
      breaks: flow.breaks + current.breaks,
      continues: flow.continues + current.continues,
      loopbacks: flow.loopbacks + current.loopbacks,
    };
  }
  return flow;
}

function branchFlow(statement: ts.IfStatement, incoming: number): Flow {
  const condition = expressionPaths(statement.expression, incoming);
  const thenFlow = statementFlow(statement.thenStatement, condition.whenTrue);
  const elseFlow =
    statement.elseStatement === undefined
      ? emptyFlow(condition.whenFalse)
      : statementFlow(statement.elseStatement, condition.whenFalse);
  return combineFlow(thenFlow, elseFlow);
}

function loopFlow(
  statement: ts.WhileStatement | ts.ForStatement,
  incoming: number,
): Flow {
  const expression = ts.isForStatement(statement)
    ? statement.condition
    : statement.expression;
  const condition =
    expression === undefined
      ? { end: incoming, whenTrue: incoming, whenFalse: 0 }
      : expressionPaths(expression, incoming);
  const body = statementFlow(statement.statement, condition.whenTrue);
  return {
    next: condition.whenFalse + body.breaks,
    returns: body.returns,
    throws: body.throws,
    breaks: 0,
    continues: 0,
    loopbacks: body.loopbacks + body.next + body.continues,
  };
}

function tryFlow(statement: ts.TryStatement, incoming: number): Flow {
  let total = statementFlow(statement.tryBlock, incoming);
  if (statement.catchClause !== undefined)
    total = combineFlow(
      total,
      statementFlow(statement.catchClause.block, incoming),
    );
  if (statement.finallyBlock === undefined) return total;
  const finallyFlow = statementFlow(statement.finallyBlock, total.next);
  return {
    next: finallyFlow.next,
    returns: total.returns + finallyFlow.returns,
    throws: total.throws + finallyFlow.throws,
    breaks: total.breaks + finallyFlow.breaks,
    continues: total.continues + finallyFlow.continues,
    loopbacks: total.loopbacks + finallyFlow.loopbacks,
  };
}

function terminalFlow(
  statement:
    | ts.ReturnStatement
    | ts.ThrowStatement
    | ts.BreakStatement
    | ts.ContinueStatement,
  incoming: number,
): Flow {
  if (ts.isReturnStatement(statement)) {
    const exits =
      statement.expression === undefined
        ? incoming
        : expressionPaths(statement.expression, incoming).end;
    return { ...emptyFlow(0), returns: exits };
  }
  if (ts.isThrowStatement(statement))
    return {
      ...emptyFlow(0),
      throws: expressionPaths(statement.expression, incoming).end,
    };
  if (ts.isBreakStatement(statement))
    return { ...emptyFlow(0), breaks: incoming };
  return { ...emptyFlow(0), continues: incoming };
}

function variableFlow(statement: ts.VariableStatement, incoming: number): Flow {
  let next = incoming;
  for (const declaration of statement.declarationList.declarations) {
    if (declaration.initializer !== undefined)
      next = expressionPaths(declaration.initializer, next).end;
  }
  return emptyFlow(next);
}

function collectionLoopFlow(
  statement: ts.ForInStatement | ts.ForOfStatement,
  incoming: number,
): Flow {
  const body = statementFlow(statement.statement, incoming);
  return {
    next: incoming + body.breaks,
    returns: body.returns,
    throws: body.throws,
    breaks: 0,
    continues: 0,
    loopbacks: body.loopbacks + body.next + body.continues,
  };
}

function doLoopFlow(statement: ts.DoStatement, incoming: number): Flow {
  const body = statementFlow(statement.statement, incoming);
  const condition = expressionPaths(
    statement.expression,
    body.next + body.continues,
  );
  return {
    next: body.breaks + condition.whenFalse,
    returns: body.returns,
    throws: body.throws,
    breaks: 0,
    continues: 0,
    loopbacks: body.loopbacks + condition.whenTrue,
  };
}

function statementFlow(statement: ts.Statement, incoming: number): Flow {
  if (incoming === 0) return emptyFlow(0);
  if (ts.isBlock(statement)) return sequence(statement.statements, incoming);
  if (ts.isIfStatement(statement)) return branchFlow(statement, incoming);
  if (
    ts.isReturnStatement(statement) ||
    ts.isThrowStatement(statement) ||
    ts.isBreakStatement(statement) ||
    ts.isContinueStatement(statement)
  )
    return terminalFlow(statement, incoming);
  if (ts.isExpressionStatement(statement))
    return emptyFlow(expressionPaths(statement.expression, incoming).end);
  if (ts.isVariableStatement(statement))
    return variableFlow(statement, incoming);
  if (ts.isWhileStatement(statement) || ts.isForStatement(statement))
    return loopFlow(statement, incoming);
  if (ts.isForInStatement(statement) || ts.isForOfStatement(statement))
    return collectionLoopFlow(statement, incoming);
  if (ts.isDoStatement(statement)) return doLoopFlow(statement, incoming);
  if (ts.isSwitchStatement(statement)) return switchFlow(statement, incoming);
  if (ts.isTryStatement(statement)) return tryFlow(statement, incoming);
  return emptyFlow(incoming);
}

function switchFlow(statement: ts.SwitchStatement, incoming: number): Flow {
  let fallthrough = 0;
  let result = emptyFlow(0);
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
      breaks: 0,
      continues: result.continues + branch.continues,
      loopbacks: result.loopbacks + branch.loopbacks,
    };
  }
  result.next += fallthrough;
  if (!hasDefault) result.next += incoming;
  return result;
}

function npath(fact: FunctionFact): number {
  const body = fact.node.body;
  if (body === undefined) return 1;
  let flow: Flow;
  if (ts.isBlock(body)) flow = sequence(body.statements, 1);
  else flow = emptyFlow(expressionPaths(body, 1).end);
  return minNPath(
    flow.next + flow.returns + flow.throws + flow.loopbacks,
    MAX_NPATH,
  );
}

function minNPath(value: number, limit: number): number {
  return value < limit ? value : limit;
}

function walkDeepIf(
  node: ts.Node,
  depth: number,
  fact: FunctionFact,
  entry: SourceEntry,
  measurements: Measurement[],
): void {
  if (node !== fact.node && isFunctionNode(node)) return;
  if (!ts.isIfStatement(node)) {
    ts.forEachChild(node, (child) =>
      walkDeepIf(child, depth, fact, entry, measurements),
    );
    return;
  }
  if (depth + 1 === 3) {
    measurements.push(
      measurement(
        entry,
        "deeply_nested_if",
        "pmd-v1",
        "expression",
        1,
        nodeSubject(entry.sourceFile, node, "if", fact.name),
        { problem_depth: 3, routine_symbol: fact.name },
      ),
    );
    return;
  }
  walkDeepIf(node.expression, depth, fact, entry, measurements);
  walkDeepIf(node.thenStatement, depth + 1, fact, entry, measurements);
  if (node.elseStatement !== undefined)
    walkDeepIf(
      node.elseStatement,
      ts.isIfStatement(node.elseStatement) ? depth : depth + 1,
      fact,
      entry,
      measurements,
    );
}

function deepIfMeasurements(
  entry: SourceEntry,
  fact: FunctionFact,
): Measurement[] {
  const measurements: Measurement[] = [];
  if (fact.node.body !== undefined)
    walkDeepIf(fact.node.body, 0, fact, entry, measurements);
  return measurements;
}

function measurement(
  entry: SourceEntry,
  component: string,
  definition: string,
  scope: "file" | "function" | "type" | "expression",
  value: number,
  subject: Subject,
  attributes: Record<string, unknown> = {},
): Measurement {
  return {
    unit_id: entry.unitId,
    component_id: component,
    definition_version: definition,
    path: entry.relativePath,
    scope,
    value,
    subject,
    attributes,
    provenance: {
      analyzer: ANALYZER_NAME,
      analyzer_version: ANALYZER_VERSION,
      rule: `${component}/${definition}`,
    },
  };
}

/* function hasModifier(node: ts.Node, kind: ts.SyntaxKind): boolean {
  return ts.canHaveModifiers(node) && ts.getModifiers(node)?.some((modifier) => modifier.kind === kind) === true;
}

function publicMember(node: ts.Node): boolean {
  return !hasModifier(node, ts.SyntaxKind.PrivateKeyword) &&
    !hasModifier(node, ts.SyntaxKind.ProtectedKeyword);
}

function functionReturnType(node: FunctionNode): ts.TypeNode | undefined {
  return "type" in node ? node.type : undefined;
}

function typeComplexity(node: ts.TypeNode | undefined, depth = 0): number {
  if (node === undefined || depth > 32) return 1;
  let total = 1;
  if (ts.isArrayTypeNode(node)) total += typeComplexity(node.elementType, depth + 1);
  else if (ts.isTupleTypeNode(node)) total += node.elements.reduce((sum, child) => sum + typeComplexity(child, depth + 1), 0);
  else if (ts.isUnionTypeNode(node) || ts.isIntersectionTypeNode(node)) total += node.types.reduce((sum, child) => sum + typeComplexity(child, depth + 1), 0);
  else if (ts.isTypeReferenceNode(node) && node.typeArguments !== undefined) total += node.typeArguments.reduce((sum, child) => sum + typeComplexity(child, depth + 1), 0);
  else if (ts.isTypeLiteralNode(node)) total += node.members.length;
  else if (ts.isFunctionTypeNode(node)) total += node.parameters.reduce((sum, parameter) => sum + typeComplexity(parameter.type, depth + 1), 0) + typeComplexity(node.type, depth + 1);
  else if (ts.isParenthesizedTypeNode(node)) total += typeComplexity(node.type, depth + 1);
  return Math.min(32, total);
}

function returnsValue(node: FunctionNode): boolean {
  const returnType = functionReturnType(node);
  if (returnType !== undefined && returnType.kind !== ts.SyntaxKind.VoidKeyword &&
      returnType.kind !== ts.SyntaxKind.UndefinedKeyword) return true;
  let result = false;
  const visit = (child: ts.Node): void => {
    if (child !== node && isFunctionNode(child)) return;
    if (ts.isReturnStatement(child) && child.expression !== undefined) result = true;
    ts.forEachChild(child, visit);
  };
  if (node.body !== undefined) visit(node.body);
  return result;
}

interface PublicOperation {
  node: FunctionNode;
  typeCost: number;
  representationCost: number;
}

function typeRepresentationCost(node: ts.TypeNode | undefined, depth = 0): number {
  if (node === undefined || depth > 32) return 0;
  if (ts.isTypeLiteralNode(node)) {
    return node.members.filter((member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword)).length +
      node.members.reduce((sum, member) => {
        if (ts.isPropertySignature(member)) return sum + typeRepresentationCost(member.type, depth + 1);
        return sum;
      }, 0);
  }
  if (ts.isArrayTypeNode(node)) return typeRepresentationCost(node.elementType, depth + 1);
  return 0;
}

function operationFacts(node: FunctionNode): PublicOperation {
  const resultCost = returnsValue(node) ? typeComplexity(functionReturnType(node)) : 0;
  return {
    node,
    typeCost: node.parameters.reduce((sum, parameter) => sum + typeComplexity(parameter.type), 0) + resultCost,
    representationCost: node.parameters.reduce((sum, parameter) => sum + typeRepresentationCost(parameter.type), 0) +
      (returnsValue(node) ? typeRepresentationCost(functionReturnType(node)) : 0)
  };
}

type TypeDeclaration = ts.ClassDeclaration | ts.InterfaceDeclaration;

function isTypeDeclaration(node: ts.Node): node is TypeDeclaration {
  return ts.isClassDeclaration(node) || ts.isInterfaceDeclaration(node);
}

function methodLike(node: ts.ClassElement | ts.TypeElement): node is ts.MethodDeclaration | ts.ConstructorDeclaration | ts.GetAccessorDeclaration | ts.SetAccessorDeclaration | ts.MethodSignature | ts.CallSignatureDeclaration | ts.ConstructSignatureDeclaration | ts.GetAccessorDeclaration | ts.SetAccessorDeclaration {
  return ts.isMethodDeclaration(node) || ts.isConstructorDeclaration(node) ||
    ts.isGetAccessorDeclaration(node) || ts.isSetAccessorDeclaration(node) ||
    ts.isMethodSignature(node) || ts.isCallSignatureDeclaration(node) ||
    ts.isConstructSignatureDeclaration(node);
}

function typeFunctionFact(node: FunctionNode, source: ts.SourceFile): FunctionFact {
  const name = functionName(node, source);
  return { node, name, subject: nodeSubject(source, node, name) };
}

function typeMembers(item: TypeDeclaration, source: ts.SourceFile): FunctionFact[] {
  const output: FunctionFact[] = [];
  for (const member of item.members) {
    if (!methodLike(member) || !isFunctionNode(member) || member.body === undefined) continue;
    output.push(typeFunctionFact(member, source));
  }
  return output;
}

function typeMemberCount(item: TypeDeclaration): number {
  return item.members.filter((member) => methodLike(member)).length;
}

function typeReferences(node: ts.Node): string[] {
  const output = new Set<string>();
  const visit = (current: ts.Node): void => {
    if (ts.isTypeReferenceNode(current)) output.add(current.typeName.getText());
    ts.forEachChild(current, visit);
  };
  visit(node);
  return [...output].sort();
}

function fieldsUsedByMethod(fact: FunctionFact): Set<string> {
  const output = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (node !== fact.node && isFunctionNode(node)) return;
    if (ts.isPropertyAccessExpression(node) && node.expression.kind === ts.SyntaxKind.ThisKeyword) {
      output.add(node.name.text);
    }
    ts.forEachChild(node, visit);
  };
  if (fact.node.body !== undefined) visit(fact.node.body);
  return output;
}

function foreignFieldsUsedByMethod(fact: FunctionFact): Set<string> {
  const output = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (node !== fact.node && isFunctionNode(node)) return;
    if (ts.isPropertyAccessExpression(node) && node.expression.kind !== ts.SyntaxKind.ThisKeyword) {
      output.add(node.name.text);
    }
    ts.forEachChild(node, visit);
  };
  if (fact.node.body !== undefined) visit(fact.node.body);
  return output;
}

function uniqueStrings(values: Iterable<string>): string[] {
  return [...new Set(values)].sort();
} */

/* function typeMetricMeasurements(
  entry: SourceEntry,
  requestedComponents: ReadonlySet<string>
): Measurement[] {
  const output: Measurement[] = [];
  for (const statement of entry.sourceFile.statements) {
    if (!isTypeDeclaration(statement)) continue;
    const methods = typeMembers(statement, entry.sourceFile);
    const methodFieldSets = methods.map((method) => fieldsUsedByMethod(method));
    let connected = 0;
    let pairs = 0;
    for (let left = 0; left < methodFieldSets.length; left++) {
      const leftFields = methodFieldSets[left];
      if (leftFields === undefined) continue;
      for (let right = left + 1; right < methodFieldSets.length; right++) {
        pairs++;
        const rightFields = methodFieldSets[right];
        if (rightFields !== undefined && [...leftFields].some((field) => rightFields.has(field))) connected++;
      }
    }
    const tcc = pairs === 0 ? 0 : connected / pairs;
    const wmc = typeMemberCount(statement) + methods.reduce((sum, method) => sum + cyclomatic(method), 0);
    const foreignTypes = uniqueStrings(typeReferences(statement));
    const foreignFields = uniqueStrings(methods.flatMap((method) => [...foreignFieldsUsedByMethod(method)]));

    if (requestedComponents.has("cyclomatic_class_complexity")) {
      output.push(measurement(entry, "cyclomatic_class_complexity", "pmd-v1", "type", wmc,
        nodeSubject(entry.sourceFile, statement, statement.name?.text ?? "<anonymous-type>"), { kind: ts.isInterfaceDeclaration(statement) ? "interface" : "class" }));
    }
    if (requestedComponents.has("coupling_between_objects")) {
      output.push(measurement(entry, "coupling_between_objects", "pmd-v1", "type", foreignTypes.length,
        nodeSubject(entry.sourceFile, statement, statement.name?.text ?? "<anonymous-type>"), {
          kind: ts.isInterfaceDeclaration(statement) ? "interface" : "class",
          referenced_types: foreignTypes
        }));
    }
    if (requestedComponents.has("god_class") && ts.isClassDeclaration(statement)) {
      output.push(measurement(entry, "god_class", "pmd-v1", "type", wmc,
        nodeSubject(entry.sourceFile, statement, statement.name?.text ?? "<anonymous-class>"), {
          kind: "class", wmc, atfd: foreignFields.length, tcc
        }));
    }
  }
  return output;
} */

/* function publicOperations(source: ts.SourceFile): { operations: PublicOperation[]; publicTypes: number; exposedState: number; exposedRepresentation: number } {
  const operations: PublicOperation[] = [];
  let publicTypes = 0;
  let exposedState = 0;
  let exposedRepresentation = 0;
  for (const statement of source.statements) {
    if (!hasModifier(statement, ts.SyntaxKind.ExportKeyword)) continue;
    if (ts.isFunctionDeclaration(statement) && statement.body !== undefined) {
      operations.push(operationFacts(statement));
    } else if (ts.isVariableStatement(statement)) {
      for (const declaration of statement.declarationList.declarations) {
        if (declaration.initializer !== undefined && (ts.isArrowFunction(declaration.initializer) || ts.isFunctionExpression(declaration.initializer))) {
          const node = declaration.initializer;
          operations.push(operationFacts(node));
        } else {
          exposedState += 1;
          if (statement.declarationList.flags & ts.NodeFlags.Const) continue;
          exposedRepresentation += 1;
        }
      }
    } else if (ts.isClassDeclaration(statement)) {
      publicTypes += 1;
      for (const member of statement.members) {
        if (!publicMember(member)) continue;
        if (ts.isMethodDeclaration(member) || ts.isGetAccessorDeclaration(member) || ts.isSetAccessorDeclaration(member)) {
          const node = member;
          operations.push(operationFacts(node));
          if ((ts.isGetAccessorDeclaration(member) || ts.isSetAccessorDeclaration(member)) && directAccessorExposure(member)) {
            exposedRepresentation += 1;
          }
        } else if (ts.isPropertyDeclaration(member)) {
          exposedState += 1;
          if (!hasModifier(member, ts.SyntaxKind.ReadonlyKeyword)) exposedRepresentation += 1;
        }
      }
    } else if (ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement)) {
      publicTypes += 1;
      if (ts.isInterfaceDeclaration(statement)) {
        exposedState += statement.members.length;
        exposedRepresentation += statement.members.filter((member) => !hasModifier(member, ts.SyntaxKind.ReadonlyKeyword)).length;
      }
      else exposedState += typeComplexity(statement.type);
    }
  }
  return { operations, publicTypes, exposedState, exposedRepresentation };
}

function directAccessorExposure(node: ts.GetAccessorDeclaration | ts.SetAccessorDeclaration): boolean {
  if (node.body === undefined) return false;
  let exposed = false;
  const visit = (child: ts.Node): void => {
    if (exposed || (child !== node && isFunctionNode(child))) return;
    if (ts.isPropertyAccessExpression(child) && child.expression.kind === ts.SyntaxKind.ThisKeyword) {
      exposed = true;
      return;
    }
    ts.forEachChild(child, visit);
  };
  visit(node.body);
  return exposed;
}

} */

function moduleShallowness(entry: SourceEntry): Measurement {
  const surface = publicOperations(entry.sourceFile);
  const role = classifyInterfaceRole(entry.sourceFile);
  const parameterCount = surface.operations.reduce(
    (sum, operation) => sum + operation.node.parameters.length,
    0,
  );
  const resultCount = surface.operations.filter(
    (operation) => operation.returnsValue,
  ).length;
  const entries = surface.operations.reduce(
    (sum, operation) => sum + Math.max(1, operation.node.parameters.length),
    0,
  );
  const exits = resultCount;
  const functionality = entries + exits;
  const operationCost = surface.operations.length;
  const typeCost = surface.operations.reduce(
    (sum, operation) => sum + operation.typeCost,
    0,
  );
  const exposedStateCost = surface.exposedState;
  const signatureRepresentation = surface.operations.reduce(
    (sum, operation) => sum + operation.representationCost,
    0,
  );
  const interfaceCost = operationCost + typeCost + exposedStateCost;
  const available = interfaceCost > 0;
  const depthReference = depthReferenceForRole(
    role.role,
    role.confidence,
    surface.operations.length,
    surface.publicTypes,
  );
  const depth = available ? functionality / interfaceCost : 0;
  const depthPenalty = available
    ? 100 * Math.max(0, 1 - depth / depthReference)
    : 0;
  const leakagePenalty = available
    ? 100 *
      Math.min(
        1,
        (surface.exposedRepresentation + signatureRepresentation) /
          interfaceCost,
      )
    : 0;
  const value = available
    ? Math.max(
        0,
        Math.min(100, Math.round(0.8 * depthPenalty + 0.2 * leakagePenalty)),
      )
    : 0;
  return measurement(
    entry,
    "module_shallowness",
    "ousterhout-v3",
    "file",
    value,
    nodeSubject(entry.sourceFile, entry.sourceFile, entry.relativePath),
    {
      available,
      functionality,
      entries,
      exits,
      parameter_count: parameterCount,
      result_count: resultCount,
      reads: 0,
      writes: 0,
      evidence_level: 2,
      evidence_coverage: 1,
      capability_basis: "cosmic-inspired-signature",
      interface_cost: interfaceCost,
      operation_cost: operationCost,
      type_cost: typeCost,
      public_types: surface.publicTypes,
      exposed_state_cost: exposedStateCost,
      information_hiding_cost: 0,
      exposed_representation:
        surface.exposedRepresentation + signatureRepresentation,
      raw_depth_ratio: depth,
      depth_reference: depthReference,
      depth_penalty: depthPenalty,
      leakage_penalty: leakagePenalty,
      available_weight: 1,
      total_weight: 1,
      reference_role: role.role,
      role_confidence: role.confidence,
      role_basis: role.basis,
      reference_basis: "role-shape-v2",
      reference_policy: "role-shape-v2",
      reference_sample_count: 0,
    },
  );
}

function depthReferenceForRole(
  role: string,
  confidence: number,
  operationCount: number,
  publicTypeCount: number,
): number {
  const priors: Record<string, number> = {
    protocol: 0.72,
    command: 1.12,
    query: 1.05,
    data: 0.85,
    general: 1,
    unknown: 1,
  };
  const prior = priors[role] ?? 1;
  const boundedConfidence = Math.max(0, Math.min(1, confidence));
  const surfaceSize = Math.min(1, (operationCount + publicTypeCount) / 4);
  return 1 + (prior - 1) * boundedConfidence * surfaceSize;
}

export function analyzeStructural(
  entry: SourceEntry,
  requestedComponents: ReadonlySet<string>,
): Measurement[] {
  const output: Measurement[] = [];
  if (requestedComponents.has("module_shallowness"))
    output.push(moduleShallowness(entry));
  output.push(...collectTypeMetrics(entry, requestedComponents, cyclomatic));
  const functions = collectFunctions(entry.sourceFile);
  for (const fact of functions) {
    if (requestedComponents.has("cognitive_complexity")) {
      output.push(
        measurement(
          entry,
          "cognitive_complexity",
          "pmd-sonar-v1",
          "function",
          cognitive(fact, entry.sourceFile),
          fact.subject,
        ),
      );
    }
    if (requestedComponents.has("cyclomatic_method_complexity")) {
      output.push(
        measurement(
          entry,
          "cyclomatic_method_complexity",
          "pmd-v1",
          "function",
          cyclomatic(fact),
          fact.subject,
        ),
      );
    }
    if (requestedComponents.has("npath_complexity")) {
      output.push(
        measurement(
          entry,
          "npath_complexity",
          "pmd-v1",
          "function",
          npath(fact),
          fact.subject,
        ),
      );
    }
    if (requestedComponents.has("deeply_nested_if")) {
      output.push(...deepIfMeasurements(entry, fact));
    }
  }
  return output;
}
