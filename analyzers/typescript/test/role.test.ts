import * as assert from "node:assert/strict";
import { test } from "node:test";
import ts from "typescript";

import { classifyInterfaceRole } from "../src/operations.js";

function classify(source: string) {
  const file = ts.createSourceFile(
    "fixture.ts",
    source,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TS,
  );
  return classifyInterfaceRole(file);
}

test("classifies exported contract interfaces as protocols", () => {
  assert.equal(
    classify("export interface Service { run(request: Request): Response; }")
      .role,
    "protocol",
  );
});

test("classifies input-only operations as commands", () => {
  assert.equal(
    classify("export function run(request: Request): void {}").role,
    "command",
  );
});

test("classifies value-returning operations as queries", () => {
  assert.equal(
    classify(
      "export function find(id: string): Result { return {} as Result; }",
    ).role,
    "query",
  );
});

test("classifies exported state-only surfaces as data", () => {
  assert.equal(
    classify("export interface State { value: number; }").role,
    "data",
  );
});

test("does not infer a role from an unexported surface", () => {
  const result = classify("interface Hidden { run(): void; }");
  assert.equal(result.role, "unknown");
  assert.equal(result.confidence, 0);
});
