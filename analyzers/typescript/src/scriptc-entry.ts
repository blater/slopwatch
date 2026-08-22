import { readFileSync } from "node:fs";
import { analyze } from "@slopslap/typescript-analyzer";

function emit(record: Record<string, unknown>): void {
  console.log(JSON.stringify(record));
}

const input = readFileSync(0, "utf8").trim();
const records = analyze(JSON.parse(input));
for (const record of records) emit(record);
