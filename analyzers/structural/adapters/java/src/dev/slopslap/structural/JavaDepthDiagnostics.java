package dev.slopslap.structural;

import javax.tools.Diagnostic;
import javax.tools.DiagnosticCollector;
import javax.tools.JavaFileObject;
import com.sun.source.tree.CompilationUnitTree;
import java.nio.file.Path;
import java.util.*;

/** Store compiler detail once; each boundary gets only its relevant summary. */
final class JavaDepthDiagnostics {
    private final List<Object> reasons = new ArrayList<>();
    private final Map<String, String> firstByFile = new LinkedHashMap<>();
    private final Map<String, String> packageByFile = new HashMap<>();
    private final Set<String> affectedPackages = new HashSet<>();
    private boolean unlocatable;
    JavaDepthDiagnostics(Path workspace, DiagnosticCollector<JavaFileObject> diagnostics,
                         List<CompilationUnitTree> units) {
        for (CompilationUnitTree unit : units) {
            String file = workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/');
            packageByFile.put(file, unit.getPackageName() == null ? "" : unit.getPackageName().toString());
        }
        for (Diagnostic<? extends JavaFileObject> diagnostic : diagnostics.getDiagnostics()) {
            if (diagnostic.getKind() != Diagnostic.Kind.ERROR) continue;
            JavaFileObject diagnosticSource = diagnostic.getSource();
            if (diagnosticSource == null) unlocatable = true;
            String source = diagnosticSource == null ? "<compiler>"
                    : workspace.relativize(Path.of(diagnosticSource.toUri())).toString().replace('\\', '/');
            String message = source + ":" + diagnostic.getLineNumber() + ":" + diagnostic.getColumnNumber()
                    + ": " + diagnostic.getMessage(Locale.ROOT);
            firstByFile.putIfAbsent(source, message);
            String packageName = packageByFile.get(source);
            if (packageName == null) unlocatable = true;
            else affectedPackages.add(packageName);
            reasons.add(Map.of("code", "java_attribution_failed", "dimension", "inventory",
                    "message", message, "fact_ids", List.of(source)));
        }
    }
    boolean incomplete() { return !reasons.isEmpty(); }
    boolean unlocatable() { return unlocatable; }
    boolean affectsFile(String file) { return firstByFile.containsKey(file); }
    boolean affectsPackage(String packageName) {
        return affectedPackages.contains(packageName);
    }
    List<Object> reasons() { return reasons; }
    String summary(String file) {
        String local = firstByFile.get(file);
        if (local != null) return local;
        return "The supplied Java source unit has unresolved attribution; "
                + firstByFile.values().iterator().next();
    }
}
