import { analyzeSourceSurface } from "./depth-surface.js";
import ts from "typescript";
import type { AnalysisContext, TypedContext } from "./context.js";
import type { SourceEntry } from "./model.js";
import { passiveCarrierEvidence } from "./passive-result-carrier.js";
import type { BoundaryIdentity, DepthBoundary, DepthFlowFunction, LocalCallable, DepthFlowArtifact, FlowFamily, DepthFacts } from "./depth-model.js";
import { sourceSignature, callableSymbol } from "./depth-scalars.js";
import { harmlessUnexportedClass, simpleEnum } from "./depth-callables.js";
import { lowerFunction } from "./depth-function.js";
import { flowFamilyKey } from "./depth-families.js";

export function buildFacts(context: AnalysisContext, typed: TypedContext): { facts: DepthFacts; entries: SourceEntry[] } {
  const boundaries: DepthBoundary[] = [];
  const flows: DepthFlowArtifact[] = [];
  const entries: SourceEntry[] = [];
  for (const entry of context.sources) {
    if (entry.isDeclaration || entry.syntaxErrors.length > 0) continue;
    const source = typed.sourceFiles.get(entry.absolutePath);
    if (source === undefined) continue;
    const surface = analyzeSourceSurface(entry, source, typed.checker);
    const { callables, symbols, passiveEnums, provenPassive, provenPassiveNodes, hasDefaultCallable, exported, unsupportedExport } = surface;
    if (exported.length === 0 && !unsupportedExport && provenPassive.length === 0 && passiveEnums.length === 0) continue;
    entries.push(entry);
    const identity: BoundaryIdentity = { artifact: entry.relativePath, audience: "external", view: "module", symbol: entry.relativePath };
    const flowFunctions: DepthFlowFunction[] = [];
    const families: DepthBoundary["route_families"] = [];
    const familyByKey = new Map<string, FlowFamily>();
    const concepts = new Set<string>();
    let complete = !unsupportedExport;
    for (const statement of source.statements) {
      if (
        (ts.isImportDeclaration(statement) && statement.importClause !== undefined) ||
        ts.isImportEqualsDeclaration(statement) ||
        ts.isFunctionDeclaration(statement) ||
        ts.isVariableStatement(statement) ||
        ts.isInterfaceDeclaration(statement) ||
        ts.isTypeAliasDeclaration(statement) ||
        ts.isEnumDeclaration(statement) && simpleEnum(statement) ||
        ts.isClassDeclaration(statement) && (provenPassiveNodes.has(statement) || harmlessUnexportedClass(statement, typed.checker)) ||
        (ts.isExportDeclaration(statement) && statement.exportClause !== undefined && ts.isNamedExports(statement.exportClause)) ||
        (ts.isExportAssignment(statement) && (ts.isIdentifier(statement.expression) || hasDefaultCallable))
      )
        continue;
      complete = false;
    }
    const helperMap = new Map<ts.Symbol, LocalCallable>();
    for (const callable of callables) {
      const symbol = callableSymbol(typed.checker, callable.node) ??
        (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
          ? typed.checker.getSymbolAtLocation(callable.node.parent.name) : undefined);
      if (symbol !== undefined) helperMap.set(symbol, callable);
    }
    if ((exported.length === 0 && provenPassive.length === 0 && passiveEnums.length === 0) || unsupportedExport) {
      const id = `${entry.relativePath}#<unsupported-export>`;
      flowFunctions.push({
        id,
        entry: "entry",
        formals: [],
        results: [{ id: "result0", type: "unknown", concept: "unknown", value_kind: "unknown" }],
        blocks: [{ id: "entry", instructions: [{ id: "unknown", opcode: "unknown", type: "unknown", value_kind: "unknown", operands: [], results: ["unknown"] }, { id: "return", opcode: "return", operands: ["unknown"] }], edges: [] }],
      });
      families.push({ id, routes: [{ id, family: id, target_function_id: id, boundary: identity, signature: null, required_slots: [], exposed_slots: [] }] });
      concepts.add("unknown");
      complete = false;
    }
    for (const callable of callables) {
      const lowered = lowerFunction(entry, typed.checker, callable.node, helperMap, callable.id);
      flowFunctions.push(lowered.flow);
      const symbol = callableSymbol(typed.checker, callable.node) ??
        (ts.isVariableDeclaration(callable.node.parent) && ts.isIdentifier(callable.node.parent.name)
          ? typed.checker.getSymbolAtLocation(callable.node.parent.name) : undefined);
      const isExported = callable.id.endsWith("#default") || (symbol !== undefined && symbols.has(symbol));
      if (isExported) complete &&= lowered.supported;
      if (!isExported) continue;
      for (const concept of lowered.concepts) concepts.add(concept);
      const signature = sourceSignature(typed.checker, callable.node);
      const key = flowFamilyKey(lowered.flow);
      let family = familyByKey.get(key);
      if (family === undefined) {
        family = { id: lowered.flow.id, routes: [] };
        familyByKey.set(key, family);
        families.push(family);
      }
      // Routes in one proven family must expose the same canonical slots. Using
      // the per-flow id here makes an otherwise-equivalent checked/raw pair
      // look like two independent exposed responsibilities to the evaluator.
      const slots = callable.node.parameters.map((_, index) => `${family.id}/arg${index}`);
      family.routes.push({ id: lowered.flow.id, family: family.id, target_function_id: lowered.flow.id, boundary: identity, signature, required_slots: slots, exposed_slots: slots });
    }
    for (const carrier of provenPassive) {
      const name = carrier.node.name?.text ?? "<anonymous-class>";
      const id = `${entry.relativePath}#${name}`;
      families.push({ id, routes: [{ id, family: id, target_function_id: id, boundary: identity, signature: null, required_slots: [], exposed_slots: [] }] });
    }
    for (const enumeration of passiveEnums) {
      const id = `${entry.relativePath}#${enumeration.name.text}`;
      families.push({ id, routes: [{ id, family: id, target_function_id: id, boundary: identity, signature: null, required_slots: [], exposed_slots: [] }] });
    }
    const passiveOnly = (provenPassive.length > 0 || passiveEnums.length > 0) && exported.length === 0 && !unsupportedExport && complete
      && !source.statements.some(ts.isVariableStatement);
    const knowledgeState = complete ? "measured" : "partial";
    const knowledge = Object.fromEntries(["inventory", "burden", "behavior", "alias_effects"].map((key) => [key, { state: knowledgeState, essential: true }]));
    const boundary: DepthBoundary = {
      identity,
      state: knowledgeState,
      knowledge,
      burden: { O: families.length, T: concepts.size, A: 0, E: 0, P: 0, S: 0, L: 0 },
      concepts: [...concepts].sort().map((id) => ({ id, kind: id, children: [] })),
      slots: [],
      route_families: families,
      files: [entry.relativePath],
    };
    if (passiveOnly) boundary.evidence = [
      ...provenPassive.map((carrier) => passiveCarrierEvidence(source, carrier.proof!)),
      ...passiveEnums.map((enumeration) => ({ id: `${entry.relativePath}#${enumeration.name.text}/passive-enum`, kind: "passive-enum-v1", status: "proven", details: { variants: enumeration.members.length }, provenance: [] })),
    ];
    boundaries.push(boundary);
    flows.push({ artifact: entry.relativePath, language: "typescript", functions: flowFunctions, public_routes: [] });
  }
  return { facts: { boundaries, flows, creations: [], reasons: [] }, entries };
}
