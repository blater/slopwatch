import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { afterEach, test } from "node:test";

import { analyze } from "../src/analyzer.js";

const temporaryDirectories: string[] = [];
const fixtureDirectory = path.join(process.cwd(), "test", "fixtures");

function fixture(name: string): string {
  return fs.readFileSync(path.join(fixtureDirectory, name), "utf8");
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    fs.rmSync(directory, { recursive: true, force: true });
  }
});

function workspace(files: Record<string, string>): string {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "slopslap-ts-"));
  temporaryDirectories.push(directory);
  for (const [relative, contents] of Object.entries(files)) {
    const file = path.join(directory, relative);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, contents);
  }
  return directory;
}

function request(
  root: string,
  files: string[],
  components: Array<[string, string]>,
  typescriptTypes: "auto" | "require" | "off" = "auto",
): Record<string, unknown> {
  return {
    type: "request",
    protocol_version: 1,
    invocation_id: "test-run",
    workspace: root,
    units: [{ unit_id: "unit", language: "typescript", source_paths: files }],
    components: components.map(([component_id, definition_version]) => ({
      component_id,
      definition_version,
    })),
    options: { typescript_types: typescriptTypes },
  };
}

function recordsOf(
  records: Record<string, unknown>[],
  type: string,
): Record<string, unknown>[] {
  return records.filter((record) => record.type === type);
}

test("structural kernels share one syntax parse and emit PMD-aligned measurements", () => {
  const root = workspace({
    "src/control.ts": fixture("structural.ts.txt"),
  });
  const records = analyze(
    request(
      root,
      ["src/control.ts"],
      [
        ["cognitive_complexity", "pmd-sonar-v1"],
        ["cyclomatic_method_complexity", "pmd-v1"],
        ["npath_complexity", "pmd-v1"],
        ["deeply_nested_if", "pmd-v1"],
      ],
    ),
  );
  const measurements = recordsOf(records, "measurement");
  const controlCyclo = measurements.find(
    (item) =>
      item.component_id === "cyclomatic_method_complexity" &&
      (item.subject as { name: string }).name === "control",
  );
  assert.equal(controlCyclo?.value, 9);
  const deep = measurements.filter(
    (item) => item.component_id === "deeply_nested_if",
  );
  assert.equal(
    deep.length,
    1,
    "PMD reports one violation and stops the nested chain",
  );

  const coverage = recordsOf(records, "coverage");
  assert.equal(coverage.length, 4);
  assert.ok(coverage.every((item) => item.state === "complete"));
  const plan = recordsOf(records, "execution_plan")[0];
  assert.equal(plan?.discovered_source_count, 1);
  assert.equal(plan?.parsed_source_count, 1);
  assert.deepEqual(plan?.parser_modes, ["typescript-syntax"]);
  assert.equal(records.at(-1)?.status, "success");
});

test("routine evidence uses qualified symbols and precise source ranges", () => {
  const root = workspace({
    "src/service.ts": `
      export class Service {
        constructor() {}
        run(): number {
          const worker = (): number => 1;
          return worker();
        }
      }
    `,
  });
  const records = analyze(
    request(root, ["src/service.ts"], [
      ["cognitive_complexity", "pmd-sonar-v1"],
    ]),
  );
  const measurements = recordsOf(records, "measurement");
  const subjects = measurements.map((item) =>
    item.subject as {
      name: string;
      symbol: string;
      routine: string;
      start: { line: number; column: number; offset: number };
      end: { line: number; column: number; offset: number };
    },
  );
  assert.deepEqual(
    subjects.map((item) => item.symbol),
    ["Service.<init>", "Service.run", "Service.run.worker"],
  );
  assert.ok(subjects.every((item) => item.name === item.symbol));
  assert.ok(subjects.every((item) => item.routine === item.symbol));
  assert.ok(
    subjects.every(
      (item) =>
        item.start.line > 0 &&
        item.start.column > 0 &&
        item.end.offset > item.start.offset,
    ),
  );
});

test("type-level structural metrics match the shared PMD measurement contract", () => {
  const root = workspace({
    "src/service.ts": `
      interface Payload { id: string }
      const external = { value: 1 };
      export class Service {
        private state = 0;
        private payload!: Payload;
        run(value: number): number { if (value > 0) this.state = value; return this.state; }
        report(): void { console.log(external.value); }
      }
    `,
  });
  const records = analyze(
    request(
      root,
      ["src/service.ts"],
      [
        ["cyclomatic_class_complexity", "pmd-v1"],
        ["coupling_between_objects", "pmd-v1"],
        ["god_class", "pmd-v1"],
      ],
    ),
  );
  const measurements = recordsOf(records, "measurement");
  const classMetric = (component: string) =>
    measurements.find(
      (item) =>
        item.component_id === component &&
        (item.subject as { name: string }).name === "Service",
    );
  assert.equal(classMetric("cyclomatic_class_complexity")?.value, 5);
  assert.equal(classMetric("coupling_between_objects")?.value, 1);
  assert.deepEqual(classMetric("god_class")?.attributes, {
    kind: "class",
    wmc: 5,
    atfd: 2,
    tcc: 0,
  });
  assert.ok(
    recordsOf(records, "coverage").every((item) => item.state === "complete"),
  );
});

