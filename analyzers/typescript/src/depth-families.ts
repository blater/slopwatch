import ts from "typescript";
import type { DepthFlowFunction } from "./depth-model.js";

export function flowFamilyKey(flow: DepthFlowFunction): string {
  if (flow.blocks.length === 0 || flow.results.length === 0) return `unique:${flow.id}`;
  const validationGuard = simpleValidationGuardFlow(flow);
  if (!validationGuard && (flow.blocks.length !== 1 || flow.blocks[0]?.edges.length !== 0)) return `unique:${flow.id}`;
  const definitions = new Map<string, Record<string, unknown>>();
  const returns: string[] = [];
  for (const block of flow.blocks) {
    for (const raw of block.instructions) {
      if (typeof raw !== "object" || raw === null) return `unique:${flow.id}`;
      const instruction = raw as Record<string, unknown>;
      if (typeof instruction.id !== "string" || typeof instruction.opcode !== "string") return `unique:${flow.id}`;
      if (instruction.opcode === "call" || instruction.opcode === "phi" || instruction.opcode === "unknown") return `unique:${flow.id}`;
      if (instruction.opcode === "branch" && !validationGuard) return `unique:${flow.id}`;
      if (instruction.opcode === "throw" && !validationGuard) return `unique:${flow.id}`;
      definitions.set(instruction.id, instruction);
      if (instruction.opcode === "return") {
        const operands = instruction.operands;
        if (!Array.isArray(operands) || operands.length !== 1 || typeof operands[0] !== "string") return `unique:${flow.id}`;
        returns.push(operands[0]);
      }
    }
  }
  // The bounded canonical form is intentionally a single returned SSA
  // outcome. Multiple return outcomes need selector/path information; treating
  // their set as unordered would merge conditionally distinct computations.
  if (returns.length !== 1) return `unique:${flow.id}`;
  const visiting = new Set<string>();
  const memo = new Map<string, string>();
  let budget = 512;
  const expression = (id: string): string | undefined => {
    const cached = memo.get(id);
    if (cached !== undefined) return cached;
    if (--budget < 0 || visiting.size >= 64) return undefined;
    if (id.startsWith("arg") && /^arg\d+$/.test(id)) return id;
    const instruction = definitions.get(id);
    if (instruction === undefined || visiting.has(id)) return undefined;
    visiting.add(id);
    let result: string | undefined;
    switch (instruction.opcode) {
      case "constant": {
        const value = instruction.value;
        if (typeof value !== "object" || value === null || !("constant" in value)) break;
        result = `constant:${JSON.stringify((value as Record<string, unknown>).constant)}`;
        break;
      }
      case "primitive": {
        if (typeof instruction.operator !== "string" || typeof instruction.type !== "string" || !Array.isArray(instruction.operands)) break;
        const operands = instruction.operands.map((operand) => typeof operand === "string" ? expression(operand) : undefined);
        if (operands.some((operand) => operand === undefined)) break;
        if (operands.reduce((size, operand) => size + (operand?.length ?? 0), 0) > 8192) break;
        result = `primitive:${instruction.operator}:${instruction.type}(${operands.join(",")})`;
        break;
      }
      default:
        break;
    }
    visiting.delete(id);
    if (result !== undefined && result.length <= 16384) memo.set(id, result);
    else result = undefined;
    return result;
  };
  const outcomes = returns.map(expression);
  if (outcomes.some((outcome) => outcome === undefined)) return `unique:${flow.id}`;
  return JSON.stringify({
    parameters: flow.formals.map((formal) => formal.value_kind),
    results: flow.results.map((result) => result.value_kind),
    outcomes: [...new Set(outcomes)].sort(),
  });
}

function simpleValidationGuardFlow(flow: DepthFlowFunction): boolean {
  if (flow.blocks.length !== 3) return false;
  const entry = flow.blocks[0];
  const edgeKind = (edge: unknown): unknown =>
    typeof edge === "object" && edge !== null ? (edge as Record<string, unknown>).kind : undefined;
  if (entry === undefined || entry.edges.length !== 2 || !entry.edges.every((edge) => edgeKind(edge) === "true" || edgeKind(edge) === "false")) return false;
  const branches = flow.blocks.slice(1);
  if (branches.some((block) => block.edges.length !== 0)) return false;
  const throwing = branches.filter((block) => block.instructions.length === 1 && block.instructions[0] !== undefined && typeof block.instructions[0] === "object" && block.instructions[0] !== null && (block.instructions[0] as Record<string, unknown>).opcode === "throw");
  if (throwing.length !== 1) return false;
  const normal = branches.find((block) => block !== throwing[0]);
  return normal !== undefined && normal.instructions.some((instruction) => typeof instruction === "object" && instruction !== null && (instruction as Record<string, unknown>).opcode === "return") && !normal.instructions.some((instruction) => typeof instruction === "object" && instruction !== null && ["branch", "phi", "throw", "call", "unknown"].includes(String((instruction as Record<string, unknown>).opcode)));
}
