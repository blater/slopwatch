import { spawn } from "node:child_process";
import type { DepthBoundary, DepthFacts, EvaluatorResponse } from "./depth-model.js";

const MAX_EVALUATOR_BYTES = 64 * 1024 * 1024;

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object";
}

export function invokeEvaluatorStreaming(
  executable: string,
  facts: DepthFacts,
  onScore: (assessment: Record<string, unknown>, score: Record<string, unknown>) => void,
): Promise<EvaluatorResponse> {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, ["--evaluate-depth-v4-stream"], {
      stdio: ["pipe", "pipe", "pipe"],
    });
    let settled = false;
    let done = false;
    let pending = "";
    let outputBytes = 0;
    let stderr = "";
    const assessments: Record<string, unknown>[] = [];
    const scores: Record<string, unknown>[] = [];
    const finishFailure = (error: Error): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      child.kill("SIGTERM");
      reject(error);
    };
    const fail = (error: Error): void => finishFailure(error);
    const timer = setTimeout(() => fail(new Error("depth evaluator timed out after 120s")), 120_000);
    const parse = (line: string): void => {
      if (line.trim().length === 0) return;
      let frame: unknown;
      try {
        frame = JSON.parse(line);
      } catch (error) {
        throw new Error(`depth evaluator returned invalid JSON: ${error instanceof Error ? error.message : String(error)}`);
      }
      if (!isObject(frame) || typeof frame.type !== "string")
        throw new Error("depth evaluator returned an invalid stream frame");
      if (frame.type === "score") {
        if (done || !isObject(frame.assessment) || !isObject(frame.score))
          throw new Error("depth evaluator returned an invalid score frame");
        assessments.push(frame.assessment);
        scores.push(frame.score);
        onScore(frame.assessment, frame.score);
        return;
      }
      if (frame.type === "done" && frame.schema_version === 1) {
        if (done) throw new Error("depth evaluator returned duplicate done frames");
        done = true;
        return;
      }
      throw new Error("depth evaluator returned an unknown stream frame");
    };
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk: string) => {
      if (settled) return;
      outputBytes += Buffer.byteLength(chunk, "utf8");
      if (outputBytes > MAX_EVALUATOR_BYTES) {
        fail(new Error("depth evaluator output exceeds 64 MiB"));
        return;
      }
      pending += chunk;
      let newline = pending.indexOf("\n");
      try {
        while (newline >= 0) {
          parse(pending.slice(0, newline).replace(/\r$/u, ""));
          pending = pending.slice(newline + 1);
          newline = pending.indexOf("\n");
        }
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    });
    child.stderr.on("data", (chunk: Buffer) => {
      if (stderr.length < 4096) stderr += chunk.toString("utf8").slice(0, 4096 - stderr.length);
    });
    child.stdout.on("error", fail);
    child.stdin.on("error", fail);
    child.on("error", fail);
    child.on("close", (status, signal) => {
      if (settled) return;
      try {
        if (pending.trim().length > 0) parse(pending.trim());
        if (!done) throw new Error("depth evaluator stream ended without done frame");
        if (status !== 0) throw new Error(`depth evaluator exited ${String(status)}${signal === null ? "" : ` (${signal})`}: ${stderr || "unknown error"}`);
        settled = true;
        clearTimeout(timer);
        resolve({ schema_version: 1, assessments: assessments as unknown as DepthBoundary[], scores });
      } catch (error) {
        fail(error instanceof Error ? error : new Error(String(error)));
      }
    });
    child.stdin.end(JSON.stringify({ schema_version: 1, depth: facts }));
  });
}
