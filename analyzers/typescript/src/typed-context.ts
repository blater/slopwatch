import * as path from "node:path";
import ts from "typescript";

import type { AnalyzerRequest, Diagnostic, TypeMode } from "./model.js";
import type { AnalysisContext, TypedContextResult } from "./context.js";

function posixPath(value: string): string {
  return value.split(path.sep).join("/");
}

function formatTsDiagnostic(item: ts.Diagnostic): string {
  return ts.flattenDiagnosticMessageText(item.messageText, "\n");
}

interface TypedConfiguration {
  compilerOptions: ts.CompilerOptions;
  projectReferences: readonly ts.ProjectReference[] | undefined;
  tsconfigPath: string | undefined;
}

type ConfigurationResolution =
  | { configuration: TypedConfiguration }
  | { failure: TypedContextResult };

function defaultCompilerOptions(): ts.CompilerOptions {
  return {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.NodeNext,
    moduleResolution: ts.ModuleResolutionKind.NodeNext,
    strict: true,
    noEmit: true,
    skipLibCheck: true,
    allowJs: false,
  };
}

function nearestTypedConfig(
  owner: AnalysisContext,
  start: string,
  configByDirectory: Map<string, string | undefined>,
): string | undefined {
  const visited: string[] = [];
  let directory = path.resolve(start);
  let result: string | undefined;
  for (;;) {
    if (configByDirectory.has(directory)) {
      result = configByDirectory.get(directory);
      break;
    }
    visited.push(directory);
    const candidate = path.join(directory, "tsconfig.json");
    if (ts.sys.fileExists(candidate)) {
      result = candidate;
      break;
    }
    if (directory === owner.workspace) break;
    const parent = path.dirname(directory);
    if (parent === directory) break;
    directory = parent;
  }
  for (const item of visited) configByDirectory.set(item, result);
  return result;
}

function isInsideWorkspace(owner: AnalysisContext, candidate: string): boolean {
  const relative = path.relative(owner.workspace, candidate);
  return (
    relative !== ".." &&
    !relative.startsWith(`..${path.sep}`) &&
    !path.isAbsolute(relative)
  );
}

function discoveredTypedConfigs(owner: AnalysisContext): Set<string> {
  const discovered = new Set<string>();
  const configByDirectory = new Map<string, string | undefined>();
  for (const source of owner.sources) {
    const config = nearestTypedConfig(
      owner,
      path.dirname(source.absolutePath),
      configByDirectory,
    );
    if (config === undefined) continue;
    const canonical = path.resolve(config);
    if (isInsideWorkspace(owner, canonical)) discovered.add(canonical);
  }
  return discovered;
}

function multipleProjectsFailure(
  owner: AnalysisContext,
  discovered: Set<string>,
): TypedContextResult {
  return {
    diagnostics: [
      {
        code: "typescript.multiple_projects",
        severity: "error",
        message:
          "Exact files resolve to multiple tsconfig projects; split them into authoritative analysis units.",
        attributes: {
          tsconfigs: [...discovered]
            .sort()
            .map((item) => posixPath(path.relative(owner.workspace, item))),
        },
      },
    ],
    unavailableReason: "multiple authoritative TypeScript projects",
  };
}

function resolveTypedConfiguration(
  owner: AnalysisContext,
  request: AnalyzerRequest,
): ConfigurationResolution {
  try {
    const configured = request.options?.tsconfig;
    let tsconfigPath: string | undefined;
    const discovered =
      configured === undefined ? discoveredTypedConfigs(owner) : undefined;
    if (configured === undefined) {
      tsconfigPath = [...(discovered ?? [])][0];
    } else {
      tsconfigPath = owner.canonicalConfigPath(configured);
    }
    if (discovered !== undefined && discovered.size > 1) {
      return { failure: multipleProjectsFailure(owner, discovered) };
    }
    if (tsconfigPath === undefined) {
      return {
        configuration: {
          compilerOptions: defaultCompilerOptions(),
          projectReferences: undefined,
          tsconfigPath: undefined,
        },
      };
    }
    const loaded = ts.readConfigFile(tsconfigPath, ts.sys.readFile);
    if (loaded.error !== undefined) {
      return { failure: owner.configFailure([loaded.error], tsconfigPath) };
    }
    const parsed = ts.parseJsonConfigFileContent(
      loaded.config,
      ts.sys,
      path.dirname(tsconfigPath),
      { noEmit: true },
      tsconfigPath,
    );
    if (parsed.errors.length > 0) {
      return { failure: owner.configFailure(parsed.errors, tsconfigPath) };
    }
    return {
      configuration: {
        compilerOptions: { ...parsed.options, noEmit: true, skipLibCheck: true },
        projectReferences: parsed.projectReferences,
        tsconfigPath,
      },
    };
  } catch (error) {
    return {
      failure: {
        diagnostics: [
          {
            code: "typescript.config_unusable",
            severity: "error",
            message: error instanceof Error ? error.message : String(error),
          },
        ],
        unavailableReason: "unusable TypeScript project configuration",
      },
    };
  }
}

