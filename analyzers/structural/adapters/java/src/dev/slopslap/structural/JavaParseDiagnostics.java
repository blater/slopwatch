package dev.slopslap.structural;

import javax.tools.*;
import java.nio.file.Path;
import java.util.*;

/** Converts javac syntax diagnostics into the stable protocol failure shape. */
final class JavaParseDiagnostics {
    private JavaParseDiagnostics() { }
    static Map<String, List<Diagnostic<? extends JavaFileObject>>> syntaxErrors(
            Path workspace, Set<String> requestedPaths, DiagnosticCollector<JavaFileObject> diagnostics) {
        Map<String, List<Diagnostic<? extends JavaFileObject>>> failures = new LinkedHashMap<>();
        for (Diagnostic<? extends JavaFileObject> diagnostic : diagnostics.getDiagnostics()) {
            if (diagnostic.getKind() != Diagnostic.Kind.ERROR) continue;
            JavaFileObject source = diagnostic.getSource();
            if (source == null) throw new IllegalArgumentException("Java parser reported an error without a source file: " + diagnostic.getMessage(Locale.ROOT));
            String relative = JavaParsePaths.relativePath(workspace, source);
            if (!requestedPaths.contains(relative)) throw new IllegalArgumentException("JDK parser returned an unrequested Java diagnostic source: " + relative);
            failures.computeIfAbsent(relative, ignored -> new ArrayList<>()).add(diagnostic);
        }
        return failures;
    }
    static void addFailures(Facts.Program program, Map<String, List<Diagnostic<? extends JavaFileObject>>> syntaxErrors) {
        for (Map.Entry<String, List<Diagnostic<? extends JavaFileObject>>> entry : syntaxErrors.entrySet()) {
            for (Diagnostic<? extends JavaFileObject> diagnostic : entry.getValue()) {
                Facts.FileFailure failure = new Facts.FileFailure(); failure.path = entry.getKey(); failure.code = "SYNTAX_ERROR";
                failure.diagnostic = entry.getKey() + ":" + diagnostic.getLineNumber() + ":" + diagnostic.getColumnNumber()
                        + ": " + diagnostic.getMessage(Locale.ROOT); program.failures.add(failure);
            }
        }
    }
}
