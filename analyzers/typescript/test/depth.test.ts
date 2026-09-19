import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";

import { analyze } from "../src/analyzer.js";

function runRequest(root: string, source: string, evaluator: string, components = [{ component_id: "module_shallowness", definition_version: "responsibility-burden-v4" }]) {
  fs.writeFileSync(path.join(root, "service.ts"), source);
  return analyze({
    type: "request",
    protocol_version: 1,
    invocation_id: "depth-test",
    workspace: root,
    units: [{ unit_id: "unit", language: "typescript", source_paths: ["service.ts"] }],
    components,
    options: { typescript_types: "off", depth_evaluator_path: evaluator },
  });
}

function depthMeasurement(records: Record<string, unknown>[]): Record<string, unknown> {
  const result = records.find((item) => item.type === "measurement");
  assert.ok(result);
  return result;
}

test("SHALLOW v4 evaluates typed TypeScript scalar functions", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  for (const [source, expected] of [
    ["export function run(x: number): number { return x; }", 100],
    ["export function run(x: number): number { return x + 1; }", 30],
    ["export function run(x: number): number { const y = x + 1; return y; }", 30],
    ["export function run(x: boolean): boolean { return !x; }", 30],
    ["export function run(x: string): string { return x; }", 100],
    ["export function run(x: string): string { return x + ''; }", 100],
    ["export function run(x: string): string { return x + '0'; }", 30],
    ["function suffix(x: string): string { const y = x + '0'; return y; } export function run(x: string): string { return suffix(x); }", 30],
  ] as const) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-depth-"));
    try {
      const measurement = depthMeasurement(runRequest(root, source, evaluator));
      assert.equal(measurement.value, expected);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  }
});

test("SHALLOW v4 reports a missing evaluator as partial and preserves other metrics", () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-depth-"));
  try {
    const source = "export function run(x: number): number { return x + 1; }";
    const missing = runRequest(root, source, path.join(root, "missing-evaluator"));
    assert.equal(depthMeasurement(missing).value, null);
    assert.ok(missing.some((item) => item.type === "diagnostic" && item.code === "typescript.depth_evaluator_failed"));

    const legacy = runRequest(root, source, "", [
      { component_id: "module_shallowness", definition_version: "ousterhout-v3" },
      { component_id: "npath_complexity", definition_version: "pmd-v1" },
    ]);
    const v4 = runRequest(root, source, "/missing-evaluator", [
      { component_id: "module_shallowness", definition_version: "responsibility-burden-v4" },
      { component_id: "npath_complexity", definition_version: "pmd-v1" },
    ]);
    const metric = (records: Record<string, unknown>[]) => records.find((item) => item.type === "measurement" && item.component_id === "npath_complexity")?.value;
    assert.equal(metric(v4), metric(legacy));
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 keeps local export aliases and arrow callables on the same flow", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-depth-"));
  try {
    const records = runRequest(root, "function twice(x: number): number { return x + x; } const run = (x: number): number => twice(x); export { run };", evaluator);
    const measurement = depthMeasurement(records);
    assert.equal(measurement.value, 30);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 admits a default arrow callable as one route", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-depth-"));
  try {
    const measurement = depthMeasurement(runRequest(root, "export default (x: number): number => x;", evaluator));
    assert.equal(measurement.value, 100);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 preserves both alternatives of a scalar ternary", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-depth-"));
  try {
    const measurement = depthMeasurement(runRequest(root, "export function run(mode: boolean, x: number): number { return mode ? x + 1 : x * 2; }", evaluator));
    const depth = measurement.attributes as { depth?: { state?: string; H?: number } };
    assert.equal(typeof measurement.value, "number");
    assert.equal(depth.depth?.state, "measured");
    assert.equal(depth.depth?.H, 2);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 lowers a bounded validation branch without inventing a sequential suffix", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-depth-"));
  try {
    const records = runRequest(root, "export function run(x: number): number { if (x < 0) throw new Error(); return x + 1; }", evaluator);
    const measurement = depthMeasurement(records);
    assert.equal(typeof measurement.value, "number");
    const depth = measurement.attributes as { depth?: { state?: string } };
    assert.equal(depth.depth?.state, "measured");
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 groups checked and raw routes around one resolved computation", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-validation-bypass-"));
  try {
    const source = "function core(x:number):number{return x+1;} export function checked(x:number):number{if(x<0)throw new Error();return core(x);} export function raw(x:number):number{return core(x);}";
    const measurement = depthMeasurement(runRequest(root, source, evaluator));
    const depth = measurement.attributes as { depth?: { H?: number; burden?: { O?: number; A?: number; E?: number } } };
    assert.equal(depth.depth?.H, 2);
    assert.equal(depth.depth?.burden?.O, 1);
    assert.equal(depth.depth?.burden?.A, 1);
    assert.equal(depth.depth?.burden?.E, 0);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 keeps distinct returned SSA expressions in separate families", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-flow-families-"));
  try {
    const source = "export function left(x:number,y:number):number{return x-y;} export function reverse(x:number,y:number):number{return y-x;} export function one():number{return 1;} export function two():number{return 2;}";
    const measurement = depthMeasurement(runRequest(root, source, evaluator));
    const depth = measurement.attributes as { depth?: { burden?: { O?: number } } };
    assert.equal(depth.depth?.burden?.O, 4);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

test("SHALLOW v4 recognizes structural passive result carriers", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) {
    t.skip("set SLOPSLAP_DEPTH_EVALUATOR to the structural evaluator executable");
    return;
  }
  const positive = `
enum Status { Empty, Ready }
class FileToken {}
export class Result {
  private file: FileToken | null = null;
  private status: Status = Status.Empty;
  fileValue(): FileToken | null { return this.file; }
  statusValue(): Status { return this.status; }
  set(file: FileToken | null, status: Status): void { this.file = file; this.status = status; }
  reset(): void { this.file = null; this.status = Status.Empty; }
}`;
  const cases = [
    [positive, true],
    [positive.replace("private file:", "constructor(seed: number = audit()) {} private file:") + " function audit():number { return 1; }", false],
    [positive.replace("this.file = file; this.status = status;", "if (status === Status.Ready) throw new Error(); this.file = file; this.status = status;"), false],
    [positive.replace("statusValue(): Status { return this.status; }", "statusValue(): Status { return this.status === Status.Ready ? Status.Empty : Status.Ready; }"), false],
    [positive.replace("reset(): void { this.file = null; this.status = Status.Empty; }", "reset(): void { this.status = this.status === Status.Empty ? Status.Ready : Status.Empty; }"), false],
  ] as const;
  for (const [source, proven] of cases) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-passive-result-"));
    try {
      const records = runRequest(root, source, evaluator);
      const measurement = depthMeasurement(records);
      const depth = measurement.attributes as { depth?: { evidence?: Array<{ kind?: string; status?: string }> } };
      const marker = depth.depth?.evidence?.some((item) => item.kind === "passive-result-carrier-v1" && item.status === "proven") ?? false;
      assert.equal(marker, proven);
      if (proven) assert.equal(measurement.value, 0);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  }
});