function typedCompilerHost(
  owner: AnalysisContext,
  compilerOptions: ts.CompilerOptions,
): ts.CompilerHost {
  const defaultHost = ts.createCompilerHost(compilerOptions, true);
  const sourceCache = new Map(
    owner.sources.map((item) => [
      owner.canonicalKey(item.absolutePath),
      item.sourceFile,
    ]),
  );
  const requested = new Map(
    owner.sources.map((item) => [
      owner.canonicalKey(item.absolutePath),
      item.absolutePath,
    ]),
  );
  const originalGetSourceFile = defaultHost.getSourceFile.bind(defaultHost);
  defaultHost.getSourceFile = (
    fileName,
    languageVersion,
    onError,
    shouldCreateNewSourceFile,
  ) => {
    const key = owner.canonicalKey(fileName);
    const cached = sourceCache.get(key);
    if (cached !== undefined) return cached;
    const source = originalGetSourceFile(
      fileName,
      languageVersion,
      onError,
      shouldCreateNewSourceFile,
    );
    if (source !== undefined) {
      sourceCache.set(key, source);
      const requestedPath = requested.get(key);
      if (requestedPath !== undefined) {
        owner.typedParseCounts.set(
          requestedPath,
          (owner.typedParseCounts.get(requestedPath) ?? 0) + 1,
        );
      }
    }
    return source;
  };
  return defaultHost;
}

function createTypedProgram(
  owner: AnalysisContext,
  configuration: TypedConfiguration,
): ts.Program {
  return ts.createProgram({
    rootNames: owner.sources.map((item) => item.absolutePath),
    options: configuration.compilerOptions,
    host: typedCompilerHost(owner, configuration.compilerOptions),
    ...(configuration.projectReferences === undefined
      ? {}
      : { projectReferences: configuration.projectReferences }),
  });
}

function compilerDiagnostics(
  owner: AnalysisContext,
  program: ts.Program,
): ts.Diagnostic[] {
  return [
    ...program.getOptionsDiagnostics(),
    ...program.getGlobalDiagnostics(),
    ...owner.sources.flatMap((entry) => {
      const source = program.getSourceFile(entry.absolutePath);
      return source === undefined
        ? []
        : [
            ...program.getSyntacticDiagnostics(source),
            ...program.getSemanticDiagnostics(source),
          ];
    }),
  ].filter((item) => item.category === ts.DiagnosticCategory.Error);
}

function compilerFailure(
  owner: AnalysisContext,
  diagnostics: Diagnostic[],
  items: ts.Diagnostic[],
): TypedContextResult {
  for (const item of items.slice(0, 50)) {
    const sourcePath = item.file?.fileName;
    const diagnosticPath =
      sourcePath === undefined ? undefined : owner.relativeIfInside(sourcePath);
    const attributes =
      item.start === undefined
        ? undefined
        : { offset: item.start, length: item.length ?? 0 };
    diagnostics.push({
      code: `typescript.compiler.${item.code}`,
      severity: "error",
      message: formatTsDiagnostic(item),
      ...(diagnosticPath === undefined ? {} : { path: diagnosticPath }),
      ...(attributes === undefined ? {} : { attributes }),
    });
  }
  return {
    diagnostics,
    unavailableReason:
      "the TypeScript compiler reported errors; the type graph is not trustworthy",
  };
}

export function createTypedContext(
  owner: AnalysisContext,
  request: AnalyzerRequest,
  mode: TypeMode,
): TypedContextResult {
  if (mode === "off") {
    return { diagnostics: [], unavailableReason: "typescript_types is off" };
  }
  if (owner.sources.length === 0) {
    return {
      diagnostics: [],
      unavailableReason: "analysis unit has no source files",
    };
  }

  const resolved = resolveTypedConfiguration(owner, request);
  if ("failure" in resolved) return resolved.failure;
  const { configuration } = resolved;
  const diagnostics: Diagnostic[] = [];
  if (configuration.compilerOptions.strict !== true) {
    diagnostics.push({
      code: "typescript.strict_disabled",
      severity: "warning",
      message:
        "TypeScript strict mode is disabled; typed findings remain available but may be less precise.",
    });
  }

  let program: ts.Program;
  try {
    program = createTypedProgram(owner, configuration);
    owner.typedProgramCreated = true;
  } catch (error) {
    return {
      diagnostics: [
        ...diagnostics,
        {
          code: "typescript.program_failed",
          severity: "error",
          message: error instanceof Error ? error.message : String(error),
        },
      ],
      unavailableReason: "TypeScript compiler program construction failed",
    };
  }

  const errors = compilerDiagnostics(owner, program);
  if (errors.length > 0) return compilerFailure(owner, diagnostics, errors);

  const typedSources = new Map<string, ts.SourceFile>();
  for (const item of owner.sources) {
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
            message:
              "The requested source was not present in the compiler program.",
          },
        ],
        unavailableReason: "requested source missing from typed program",
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
      compilerOptions: configuration.compilerOptions,
      ...(configuration.tsconfigPath === undefined
        ? {}
        : { tsconfigPath: configuration.tsconfigPath }),
    },
  };
}