test("module shallowness reports signature evidence", () => {
  const root = workspace({
    "src/service.ts":
      "export function run(request: { id: string }): { ok: boolean } { return { ok: true }; }\n",
  });
  const records = analyze(
    request(
      root,
      ["src/service.ts"],
      [["module_shallowness", "ousterhout-v3"]],
    ),
  );
  const measurement = recordsOf(records, "measurement")[0];
  const attributes = measurement?.attributes as Record<string, unknown>;
  assert.equal(attributes.capability_basis, "cosmic-inspired-signature");
  assert.equal(attributes.evidence_level, 2);
  assert.equal(attributes.entries, 1);
  assert.equal(attributes.exits, 1);
  assert.equal(attributes.parameter_count, 1);
  assert.equal(attributes.result_count, 1);
});

test("module shallowness applies the role-shaped reference to protocols", () => {
  const root = workspace({
    "src/service.ts":
      "export interface Service { run(request: Request): Response; }\n",
  });
  const records = analyze(
    request(
      root,
      ["src/service.ts"],
      [["module_shallowness", "ousterhout-v3"]],
    ),
  );
  const attributes = recordsOf(records, "measurement")[0]?.attributes as Record<
    string,
    unknown
  >;
  assert.equal(attributes.reference_role, "protocol");
  assert.equal(attributes.reference_policy, "role-shape-v2");
  assert.ok((attributes.depth_reference as number) < 1);
});

test("module shallowness distinguishes immutable state from representation exposure", () => {
  const root = workspace({
    "src/service.ts": `
      export class Service {
        readonly stable = 1;
        mutable = 2;
        private hidden = 3;
        get exposed(): number { return this.mutable; }
        set exposed(value: number) { this.mutable = value; }
      }
    `,
  });
  const records = analyze(
    request(
      root,
      ["src/service.ts"],
      [["module_shallowness", "ousterhout-v3"]],
    ),
  );
  const measurement = recordsOf(records, "measurement")[0];
  const attributes = measurement?.attributes as Record<string, unknown>;
  assert.equal(attributes.exposed_state_cost, 2);
  assert.equal(attributes.exposed_representation, 3);
});

test("module shallowness counts mutable structured signature members as exposure", () => {
  const root = workspace({
    "src/service.ts":
      "export function run(value: { stable: number; readonly id: string }): void {}\n",
  });
  const records = analyze(
    request(
      root,
      ["src/service.ts"],
      [["module_shallowness", "ousterhout-v3"]],
    ),
  );
  const measurement = recordsOf(records, "measurement")[0];
  const attributes = measurement?.attributes as Record<string, unknown>;
  assert.equal(attributes.exposed_representation, 1);
});

test("NPath is capped at the supported maximum", () => {
  const decisions = Array.from(
    { length: 55 },
    (_, index) => `if (v${index}) {}`,
  ).join("\n");
  const parameters = Array.from(
    { length: 55 },
    (_, index) => `v${index}: boolean`,
  ).join(", ");
  const root = workspace({
    "huge.mts": `export function huge(${parameters}) { ${decisions} }`,
  });
  const records = analyze(
    request(root, ["huge.mts"], [["npath_complexity", "pmd-v1"]]),
  );
  const measurement = recordsOf(records, "measurement")[0];
  assert.equal(measurement?.value, 9999);
});

