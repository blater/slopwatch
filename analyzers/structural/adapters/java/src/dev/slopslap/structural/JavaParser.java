package dev.slopslap.structural;

import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.util.JavacTask;
import com.sun.source.util.Trees;

import javax.tools.Diagnostic;
import javax.tools.DiagnosticCollector;
import javax.tools.JavaCompiler;
import javax.tools.JavaFileObject;
import javax.tools.StandardJavaFileManager;
import javax.tools.ToolProvider;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Optional;
import java.util.Set;

final class JavaParser {
    static Facts.Program analyze(Protocol.Request request) throws Exception {
        Path workspace = Path.of(request.workspace()).toRealPath();
        List<Path> sources = new ArrayList<>();
        Set<String> requestedPaths = new HashSet<>(request.paths().size());
        for (String requested : request.paths()) {
            sources.add(sourcePath(workspace, requested));
            requestedPaths.add(requested.replace('\\', '/'));
        }
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        if (compiler == null) {
            throw new IllegalStateException("Java structural analysis requires a JDK, not a JRE");
        }
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager files = compiler.getStandardFileManager(diagnostics, null, null)) {
            JavacTask task = (JavacTask) compiler.getTask(
                    null, files, diagnostics, List.of("-proc:none", "-implicit:none", "-Xlint:none"),
                    null, files.getJavaFileObjectsFromPaths(sources)
            );
            List<CompilationUnitTree> units = new ArrayList<>();
            task.parse().forEach(units::add);
            rejectSyntaxErrors(diagnostics);
            JavaAnalyzer analyzer = new JavaAnalyzer(Trees.instance(task));
            for (CompilationUnitTree unit : units) {
                String relative = relativePath(workspace, unit.getSourceFile());
                if (!requestedPaths.contains(relative)) {
                    throw new IllegalArgumentException("JDK parser returned an unrequested Java source");
                }
                if (!request.includeTests() && testPackage(unit.getPackageName())) {
                    analyzer.skipUnit(relative);
                } else {
                    analyzer.scanUnit(unit, relative);
                }
            }
            analyzer.order();
            return analyzer.program();
        }
    }

    private static void rejectSyntaxErrors(DiagnosticCollector<JavaFileObject> diagnostics) {
        Optional<Diagnostic<? extends JavaFileObject>> error = diagnostics.getDiagnostics()
                .stream().filter(item -> item.getKind() == Diagnostic.Kind.ERROR).findFirst();
        if (error.isPresent()) {
            throw new IllegalArgumentException(
                    "failed to parse Java source: " + error.get().getMessage(Locale.ROOT)
            );
        }
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
        return current;
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
