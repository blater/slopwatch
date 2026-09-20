import {
  analyzeSyntaxSources,
  analyzeTypedSources,
  emitFileProgress,
  finishAnalysis,
} from "./analyzer.js";
import {
  prepareAnalysis,
  responseDiagnostic,
  type PreparedAnalysis,
  type ResponseRecord,
} from "./analysis-preparation.js";
import { analyzeDepthStreaming } from "./depth-stream.js";
import type { TypedContext } from "./context.js";

function prepared(value: PreparedAnalysis | ResponseRecord[]): value is PreparedAnalysis {
  return !Array.isArray(value);
}

export async function analyzeStreaming(
  input: unknown,
  emitProgress?: (record: ResponseRecord) => void,
): Promise<ResponseRecord[]> {
  const value = prepareAnalysis(input, emitProgress);
  if (!prepared(value)) return value;
  const { request, context, invocationId, requested, requestedStructural,
    requestedTyped, requestedDepth, requestedSyntax, typeMode, buffers } = value;
  const progress = request.options?.stream_results === true ? emitProgress : undefined;
  analyzeSyntaxSources(context, invocationId, requestedSyntax, buffers, progress);
  let typedUnavailableReason: string | undefined;
  let typedContext: TypedContext | undefined;
  if (requestedTyped.size > 0 || requestedDepth) {
    const typedResult = analyzeTypedSources(
      request,
      context,
      invocationId,
      requestedTyped,
      typeMode,
      buffers,
      progress,
      requestedDepth,
    );
    typedUnavailableReason = typedResult.unavailableReason;
    typedContext = typedResult.context;
  }
  if (requestedDepth) {
    const depth = await analyzeDepthStreaming(
      request,
      context,
      typedContext,
      invocationId,
      progress,
      progress === undefined
        ? undefined
        : (entry, measurements, coverage, complete) =>
            emitFileProgress(progress, invocationId, entry, measurements, coverage, complete),
    );
    for (const item of depth.diagnostics)
      buffers.records.push(responseDiagnostic(invocationId, item));
    buffers.measurements.push(...depth.measurements);
    buffers.coverage.push(...depth.coverage);
  }
  return finishAnalysis(
    request,
    context,
    invocationId,
    requested,
    requestedTyped,
    typeMode,
    typedUnavailableReason,
    requestedDepth,
    buffers,
  );
}