test("typed kernels cover all initial local sink components with deterministic dedup", () => {
  const root = workspace({
    "src/risk.cts": fixture("risk.cts.txt"),
  });
  const typedComponents = [
    "explicit_any",
    "unsafe_type_assertion",
    "unsafe_type_propagation",
    "unsafe_type_use",
    "unsafe_type_boundary",
    "ambiguous_boolean_expression",
    "non_exhaustive_union",
  ];
  const records = analyze(
    request(
      root,
      ["src/risk.cts"],
      typedComponents.map((item) => [item, "typescript-local-sink-v1"]),
    ),
  );
  const measurements = recordsOf(records, "measurement");
  assert.deepEqual(
    [...new Set(measurements.map((item) => item.component_id))].sort(),
    [...typedComponents].sort(),
  );
  const keys = measurements.map((item) => {
    const found = item as {
      component_id: string;
      path: string;
      subject: { start: { offset: number }; end: { offset: number } };
      attributes: { normalized_symbol: string };
    };
    return [
      found.component_id,
      found.path,
      found.subject.start.offset,
      found.subject.end.offset,
      found.attributes.normalized_symbol,
    ].join("|");
  });
  assert.equal(new Set(keys).size, keys.length);
  assert.equal(
    measurements.filter((item) => item.component_id === "unsafe_type_use")
      .length,
    1,
    "value.missing() collapses member and call candidates to the outer call sink",
  );
  assert.ok(
    recordsOf(records, "coverage").every((item) => item.state === "complete"),
  );
  assert.deepEqual(recordsOf(records, "execution_plan")[0]?.parser_modes, [
    "typescript-syntax",
    "typescript-compiler",
  ]);
  assert.equal(
    recordsOf(records, "execution_plan")[0]?.parsed_source_count,
    1,
    "the compiler program reuses the structural syntax tree",
  );
});

test("missing declarations retain structural results and expose unavailable typed coverage", () => {
  const root = workspace({
    "src/missing.ts": fixture("missing.ts.txt"),
  });
  const records = analyze(
    request(
      root,
      ["src/missing.ts"],
      [
        ["cyclomatic_method_complexity", "pmd-v1"],
        ["unsafe_type_use", "typescript-local-sink-v1"],
      ],
    ),
  );
  assert.ok(
    recordsOf(records, "measurement").some(
      (item) => item.component_id === "cyclomatic_method_complexity",
    ),
  );
  const typedCoverage = recordsOf(records, "coverage").find(
    (item) => item.component_id === "unsafe_type_use",
  );
  assert.equal(typedCoverage?.state, "unavailable");
  assert.match(String(typedCoverage?.reason), /not trustworthy/u);
  assert.equal(records.at(-1)?.status, "success");

  const required = analyze(
    request(
      root,
      ["src/missing.ts"],
      [["unsafe_type_use", "typescript-local-sink-v1"]],
      "require",
    ),
  );
  assert.equal(required.at(-1)?.status, "failure");
  assert.deepEqual(required.at(-1)?.failed_unit_ids, ["unit"]);
});

test("syntax failure is never represented as complete zero-score coverage", () => {
  const root = workspace({ "broken.ts": "export function broken( {\n" });
  const records = analyze(
    request(
      root,
      ["broken.ts"],
      [
        ["cognitive_complexity", "pmd-sonar-v1"],
        ["unsafe_type_use", "typescript-local-sink-v1"],
      ],
    ),
  );
  assert.equal(records.at(-1)?.status, "failure");
  assert.ok(
    recordsOf(records, "coverage").every((item) => item.state !== "complete"),
  );
  assert.equal(recordsOf(records, "measurement").length, 0);
});

test("multiple tsconfig ownership candidates make typed coverage unavailable", () => {
  const root = workspace({
    "a/tsconfig.json": JSON.stringify({ compilerOptions: { strict: true } }),
    "a/a.ts": "export const a: string = 'a';\n",
    "b/tsconfig.json": JSON.stringify({ compilerOptions: { strict: true } }),
    "b/b.ts": "export const b: string = 'b';\n",
  });
  const records = analyze(
    request(
      root,
      ["a/a.ts", "b/b.ts"],
      [["unsafe_type_use", "typescript-local-sink-v1"]],
    ),
  );
  assert.ok(
    recordsOf(records, "coverage").every(
      (item) => item.state === "unavailable",
    ),
  );
  assert.ok(
    recordsOf(records, "diagnostic").some(
      (item) => item.code === "typescript.multiple_projects",
    ),
  );
});

test("canonical inventory rejects files outside the workspace and duplicate owners", () => {
  const root = workspace({ "inside.tsx": "export const ok = true;" });
  const outside = workspace({ "outside.ts": "export const no = true;" });
  const records = analyze(
    request(
      root,
      [path.join(outside, "outside.ts")],
      [["npath_complexity", "pmd-v1"]],
    ),
  );
  assert.equal(records.at(-1)?.status, "failure");
  assert.ok(
    recordsOf(records, "diagnostic").some(
      (item) => item.code === "typescript.source_inventory_failed",
    ),
  );

  const duplicate = analyze({
    ...request(root, ["inside.tsx"], [["npath_complexity", "pmd-v1"]]),
    units: [
      {
        unit_id: "first",
        language: "typescript",
        source_paths: ["inside.tsx"],
      },
      {
        unit_id: "second",
        language: "typescript",
        source_paths: ["inside.tsx"],
      },
    ],
  });
  assert.equal(duplicate.at(-1)?.status, "failure");
  assert.match(
    String(recordsOf(duplicate, "diagnostic")[0]?.message),
    /owned by both/u,
  );
});
