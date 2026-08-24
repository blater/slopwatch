import * as fs from "node:fs";
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

  const diagnostics: Diagnostic[] = [];
  let compilerOptions: ts.CompilerOptions;
  let projectReferences: readonly ts.ProjectReference[] | undefined;
  let tsconfigPath: string | undefined;

  try {
    const configured = request.options?.tsconfig;
    if (configured !== undefined) {
      tsconfigPath = owner.canonicalConfigPath(configured);
    } else {
      const discovered = new Set<string>();
      const configByDirectory = new Map<string, string | undefined>();
      const nearestConfig = (start: string): string | undefined => {
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
      };
      for (const source of owner.sources) {
        const config = nearestConfig(path.dirname(source.absolutePath));
        if (config !== undefined) {
          const canonical = fs.realpathSync(config);
          const relative = path.relative(owner.workspace, canonical);
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
              attributes: {
                tsconfigs: [...discovered]
                  .sort()
                  .map((item) =>
                    posixPath(path.relative(owner.workspace, item)),
                  ),
              },
            },
          ],
          unavailableReason: "multiple authoritative TypeScript projects",
        };
      }
      tsconfigPath = [...discovered][0];
    }

    if (tsconfigPath !== undefined) {
      const loaded = ts.readConfigFile(tsconfigPath, ts.sys.readFile);
      if (loaded.error !== undefined) {
        return owner.configFailure([loaded.error], tsconfigPath);
      }
      const parsed = ts.parseJsonConfigFileContent(
        loaded.config,
        ts.sys,
        path.dirname(tsconfigPath),
        { noEmit: true },
        tsconfigPath,
      );
      if (parsed.errors.length > 0)
        return owner.configFailure(parsed.errors, tsconfigPath);
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
        allowJs: false,
      };
    }
  } catch (error) {
    return {
      diagnostics: [
        {
          code: "typescript.config_unusable",
          severity: "error",
          message: error instanceof Error ? error.message : String(error),
        },
      ],
      unavailableReason: "unusable TypeScript project configuration",
    };
  }

  if (compilerOptions.strict !== true) {
    diagnostics.push({
      code: "typescript.strict_disabled",
      severity: "warning",
      message:
        "TypeScript strict mode is disabled; typed findings remain available but may be less precise.",
    });
  }

  const defaultHost = ts.createCompilerHost(compilerOptions, true);
  const sourceCache = new Map<string, ts.SourceFile>();
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
    if (!shouldCreateNewSourceFile) {
      const cached = sourceCache.get(key);
      if (cached !== undefined) return cached;
    }
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

  let program: ts.Program;
  try {
    program = ts.createProgram({
      rootNames: owner.sources.map((item) => item.absolutePath),
      options: compilerOptions,
      host: defaultHost,
      ...(projectReferences === undefined ? {} : { projectReferences }),
    });
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

  const compilerDiagnostics = [
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

  if (compilerDiagnostics.length > 0) {
    for (const item of compilerDiagnostics.slice(0, 50)) {
      const sourcePath = item.file?.fileName;
      const diagnosticPath =
        sourcePath === undefined
          ? undefined
          : owner.relativeIfInside(sourcePath);
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
      compilerOptions,
      ...(tsconfigPath === undefined ? {} : { tsconfigPath }),
    },
  };
}
