#!/usr/bin/env node
import { readFileSync } from "node:fs";

import { analyze } from "./analyzer.js";

function emit(records: ReadonlyArray<Record<string, unknown>>): void {
  const batchSize = 256;
  for (let start = 0; start < records.length; start += batchSize) {
    const end = Math.min(records.length, start + batchSize);
    let output = "";
    for (let index = start; index < end; index++)
      output += `${JSON.stringify(records[index])}\n`;
    process.stdout.write(output);
  }
}

function main(): void {
  const input = readFileSync(0, "utf8").trim();
  let parsed: unknown;
  try {
    parsed = JSON.parse(input);
  } catch (error) {
    // Protocol v1 is one NDJSON request record. Pretty-printed JSON is also accepted.
    const lines = input
      .split(/\r?\n/u)
      .filter((line) => line.trim().length > 0);
    if (lines.length !== 1) {
      parsed = {
        type: "invalid",
        parse_error: error instanceof Error ? error.message : String(error),
      };
    } else {
      try {
        parsed = JSON.parse(lines[0] ?? "");
      } catch (lineError) {
        parsed = {
          type: "invalid",
          parse_error:
            lineError instanceof Error ? lineError.message : String(lineError),
        };
      }
    }
  }
  const records = analyze(parsed);
  emit(records);
  const terminal = records.at(-1);
  if (terminal?.status === "failure") process.exitCode = 1;
}

main();
