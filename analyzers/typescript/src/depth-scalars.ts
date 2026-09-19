import ts from "typescript";
import type { SourceEntry } from "./model.js";
import type { ScalarKind, FunctionLike } from "./depth-model.js";

export function scalarKind(checker: ts.TypeChecker, type: ts.Type): ScalarKind {
  if (type.isUnion()) {
    const members = type.types.map((member) => scalarKind(checker, member));
    if (members.length > 0 && members.every((member) => member === "numeric")) return "numeric";
    if (members.length > 0 && members.every((member) => member === "boolean")) return "boolean";
    if (members.length > 0 && members.every((member) => member === "string")) return "string";
    return "unknown";
  }
  if (
    (type.flags & (ts.TypeFlags.Number | ts.TypeFlags.NumberLiteral)) !== 0
  )
    return "numeric";
  if (
    (type.flags & (ts.TypeFlags.Boolean | ts.TypeFlags.BooleanLiteral)) !== 0
  )
    return "boolean";
  if ((type.flags & (ts.TypeFlags.String | ts.TypeFlags.StringLiteral)) !== 0)
    return "string";
  return "unknown";
}

export function scalarType(kind: ScalarKind): string {
  return kind === "boolean" ? "boolean" : kind === "numeric" ? "number" : kind === "string" ? "string" : "unknown";
}

export function sourceSignature(checker: ts.TypeChecker, node: FunctionLike): string | null {
  const signature = checker.getSignatureFromDeclaration(node);
  if (signature === undefined) return null;
  const typeText = (type: ts.Type, location: ts.Node): string => checker.typeToString(type, location, ts.TypeFormatFlags.NoTruncation);
  const parameters = node.parameters.map((parameter) => typeText(checker.getTypeAtLocation(parameter), parameter));
  return `(${parameters.join(",")})->${typeText(checker.getReturnTypeOfSignature(signature), node)}`;
}

export function sourceKind(checker: ts.TypeChecker, node: ts.Node): ScalarKind {
  return scalarKind(checker, checker.getTypeAtLocation(node));
}

export function functionID(entry: SourceEntry, node: FunctionLike, fallback = "<anonymous>"): string {
  const name = "name" in node && node.name !== undefined && ts.isIdentifier(node.name) ? node.name.text : fallback;
  return `${entry.relativePath}#${name}`;
}

export function constantInstruction(id: string, type: string, kind: ScalarKind, value: string): Record<string, unknown> {
  return {
    id,
    opcode: "constant",
    type,
    value_kind: kind,
    value: { type, value_kind: kind, constant: value },
    operands: [],
    results: [id],
  };
}

export function callableSymbol(checker: ts.TypeChecker, node: FunctionLike): ts.Symbol | undefined {
  if ("name" in node && node.name !== undefined && ts.isIdentifier(node.name)) return checker.getSymbolAtLocation(node.name);
  return undefined;
}
