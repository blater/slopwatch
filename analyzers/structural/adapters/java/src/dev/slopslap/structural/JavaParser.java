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
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Locale;
import java.util.Optional;

final class JavaParser {
    static Facts.Program analyze(Protocol.Request request) throws Exception {
        Path workspace = Path.of(request.workspace()).toRealPath();
        List<Path> sources = new ArrayList<>();
        List<String> requestedPaths = new ArrayList<>();
        for (String requested : request.paths()) {
            sources.add(canonicalSource(workspace, requested));
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

    private static Path canonicalSource(Path workspace, String requested) throws IOException {
        if (requested.isEmpty() || requested.indexOf('\\') >= 0 || !requested.endsWith(".java")) {
            throw new IllegalArgumentException("non-canonical Java source path: " + requested);
        }
        Path current = workspace;
        for (String part : requested.split("/", -1)) {
            if (part.isEmpty() || part.equals(".") || part.equals("..")) {
                throw new IllegalArgumentException("non-canonical Java source path: " + requested);
            }
            current = current.resolve(part);
            if (Files.isSymbolicLink(current)) {
                throw new IllegalArgumentException("symlinked Java source is not allowed: " + requested);
            }
        }
        Path source = current.toRealPath(LinkOption.NOFOLLOW_LINKS);
        if (!source.startsWith(workspace) || !Files.isRegularFile(source, LinkOption.NOFOLLOW_LINKS)) {
            throw new IllegalArgumentException("Java source escapes workspace: " + requested);
        }
        return source;
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
