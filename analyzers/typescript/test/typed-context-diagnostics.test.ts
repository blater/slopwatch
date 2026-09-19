import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";
import { analyze } from "../src/analyzer.js";

function evaluate(source: string, config: string) {
  const workspace = fs.mkdtempSync(path.join(os.tmpdir(), "slopwatch-ts-rootdir-"));
  try {
    fs.mkdirSync(path.join(workspace, "analyzer/src"), { recursive: true });
    fs.mkdirSync(path.join(workspace, "snapshots"));
    fs.writeFileSync(path.join(workspace, "analyzer/tsconfig.json"), config);
    fs.writeFileSync(path.join(workspace, "analyzer/src/inside.ts"), "export const value = 1;");
    fs.writeFileSync(path.join(workspace, "snapshots/example.ts"), source);
    return analyze({
      type: "request", protocol_version: 1, invocation_id: "rootdir-regression", workspace,
      units: [{ unit_id: "snapshot", language: "typescript", source_paths: ["snapshots/example.ts"] }],
      components: [
        { component_id: "cyclomatic_method_complexity", definition_version: "pmd-v1" },
        { component_id: "unsafe_type_use", definition_version: "typescript-local-sink-v1" },
      ],
      options: { typescript_types: "require", tsconfig: "analyzer/tsconfig.json" },
    });
  } finally { fs.rmSync(workspace, { recursive: true, force: true }); }
}
const validConfig = JSON.stringify({
  compilerOptions: { strict: true, rootDir: "src", outDir: "dist", target: "ES2022", module: "NodeNext", moduleResolution: "NodeNext" },
  include: ["src/**/*.ts"],
});

test("emit-only rootDir diagnostics are log-only while exact snapshot analysis remains complete", () => {
  const records = evaluate("export function example(value: number): number { return value + 1; }", validConfig);
  const diagnostics = records.filter((record) => record.type === "diagnostic");
  const layout = diagnostics.find((record) => record.code === "typescript.compiler.6059");
  assert.ok(layout, "retain the actual compiler detail for the diagnostic log");
  assert.equal(layout.severity, "info");
  assert.deepEqual(layout.attributes, { log_only: true, classification: "incidental_emit_layout" });
  assert.equal(diagnostics.filter((record) => record.severity !== "info" || (String(record.code).startsWith("typescript.compiler.") && !(record.attributes as Record<string, unknown> | undefined)?.log_only)).length, 0, JSON.stringify(diagnostics));
  const coverage = records.filter((record) => record.type === "coverage");
  assert.equal(coverage.length, 2);
  assert.ok(coverage.every((record) => record.state === "complete"));
  assert.equal(records.at(-1)?.status, "success");
  assert.ok(records.some((record) => record.type === "measurement" && record.component_id === "cyclomatic_method_complexity"));
});

test("rootDir log classification does not suppress real semantic errors", () => {
  const records = evaluate("export const value: number = 'wrong';", validConfig);
  assert.ok(records.some((record) => record.type === "diagnostic" && record.code === "typescript.compiler.2322" && record.severity === "error"));
  assert.ok(records.some((record) => record.type === "coverage" && record.component_id === "unsafe_type_use" && record.state !== "complete"));
  assert.equal(records.at(-1)?.status, "failure");
});

test("unusable configuration and invalid syntax remain observable failures", () => {
  for (const [source, config] of [
    ["export function broken( {", validConfig],
    ["export const value=1;", JSON.stringify({ compilerOptions: { module: "not-a-module-kind" } })],
  ]) {
    const records = evaluate(source!, config!);
    assert.ok(records.some((record) => record.type === "diagnostic" && record.severity === "error"));
    assert.ok(records.some((record) => record.type === "coverage" && record.state !== "complete"));
  }
});
