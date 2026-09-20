import * as fs from "node:fs";
import * as path from "node:path";
import ts from "typescript";

import type {
  AnalyzerRequest,
  Diagnostic,
  SourceEntry,
  TypeMode,
} from "./model.js";
import { createTypedContext } from "./typed-context.js";

const SUPPORTED_EXTENSIONS = new Set([".ts", ".tsx", ".mts", ".cts"]);

export interface TypedContext {
  program: ts.Program;
  checker: ts.TypeChecker;
  sourceFiles: Map<string, ts.SourceFile>;
  compilerOptions: ts.CompilerOptions;
  tsconfigPath?: string;
}

export interface TypedContextResult {
  context?: TypedContext;
  diagnostics: Diagnostic[];
  unavailableReason?: string;
}

export interface SourceInventoryIssue {
  unitId: string;
  path: string;
  message: string;
}

function posixPath(value: string): string {
  return value.split(path.sep).join("/");
}

function isDeclarationFile(file: string): boolean {
  return /\.d\.(?:ts|mts|cts)$/iu.test(file);
}

function scriptKind(file: string): ts.ScriptKind {
  return file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
}

function formatTsDiagnostic(item: ts.Diagnostic): string {
  return ts.flattenDiagnosticMessageText(item.messageText, "\n");
}

export class AnalysisContext {
  readonly workspace: string;
  readonly sources: SourceEntry[];
  readonly diagnostics: Diagnostic[] = [];
  readonly inventoryIssues: SourceInventoryIssue[] = [];
  readonly syntaxParseCounts = new Map<string, number>();
  readonly typedParseCounts = new Map<string, number>();
  typedProgramCreated = false;

  private constructor(workspace: string, sources: SourceEntry[]) {
    this.workspace = workspace;
    this.sources = sources;
  }

  static create(
    request: AnalyzerRequest,
    onSource?: (completed: number, total: number) => void,
  ): AnalysisContext {
    const workspace = path.resolve(request.workspace);
    const seen = new Map<string, string>();
    const sources: SourceEntry[] = [];
    const parseCounts = new Map<string, number>();
    const inventoryIssues: SourceInventoryIssue[] = [];

    const total = request.units.reduce(
      (count, unit) => count + unit.source_paths.length,
      0,
    );
    let completed = 0;
    for (const unit of request.units) {
      validateTypeScriptUnit(unit);
      for (const requestedPath of unit.source_paths) {
        try {
          let source: SourceEntry | undefined;
          try {
            source = loadSource(
              workspace,
              unit.unit_id,
              requestedPath,
              seen,
              parseCounts,
            );
          } catch (error) {
            if (error instanceof SourceInventoryError) {
              const relative = path.relative(workspace, error.absolutePath);
              const location =
                relative === "" ||
                (relative !== ".." &&
                  !relative.startsWith(`..${path.sep}`) &&
                  !path.isAbsolute(relative))
                  ? posixPath(relative)
                  : requestedPath;
              inventoryIssues.push({
                unitId: unit.unit_id,
                path: location,
                message: error.message,
              });
            } else {
              throw error;
            }
          }
          if (source !== undefined) sources.push(source);
        } finally {
          completed++;
          onSource?.(completed, total);
        }
      }
    }

    sources.sort((a, b) => a.relativePath.localeCompare(b.relativePath));
    const context = new AnalysisContext(workspace, sources);
    context.inventoryIssues.push(...inventoryIssues);
    for (const [file, count] of parseCounts)
      context.syntaxParseCounts.set(file, count);
    return context;
  }

  createTypedContext(
    request: AnalyzerRequest,
    mode: TypeMode,
  ): TypedContextResult {
    return createTypedContext(this, request, mode);
  }

  public canonicalConfigPath(configured: string): string {
    const candidate = path.isAbsolute(configured)
      ? configured
      : path.resolve(this.workspace, configured);
    return path.resolve(candidate);
  }

