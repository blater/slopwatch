import * as fs from "node:fs";
import * as path from "node:path";
import ts from "typescript";

import type { AnalyzerRequest, Diagnostic, SourceEntry, TypeMode } from "./model.js";
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

function posixPath(value: string): string {
  return value.split(path.sep).join("/");
}

function isDeclarationFile(file: string): boolean {
  return /\.d\.(?:ts|mts|cts)$/u.test(file);
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
  readonly syntaxParseCounts = new Map<string, number>();
  readonly typedParseCounts = new Map<string, number>();
  typedProgramCreated = false;

  private constructor(workspace: string, sources: SourceEntry[]) {
    this.workspace = workspace;
    this.sources = sources;
  }

  static create(request: AnalyzerRequest): AnalysisContext {
    const workspace = fs.realpathSync(path.resolve(request.workspace));
    const seen = new Map<string, string>();
    const sources: SourceEntry[] = [];
    const parseCounts = new Map<string, number>();

    for (const unit of request.units) {
      if (unit.language !== "typescript") {
        throw new Error(`Unit ${unit.unit_id} has unsupported language ${String(unit.language)}`);
      }
      for (const requestedPath of unit.source_paths) {
        const candidate = path.isAbsolute(requestedPath)
          ? requestedPath
          : path.resolve(workspace, requestedPath);
        const absolutePath = fs.realpathSync(candidate);
        const relativeNative = path.relative(workspace, absolutePath);
        if (
          relativeNative === ".." ||
          relativeNative.startsWith(`..${path.sep}`) ||
          path.isAbsolute(relativeNative)
        ) {
          throw new Error(`Requested source is outside workspace: ${requestedPath}`);
        }
        const extension = path.extname(absolutePath).toLowerCase();
        if (!SUPPORTED_EXTENSIONS.has(extension) || isDeclarationFile(absolutePath)) {
          throw new Error(`Unsupported TypeScript source: ${requestedPath}`);
        }
        const previousOwner = seen.get(absolutePath);
        if (previousOwner !== undefined && previousOwner !== unit.unit_id) {
          throw new Error(
            `Source ${posixPath(relativeNative)} is owned by both ${previousOwner} and ${unit.unit_id}`
          );
        }
        if (previousOwner !== undefined) continue;

        seen.set(absolutePath, unit.unit_id);
        const text = fs.readFileSync(absolutePath, "utf8");
        const sourceFile = ts.createSourceFile(
          absolutePath,
          text,
          ts.ScriptTarget.Latest,
          true,
          scriptKind(absolutePath)
        );
        parseCounts.set(absolutePath, (parseCounts.get(absolutePath) ?? 0) + 1);
        const syntaxErrors = (
          sourceFile as ts.SourceFile & { parseDiagnostics?: readonly ts.Diagnostic[] }
        ).parseDiagnostics ?? [];
        sources.push({
          unitId: unit.unit_id,
          absolutePath,
          relativePath: posixPath(relativeNative),
          sourceFile,
          syntaxErrors
        });
      }
    }

    sources.sort((a, b) => a.relativePath.localeCompare(b.relativePath));
    const context = new AnalysisContext(workspace, sources);
    for (const [file, count] of parseCounts) context.syntaxParseCounts.set(file, count);
    return context;
  }

  createTypedContext(request: AnalyzerRequest, mode: TypeMode): TypedContextResult {
    return createTypedContext(this, request, mode);
  }

  public canonicalConfigPath(configured: string): string {
    const candidate = path.isAbsolute(configured)
      ? configured
      : path.resolve(this.workspace, configured);
    const canonical = fs.realpathSync(candidate);
    const relative = path.relative(this.workspace, canonical);
    if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
      throw new Error(`tsconfig is outside workspace: ${configured}`);
    }
    return canonical;
  }

  public configFailure(items: readonly ts.Diagnostic[], config: string): TypedContextResult {
    return {
      diagnostics: items.map((item) => ({
        code: `typescript.config.${item.code}`,
        severity: "error" as const,
        message: `${posixPath(path.relative(this.workspace, config))}: ${formatTsDiagnostic(item)}`
      })),
      unavailableReason: "unusable TypeScript project configuration"
    };
  }

  public canonicalKey(file: string): string {
    const resolved = path.resolve(file);
    return ts.sys.useCaseSensitiveFileNames ? resolved : resolved.toLowerCase();
  }

  public relativeIfInside(file: string): string | undefined {
    const relative = path.relative(this.workspace, path.resolve(file));
    if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
      return undefined;
    }
    return posixPath(relative);
  }
}
