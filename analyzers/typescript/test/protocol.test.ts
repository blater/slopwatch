import * as assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

test("CLI accepts a one-record NDJSON request and emits one terminal record last", () => {
  const workspace = fs.mkdtempSync(
    path.join(os.tmpdir(), "slopslap-ts-protocol-"),
  );
  try {
    fs.writeFileSync(
      path.join(workspace, "entry.ts"),
      "export const entry = (): number => 1;\n",
    );
    const request = {
      type: "request",
      protocol_version: 1,
      invocation_id: "protocol-test",
      workspace,
      units: [
        { unit_id: "unit", language: "typescript", source_paths: ["entry.ts"] },
      ],
      components: [
        { component_id: "npath_complexity", definition_version: "pmd-v1" },
      ],
      options: { typescript_types: "auto" },
    };
    const cli = fileURLToPath(new URL("../src/cli.js", import.meta.url));
    const result = spawnSync(process.execPath, [cli], {
      input: `${JSON.stringify(request)}\n`,
      encoding: "utf8",
    });
    assert.equal(result.status, 0, result.stderr);
    const records = result.stdout
      .trim()
      .split("\n")
      .map((line) => JSON.parse(line) as Record<string, unknown>);
    assert.equal(records.at(-1)?.type, "terminal");
    assert.equal(
      records.filter((record) => record.type === "terminal").length,
      1,
    );
    assert.ok(
      records.every((record) => record.invocation_id === "protocol-test"),
    );
  } finally {
    fs.rmSync(workspace, { recursive: true, force: true });
  }
});