  public configFailure(
    items: readonly ts.Diagnostic[],
    config: string,
  ): TypedContextResult {
    return {
      diagnostics: items.map((item) => ({
        code: `typescript.config.${item.code}`,
        severity: "error" as const,
        message: `${posixPath(path.relative(this.workspace, config))}: ${formatTsDiagnostic(item)}`,
      })),
      unavailableReason: "unusable TypeScript project configuration",
    };
  }

  public canonicalKey(file: string): string {
    const resolved = path.resolve(file);
    return ts.sys.useCaseSensitiveFileNames ? resolved : resolved.toLowerCase();
  }

  public relativeIfInside(file: string): string | undefined {
    const relative = path.relative(this.workspace, path.resolve(file));
    if (
      relative === ".." ||
      relative.startsWith(`..${path.sep}`) ||
      path.isAbsolute(relative)
    ) {
      return undefined;
    }
    return posixPath(relative);
  }
}

class SourceInventoryError extends Error {
  constructor(
    readonly absolutePath: string,
    message: string,
  ) {
    super(message);
    this.name = "SourceInventoryError";
  }
}

function validateTypeScriptUnit(unit: AnalyzerRequest["units"][number]): void {
  if (unit.language !== "typescript") {
    throw new Error(
      `Unit ${unit.unit_id} has unsupported language ${String(unit.language)}`,
    );
  }
}

function loadSource(
  workspace: string,
  unitId: string,
  requestedPath: string,
  seen: Map<string, string>,
  parseCounts: Map<string, number>,
): SourceEntry | undefined {
  const absolutePath = path.resolve(
    path.isAbsolute(requestedPath)
      ? requestedPath
      : path.resolve(workspace, requestedPath),
  );
  const relativeNative = validateSourcePath(
    workspace,
    absolutePath,
    requestedPath,
  );
  const previousOwner = seen.get(absolutePath);
  if (previousOwner !== undefined && previousOwner !== unitId) {
    if (isDeclarationFile(absolutePath)) return undefined;
    throw new Error(
      `Source ${posixPath(relativeNative)} is owned by both ${previousOwner} and ${unitId}`,
    );
  }
  if (previousOwner !== undefined) return undefined;
  seen.set(absolutePath, unitId);
  return parseSource(unitId, absolutePath, relativeNative, parseCounts);
}

function validateSourcePath(
  workspace: string,
  absolutePath: string,
  requestedPath: string,
): string {
  const relativeNative = path.relative(workspace, absolutePath);
  if (
    relativeNative === ".." ||
    relativeNative.startsWith(`..${path.sep}`) ||
    path.isAbsolute(relativeNative)
  ) {
    throw new SourceInventoryError(
      absolutePath,
      `Requested source is outside workspace: ${requestedPath}`,
    );
  }
  const extension = path.extname(absolutePath).toLowerCase();
  if (!SUPPORTED_EXTENSIONS.has(extension)) {
    throw new SourceInventoryError(
      absolutePath,
      `Unsupported TypeScript source: ${requestedPath}`,
    );
  }
  return relativeNative;
}

function parseSource(
  unitId: string,
  absolutePath: string,
  relativeNative: string,
  parseCounts: Map<string, number>,
): SourceEntry {
  let text: string;
  try {
    text = fs.readFileSync(absolutePath, "utf8");
  } catch (error) {
    throw new SourceInventoryError(
      absolutePath,
      `Unable to read TypeScript source ${relativeNative}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
  const sourceFile = ts.createSourceFile(
    absolutePath,
    text,
    ts.ScriptTarget.Latest,
    true,
    scriptKind(absolutePath),
  );
  parseCounts.set(absolutePath, (parseCounts.get(absolutePath) ?? 0) + 1);
  const syntaxErrors =
    (
      sourceFile as ts.SourceFile & {
        parseDiagnostics?: readonly ts.Diagnostic[];
      }
    ).parseDiagnostics ?? [];
  return {
    unitId,
    absolutePath,
    relativePath: posixPath(relativeNative),
    sourceFile,
    syntaxErrors,
    isDeclaration: isDeclarationFile(absolutePath),
  };
}
