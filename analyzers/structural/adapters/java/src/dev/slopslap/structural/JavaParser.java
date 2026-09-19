package dev.slopslap.structural;

import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.util.JavacTask;
import com.sun.source.util.Trees;

import javax.tools.Diagnostic;
import javax.tools.DiagnosticCollector;
import javax.tools.JavaCompiler;
import javax.tools.JavaFileObject;
import javax.tools.StandardLocation;
import javax.tools.StandardJavaFileManager;
import javax.tools.ToolProvider;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

final class JavaParser {
    static Facts.Program analyze(Protocol.Request request) throws Exception {
        Path workspace = Path.of(request.workspace()).toRealPath();
        List<Path> sources = new ArrayList<>();
        Set<String> requestedPaths = new HashSet<>(request.paths().size());
        List<Facts.FileFailure> sourceFailures = new ArrayList<>();
        for (String requested : request.paths()) {
            String relative = requested.replace('\\', '/');
            if (!requestedPaths.add(relative)) {
                throw new IllegalArgumentException("duplicate Java source path: " + relative);
            }
            try {
                sources.add(sourcePath(workspace, requested));
            } catch (IOException | IllegalArgumentException error) {
                Facts.FileFailure failure = new Facts.FileFailure();
                failure.path = relative;
                failure.code = sourceFailureCode(requested, error);
                failure.diagnostic = relative + ": " + error.getMessage();
                sourceFailures.add(failure);
            }
        }
        if (sources.isEmpty()) {
            Facts.Program program = new Facts.Program();
            program.failures.addAll(sourceFailures);
            return program;
        }
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        if (compiler == null) {
            throw new IllegalStateException("Java structural analysis requires a JDK, not a JRE");
        }
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager files = compiler.getStandardFileManager(diagnostics, null, null)) {
            JavacTask task = (JavacTask) compiler.getTask(
                    null, files, diagnostics, compilerOptions(request.depth()),
                    null, files.getJavaFileObjectsFromPaths(sources)
            );
            List<CompilationUnitTree> units = new ArrayList<>();
            task.parse().forEach(units::add);
            Map<String, List<Diagnostic<? extends JavaFileObject>>> syntaxErrors = syntaxErrors(workspace, requestedPaths, diagnostics);
            JavaAnalyzer analyzer = new JavaAnalyzer(Trees.instance(task));
            for (CompilationUnitTree unit : units) {
                String relative = relativePath(workspace, unit.getSourceFile());
                if (!requestedPaths.contains(relative)) {
                    throw new IllegalArgumentException("JDK parser returned an unrequested Java source");
                }
                if (syntaxErrors.containsKey(relative)) {
                    continue;
                }
                if (!request.includeTests() && testPackage(unit.getPackageName())) {
                    analyzer.skipUnit(relative);
                } else {
                    analyzer.scanUnit(unit, relative);
                }
            }
            analyzer.order();
            Facts.Program program = analyzer.program();
            program.failures.addAll(sourceFailures);
            for (Map.Entry<String, List<Diagnostic<? extends JavaFileObject>>> entry : syntaxErrors.entrySet()) {
                for (Diagnostic<? extends JavaFileObject> diagnostic : entry.getValue()) {
                    Facts.FileFailure failure = new Facts.FileFailure();
                    failure.path = entry.getKey();
                    failure.code = "SYNTAX_ERROR";
                    failure.diagnostic = entry.getKey() + ":" + diagnostic.getLineNumber() + ":"
                            + diagnostic.getColumnNumber() + ": " + diagnostic.getMessage(Locale.ROOT);
                    program.failures.add(failure);
                }
            }
            if (request.depth()) {
                // Keep the legacy tree extraction above independent from attribution.
                // Javac otherwise falls back to source files reachable from the
                // workspace/class path while resolving the supplied units.
                configureDepthFileManager(files);
                program.depth.addAll(JavaDepth.collect(task, units, workspace, program, diagnostics));
            }
            return program;
        }
    }

    private static List<String> compilerOptions(boolean depth) {
        List<String> options = new ArrayList<>(List.of("-proc:none", "-implicit:none", "-Xlint:none"));
        if (depth) {
            // An empty source path disables javac's implicit source discovery. An
            // explicitly empty class path leaves only the JDK platform classes
            // available for attribution.
            options.addAll(List.of("-sourcepath", "", "-classpath", ""));
        }
        return options;
    }

    private static void configureDepthFileManager(StandardJavaFileManager files) throws IOException {
        files.setLocationFromPaths(StandardLocation.SOURCE_PATH, List.of());
        files.setLocationFromPaths(StandardLocation.CLASS_PATH, List.of());
    }

    private static Map<String, List<Diagnostic<? extends JavaFileObject>>> syntaxErrors(
            Path workspace, Set<String> requestedPaths, DiagnosticCollector<JavaFileObject> diagnostics) {
        Map<String, List<Diagnostic<? extends JavaFileObject>>> failures = new LinkedHashMap<>();
        for (Diagnostic<? extends JavaFileObject> diagnostic : diagnostics.getDiagnostics()) {
            if (diagnostic.getKind() != Diagnostic.Kind.ERROR) {
                continue;
            }
            JavaFileObject source = diagnostic.getSource();
            if (source == null) {
                throw new IllegalArgumentException("Java parser reported an error without a source file: "
                        + diagnostic.getMessage(Locale.ROOT));
            }
            String relative = relativePath(workspace, source);
            if (!requestedPaths.contains(relative)) {
                throw new IllegalArgumentException("JDK parser returned an unrequested Java diagnostic source: " + relative);
            }
            failures.computeIfAbsent(relative, ignored -> new ArrayList<>()).add(diagnostic);
        }
        return failures;
    }

    private static Path sourcePath(Path workspace, String requested) throws IOException {
        if (requested.isEmpty() || requested.indexOf('\\') >= 0 || !requested.endsWith(".java")) {
            throw new IllegalArgumentException("non-canonical Java source path: " + requested);
        }
        Path current = workspace;
        String[] parts = requested.split("/", -1);
        for (int index = 0; index < parts.length; index++) {
            String part = parts[index];
            if (part.isEmpty() || part.equals(".") || part.equals("..")) {
                throw new IllegalArgumentException("non-canonical Java source path: " + requested);
            }
            current = current.resolve(part);
        }
        if (!Files.isRegularFile(current)) {
            throw new IllegalArgumentException("Java source is not a regular file: " + requested);
        }
        if (!Files.isReadable(current)) {
            throw new IOException("Java source is not readable: " + requested);
        }
        Path resolved = current.toRealPath();
        if (!resolved.startsWith(workspace)) {
            throw new IOException("Java source escapes workspace: " + requested);
        }
        return current;
    }

    private static String sourceFailureCode(String requested, Exception sourceError) {
        if (sourceError != null && sourceError.getMessage() != null
                && sourceError.getMessage().contains("escapes workspace")) {
            return "SOURCE_PATH_ERROR";
        }
        if (requested.isEmpty() || requested.indexOf('\\') >= 0 || requested.startsWith("/")
                || Arrays.stream(requested.split("/", -1))
                        .anyMatch(part -> part.isEmpty() || part.equals(".") || part.equals(".."))) {
            return "SOURCE_PATH_ERROR";
        }
        if (!requested.endsWith(".java")) {
            return "UNSUPPORTED_SOURCE";
        }
        return "SOURCE_READ_ERROR";
    }

    private static String relativePath(Path workspace, JavaFileObject source) {
        return workspace.relativize(Path.of(source.toUri())).toString().replace('\\', '/');
    }

    private static boolean testPackage(ExpressionTree packageName) {
        if (packageName == null) {
            return false;
        }
        return Arrays.stream(packageName.toString().split("\\."))
                .anyMatch(part -> part.equalsIgnoreCase("test") || part.equalsIgnoreCase("tests"));
    }

    private JavaParser() { }
}
