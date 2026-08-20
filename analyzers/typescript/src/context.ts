import * as fs from "node:fs";
import * as path from "node:path";
import ts from "typescript";

import type { AnalyzerRequest, Diagnostic, SourceEntry, TypeMode } from "./model.js";

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
    if (mode === "off") {
      return { diagnostics: [], unavailableReason: "typescript_types is off" };
    }
    if (this.sources.length === 0) {
      return { diagnostics: [], unavailableReason: "analysis unit has no source files" };
    }

    const diagnostics: Diagnostic[] = [];
    let compilerOptions: ts.CompilerOptions;
    let projectReferences: readonly ts.ProjectReference[] | undefined;
    let tsconfigPath: string | undefined;

    try {
      const configured = request.options?.tsconfig;
      if (configured !== undefined) {
        tsconfigPath = this.canonicalConfigPath(configured);
      } else {
        const discovered = new Set<string>();
        for (const source of this.sources) {
          const config = ts.findConfigFile(path.dirname(source.absolutePath), ts.sys.fileExists);
          if (config !== undefined) {
            const canonical = fs.realpathSync(config);
            const relative = path.relative(this.workspace, canonical);
            if (
              relative !== ".." &&
              !relative.startsWith(`..${path.sep}`) &&
              !path.isAbsolute(relative)
            ) {
              discovered.add(canonical);
            }
          }
        }
        if (discovered.size > 1) {
          return {
            diagnostics: [
              {
                code: "typescript.multiple_projects",
                severity: "error",
                message:
                  "Exact files resolve to multiple tsconfig projects; split them into authoritative analysis units.",
                attributes: { tsconfigs: [...discovered].sort().map((item) => posixPath(path.relative(this.workspace, item))) }
              }
            ],
            unavailableReason: "multiple authoritative TypeScript projects"
          };
        }
        tsconfigPath = [...discovered][0];
      }

      if (tsconfigPath !== undefined) {
        const loaded = ts.readConfigFile(tsconfigPath, ts.sys.readFile);
        if (loaded.error !== undefined) {
          return this.configFailure([loaded.error], tsconfigPath);
        }
        const parsed = ts.parseJsonConfigFileContent(
          loaded.config,
          ts.sys,
          path.dirname(tsconfigPath),
          { noEmit: true },
          tsconfigPath
        );
        if (parsed.errors.length > 0) return this.configFailure(parsed.errors, tsconfigPath);
        compilerOptions = { ...parsed.options, noEmit: true, skipLibCheck: true };
        projectReferences = parsed.projectReferences;
      } else {
        compilerOptions = {
          target: ts.ScriptTarget.ES2022,
          module: ts.ModuleKind.NodeNext,
          moduleResolution: ts.ModuleResolutionKind.NodeNext,
          strict: true,
          noEmit: true,
          skipLibCheck: true,
          allowJs: false
        };
      }
    } catch (error) {
      return {
        diagnostics: [
          {
            code: "typescript.config_unusable",
            severity: "error",
            message: error instanceof Error ? error.message : String(error)
          }
        ],
        unavailableReason: "unusable TypeScript project configuration"
      };
    }

    if (compilerOptions.strict !== true) {
      diagnostics.push({
        code: "typescript.strict_disabled",
        severity: "warning",
        message: "TypeScript strict mode is disabled; typed findings remain available but may be less precise."
      });
    }

    const defaultHost = ts.createCompilerHost(compilerOptions, true);
    const sourceCache = new Map<string, ts.SourceFile>();
    const requested = new Map(
      this.sources.map((item) => [this.canonicalKey(item.absolutePath), item.absolutePath])
    );
    const originalGetSourceFile = defaultHost.getSourceFile.bind(defaultHost);
    defaultHost.getSourceFile = (fileName, languageVersion, onError, shouldCreateNewSourceFile) => {
      const key = this.canonicalKey(fileName);
      if (!shouldCreateNewSourceFile) {
        const cached = sourceCache.get(key);
        if (cached !== undefined) return cached;
      }
      const source = originalGetSourceFile(fileName, languageVersion, onError, shouldCreateNewSourceFile);
      if (source !== undefined) {
        sourceCache.set(key, source);
        const requestedPath = requested.get(key);
        if (requestedPath !== undefined) {
          this.typedParseCounts.set(
            requestedPath,
            (this.typedParseCounts.get(requestedPath) ?? 0) + 1
          );
        }
      }
      return source;
    };

    let program: ts.Program;
    try {
      program = ts.createProgram({
        rootNames: this.sources.map((item) => item.absolutePath),
        options: compilerOptions,
        host: defaultHost,
        ...(projectReferences === undefined ? {} : { projectReferences })
      });
      this.typedProgramCreated = true;
    } catch (error) {
      return {
        diagnostics: [
          ...diagnostics,
          {
            code: "typescript.program_failed",
            severity: "error",
            message: error instanceof Error ? error.message : String(error)
          }
        ],
        unavailableReason: "TypeScript compiler program construction failed"
      };
    }

    const compilerDiagnostics = [
      ...program.getOptionsDiagnostics(),
      ...program.getGlobalDiagnostics(),
      ...this.sources.flatMap((entry) => {
        const source = program.getSourceFile(entry.absolutePath);
        return source === undefined
          ? []
          : [...program.getSyntacticDiagnostics(source), ...program.getSemanticDiagnostics(source)];
      })
    ].filter((item) => item.category === ts.DiagnosticCategory.Error);

    if (compilerDiagnostics.length > 0) {
      for (const item of compilerDiagnostics.slice(0, 50)) {
        const sourcePath = item.file?.fileName;
        const diagnosticPath = sourcePath === undefined ? undefined : this.relativeIfInside(sourcePath);
        const attributes =
          item.start === undefined ? undefined : { offset: item.start, length: item.length ?? 0 };
        diagnostics.push({
          code: `typescript.compiler.${item.code}`,
          severity: "error",
          message: formatTsDiagnostic(item),
          ...(diagnosticPath === undefined ? {} : { path: diagnosticPath }),
          ...(attributes === undefined ? {} : { attributes })
        });
      }
      return {
        diagnostics,
        unavailableReason: "the TypeScript compiler reported errors; the type graph is not trustworthy"
      };
    }

    const typedSources = new Map<string, ts.SourceFile>();
    for (const item of this.sources) {
      const typedSource = program.getSourceFile(item.absolutePath);
      if (typedSource === undefined) {
        return {
          diagnostics: [
            ...diagnostics,
            {
              unit_id: item.unitId,
              path: item.relativePath,
              code: "typescript.source_missing_from_program",
              severity: "error",
              message: "The requested source was not present in the compiler program."
            }
          ],
          unavailableReason: "requested source missing from typed program"
        };
      }
      typedSources.set(item.absolutePath, typedSource);
    }

    return {
      diagnostics,
      context: {
        program,
        checker: program.getTypeChecker(),
        sourceFiles: typedSources,
        compilerOptions,
        ...(tsconfigPath === undefined ? {} : { tsconfigPath })
      }
    };
  }

  private canonicalConfigPath(configured: string): string {
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

  private configFailure(items: readonly ts.Diagnostic[], config: string): TypedContextResult {
    return {
      diagnostics: items.map((item) => ({
        code: `typescript.config.${item.code}`,
        severity: "error" as const,
        message: `${posixPath(path.relative(this.workspace, config))}: ${formatTsDiagnostic(item)}`
      })),
      unavailableReason: "unusable TypeScript project configuration"
    };
  }

  private canonicalKey(file: string): string {
    const resolved = path.resolve(file);
    return ts.sys.useCaseSensitiveFileNames ? resolved : resolved.toLowerCase();
  }

  private relativeIfInside(file: string): string | undefined {
    const relative = path.relative(this.workspace, path.resolve(file));
    if (relative === ".." || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
      return undefined;
    }
    return posixPath(relative);
  }
}