test("SHALLOW v4 exempts data enums but not computation or mixed behavior", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) { t.skip("set SLOPSLAP_DEPTH_EVALUATOR"); return; }
  for (const [source, passive] of [
    ['export enum Status { Empty, Ready = "ready" }', true],
    ['export enum Status { Empty = compute() } function compute() { return 1; }', false],
    ['export enum Status { Empty, Ready } export function compute(x:number) { return x*2; }', false],
    ['export enum Status { Empty, Ready } const registration=audit(); function audit(){ return 1; }', false],
  ] as const) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-enum-"));
    try {
      const measurement = depthMeasurement(runRequest(root, source, evaluator));
      const attributes = measurement.attributes as { depth?: { evidence?: Array<{kind?:string; status?:string}> } };
      const proven = attributes.depth?.evidence?.some(e => e.kind === "passive-enum-v1" && e.status === "proven") ?? false;
      assert.equal(proven, passive);
      if (passive) assert.equal(measurement.value, 0);
    } finally { fs.rmSync(root, {recursive:true, force:true}); }
  }
});

test("SHALLOW v4 recognizes exposed data fields without exempting behavior", (t) => {
  const evaluator = process.env.SLOPSLAP_DEPTH_EVALUATOR ?? "/tmp/slopwatch-depth-v4-host";
  if (!fs.existsSync(evaluator)) { t.skip("set SLOPSLAP_DEPTH_EVALUATOR"); return; }
  for (const [source, passive, kind] of [
    ['export class Point { readonly x:number; constructor(x:number) { this.x=x; } }', true, 'passive-value-object-v1'],
    ['export class Value<T> { constructor(public readonly value:T) {} }', true, 'passive-value-object-v1'],
    ['export class Box<T> { constructor(private readonly value:T) {} getValue():T { return this.value; } }', true, 'passive-result-carrier-v1'],
    ['export class Point { x:number=0; move(dx:number) { this.x+=dx; } }', false, 'passive-value-object-v1'],
    ['export class Point { x:number=0; constructor(seed:number=audit()) {} } function audit(){return 1;}', false, 'passive-value-object-v1'],
  ] as const) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-value-"));
    try {
      const measurement = depthMeasurement(runRequest(root, source, evaluator));
      const attributes = measurement.attributes as { depth?: { evidence?: Array<{kind?:string; status?:string}> } };
      const proven = attributes.depth?.evidence?.some(e => e.kind === kind && e.status === "proven") ?? false;
      assert.equal(proven, passive);
      if (passive) assert.equal(measurement.value, 0);
    } finally { fs.rmSync(root, {recursive:true, force:true}); }
  }
});
