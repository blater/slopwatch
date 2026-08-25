import ts from "typescript";

export function isFunctionInitializer(
  node: ts.Expression | undefined,
): node is ts.ArrowFunction | ts.FunctionExpression {
  return node !== undefined && (ts.isArrowFunction(node) || ts.isFunctionExpression(node));
}

export function isOperationMember(
  member: ts.ClassElement,
): member is
  | ts.MethodDeclaration
  | ts.GetAccessorDeclaration
  | ts.SetAccessorDeclaration {
  return (
    ts.isMethodDeclaration(member) ||
    ts.isGetAccessorDeclaration(member) ||
    ts.isSetAccessorDeclaration(member)
  );
}

export function isContractMember(member: ts.TypeElement): boolean {
  return (
    ts.isMethodSignature(member) ||
    ts.isCallSignatureDeclaration(member) ||
    ts.isConstructSignatureDeclaration(member)
  );
}

export function containsThisAssignment(node: ts.Node): boolean {
  return containsNode(node, (child) => {
    return (
      ts.isBinaryExpression(child) &&
      ts.isPropertyAccessExpression(child.left) &&
      child.left.expression.kind === ts.SyntaxKind.ThisKeyword
    );
  });
}

function containsNode(node: ts.Node, match: (child: ts.Node) => boolean): boolean {
  let found = false;
  const visit = (child: ts.Node): void => {
    if (found || (child !== node && ts.isFunctionLike(child))) return;
    if (match(child)) {
      found = true;
      return;
    }
    ts.forEachChild(child, visit);
  };
  visit(node);
  return found;
}
