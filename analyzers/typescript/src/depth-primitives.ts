import type { ScalarKind } from "./depth-model.js";

type ScalarValue = { id: string; kind: ScalarKind };

// Emit a primitive only after both operands have been lowered, preserving
// evaluation order and the distinction between text and scalar arithmetic.
export function lowerBinaryPrimitive(
  left: ScalarValue | undefined,
  right: ScalarValue | undefined,
  operator: string,
  kind: ScalarKind,
  type: string,
  next: { value: number },
  instructions: Record<string, unknown>[],
): ScalarValue | undefined {
  const supported = new Set(["+", "-", "*", "/", "%", "<", "<=", ">", ">=", "==", "===", "!=", "!=="]);
  if (left === undefined || right === undefined || !supported.has(operator)) return undefined;
  if (kind === "string" && left.kind === "string" && right.kind === "string" && operator === "+") {
    const id = `n${++next.value}`;
    instructions.push({ id, opcode: "primitive", type: "string", value_kind: "string", arithmetic_mode: "text", operator, operands: [left.id, right.id], results: [id] });
    return { id, kind };
  }
  if (kind === "unknown" || (left.kind !== "numeric" && left.kind !== "boolean") || (right.kind !== "numeric" && right.kind !== "boolean")) return undefined;
  const id = `n${++next.value}`;
  const booleanComparison =
    kind === "boolean" &&
    left.kind === "boolean" &&
    right.kind === "boolean" &&
    ["==", "===", "!=", "!=="].includes(operator);
  instructions.push({
    id,
    opcode: "primitive",
    type,
    value_kind: kind,
    arithmetic_mode: booleanComparison ? "boolean" : "js_number",
    operator: operator === "===" ? "==" : operator === "!==" ? "!=" : operator,
    operands: [left.id, right.id],
    results: [id],
  });
  return { id, kind };
}
