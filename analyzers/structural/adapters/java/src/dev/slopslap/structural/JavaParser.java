package dev.slopslap.structural;

import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.util.JavacTask;
import com.sun.source.util.Trees;
import javax.tools.*;
import java.io.IOException;
import java.nio.file.Path;
import java.util.*;

/** Compiler orchestration for Java syntax and optional attributed-depth analysis. */
final class JavaParser {
    static Facts.Program analyze(Protocol.Request request) throws Exception {
        Path workspace = Path.of(request.workspace()).toRealPath();
        List<Path> sources = new ArrayList<>();
        Set<String> requestedPaths = new HashSet<>(request.paths().size());
        List<Facts.FileFailure> sourceFailures = new ArrayList<>();
        for (String requested : request.paths()) {
            String relative = requested.replace('\\', '/');
            if (!requestedPaths.add(relative)) throw new IllegalArgumentException("duplicate Java source path: " + relative);
            try { sources.add(JavaParsePaths.sourcePath(workspace, requested)); }
            catch (IOException | IllegalArgumentException error) {
                Facts.FileFailure failure = new Facts.FileFailure();
                failure.path = relative; failure.code = JavaParsePaths.sourceFailureCode(requested, error);
                failure.diagnostic = relative + ": " + error.getMessage(); sourceFailures.add(failure);
            }
        }
        if (sources.isEmpty()) {
            Facts.Program program = new Facts.Program();
            program.failures.addAll(sourceFailures);
            return program;
        }
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        if (compiler == null) throw new IllegalStateException("Java structural analysis requires a JDK, not a JRE");
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager files = compiler.getStandardFileManager(diagnostics, null, null)) {
            JavacTask task = (JavacTask) compiler.getTask(null, files, diagnostics, options(request.depth()),
                    null, files.getJavaFileObjectsFromPaths(sources));
            List<CompilationUnitTree> units = new ArrayList<>();
            task.parse().forEach(units::add);
            Map<String, List<Diagnostic<? extends JavaFileObject>>> syntaxErrors =
                    JavaParseDiagnostics.syntaxErrors(workspace, requestedPaths, diagnostics);
            JavaAnalyzer analyzer = new JavaAnalyzer(Trees.instance(task));
            for (CompilationUnitTree unit : units) {
                String relative = JavaParsePaths.relativePath(workspace, unit.getSourceFile());
                if (!requestedPaths.contains(relative)) throw new IllegalArgumentException("JDK parser returned an unrequested Java source");
                if (syntaxErrors.containsKey(relative)) continue;
                if (!request.includeTests() && JavaParsePaths.testPackage(unit.getPackageName())) analyzer.skipUnit(relative);
                else analyzer.scanUnit(unit, relative);
            }
            analyzer.order();
            Facts.Program program = analyzer.program();
            program.failures.addAll(sourceFailures);
            JavaParseDiagnostics.addFailures(program, syntaxErrors);
            if (request.depth()) {
                configureDepthFileManager(files);
                program.depth.addAll(JavaDepth.collect(task, units, workspace, program, diagnostics));
            }
            return program;
        }
    }

    private static List<String> options(boolean depth) {
        List<String> options = new ArrayList<>(List.of("-proc:none", "-implicit:none", "-Xlint:none"));
        if (depth) options.addAll(List.of("-sourcepath", "", "-classpath", ""));
        return options;
    }

    private static void configureDepthFileManager(StandardJavaFileManager files) throws IOException {
        files.setLocationFromPaths(StandardLocation.SOURCE_PATH, List.of());
        files.setLocationFromPaths(StandardLocation.CLASS_PATH, List.of());
    }

    private JavaParser() { }
}
