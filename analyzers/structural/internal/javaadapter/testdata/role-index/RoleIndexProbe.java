package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.*;
import javax.tools.*;
import javax.lang.model.element.*;
import java.net.URI;
import java.util.*;

/** Small operation-count and frozen-behaviour regressions, run by the Go suite. */
public final class RoleIndexProbe {
    private record Source(String name, String text) {
        JavaFileObject file() {
            return new SimpleJavaFileObject(URI.create("string:///p/" + name + ".java"), JavaFileObject.Kind.SOURCE) {
                @Override public CharSequence getCharContent(boolean ignored) { return text; }
            };
        }
    }

    public static void main(String[] args) throws Exception {
        for (int size : new int[]{16, 64, 256}) check(size, false);
        check(512, true);
        for (int size : new int[]{16, 64, 256}) checkSparseOverrides(size);
    }

    private static void checkSparseOverrides(int size) throws Exception {
        List<JavaFileObject> files = new ArrayList<>();
        StringBuilder contract = new StringBuilder("package p; interface Contract {");
        for (int i = 0; i < size; i++) {
            contract.append("default int value(T").append(i).append(" x){return 0;}");
            files.add(new Source("T" + i, "package p; final class T" + i + " {}").file());
            files.add(new Source("P" + i, "package p; final class P" + i
                    + " implements Contract { public int value(T" + i + " x){return 1;} }").file());
        }
        files.add(new Source("Contract", contract.append("}").toString()).file());
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager manager = compiler.getStandardFileManager(diagnostics, null, null)) {
            JavacTask task = (JavacTask) compiler.getTask(null, manager, diagnostics,
                    List.of("-proc:none", "-implicit:none"), null, files);
            List<CompilationUnitTree> units = new ArrayList<>();
            task.parse().forEach(units::add);
            task.analyze();
            if (diagnostics.getDiagnostics().stream().anyMatch(d -> d.getKind() == Diagnostic.Kind.ERROR))
                throw new AssertionError(diagnostics.getDiagnostics());
            Trees trees = Trees.instance(task);
            List<JavaDepthRoles.SourceType> sources = new ArrayList<>();
            for (CompilationUnitTree unit : units)
                JavaDepthRoleSourceIndex.collectTypes(trees, unit, unit.getTypeDecls(), null, unit.getSourceFile().getName(), sources);
            JavaDepthRoles roles = new JavaDepthRoles(trees, task.getElements(), task.getTypes(), sources);
            JavaDepthRoleCandidate candidate = new JavaDepthRoleCandidate(roles);
            int matched = 0;
            for (JavaDepthRoles.SourceType source : sources) {
                JavaDepthRoleCandidate.Result result = candidate.analyze(source);
                if (result == null) continue;
                if (result.matches().size() != 1) throw new AssertionError("sparse overload match lost: " + source.file());
                matched++;
            }
            if (matched != size || candidate.signatureWork() > 3L * size + 32)
                throw new AssertionError("shared overload inventory rebuilt: " + matched + "/" + candidate.signatureWork());
        }
    }

    private static void check(int size, boolean fanout) throws Exception {
        List<JavaFileObject> files = new ArrayList<>();
        files.add(new Source("Contract", "package p; interface Contract<T> { T value(); }").file());
        for (int i = 0; i < size; i++) {
            files.add(new Source("P" + i, "package p; final class P" + i
                    + " implements Contract<String> { public String value(){return null;} }").file());
            String body = fanout ? "return new P0();" : "return null;";
            files.add(new Source("Facade" + i, "package p; public class Facade" + i
                    + " { public static Object expose(){" + body + "} }").file());
        }
        files.add(new Source("Base", "package p; class Base { public Contract<String>[] values(){return null;} }").file());
        files.add(new Source("Visible", "package p; public class Visible extends Base {"
                + " public static Object value = new P0(); public static P1 direct; }").file());
        JavaCompiler compiler = ToolProvider.getSystemJavaCompiler();
        DiagnosticCollector<JavaFileObject> diagnostics = new DiagnosticCollector<>();
        try (StandardJavaFileManager manager = compiler.getStandardFileManager(diagnostics, null, null)) {
            JavacTask task = (JavacTask) compiler.getTask(null, manager, diagnostics,
                    List.of("-proc:none", "-implicit:none"), null, files);
            List<CompilationUnitTree> units = new ArrayList<>();
            task.parse().forEach(units::add);
            task.analyze();
            if (diagnostics.getDiagnostics().stream().anyMatch(d -> d.getKind() == Diagnostic.Kind.ERROR))
                throw new AssertionError(diagnostics.getDiagnostics());
            Trees trees = Trees.instance(task);
            List<JavaDepthRoles.SourceType> sources = new ArrayList<>();
            for (CompilationUnitTree unit : units)
                JavaDepthRoleSourceIndex.collectTypes(trees, unit, unit.getTypeDecls(), null, unit.getSourceFile().getName(), sources);
            JavaDepthRoles roles = new JavaDepthRoles(trees, task.getElements(), task.getTypes(), sources);
            JavaDepthRolePublicSurface surface = new JavaDepthRolePublicSurface(roles);
            FrozenRolePublicSurface frozen = new FrozenRolePublicSurface(roles);
            JavaDepthRoleReachability graph = new JavaDepthRoleReachability(roles);
            FrozenRoleReachability referenceGraph = new FrozenRoleReachability(roles);
            TypeElement contract = task.getElements().getTypeElement("p.Contract");
            for (int i = 0; i < size; i++) {
                TypeElement implementation = task.getElements().getTypeElement("p.P" + i);
                roles.exposureCutoff = false;
                Set<ExecutableElement> reached = graph.candidateExposureMethods(implementation, List.of(contract));
                boolean cutoff = roles.exposureCutoff;
                roles.exposureCutoff = false;
                Set<ExecutableElement> expectedReached = referenceGraph.candidateExposureMethods(implementation, List.of(contract));
                if (cutoff != roles.exposureCutoff || !reached.equals(expectedReached))
                    throw new AssertionError("reachability changed: " + i);
                List<String> actual = surface.routes(implementation, List.of(contract), reached);
                List<String> expected = new ArrayList<>();
                for (JavaDepthRoles.SourceType source : sources)
                    frozen.inspect(source, implementation, expected, List.of(contract), expectedReached);
                Collections.sort(actual); Collections.sort(expected);
                if (!actual.equals(expected)) throw new AssertionError("public routes changed: " + i + " " + actual + " != " + expected);
                if (!actual.contains("p.Base#values()")) throw new AssertionError("inherited generic array exposure lost");
            }
            if (surface.indexWork > size * 100L + 1000 || surface.queryWork > size * 20L + 1000)
                throw new AssertionError("surface work grew beyond source plus returned routes: " + surface.indexWork + "/" + surface.queryWork);
            if (graph.queryWork > size * 10L + 257L * 257L)
                throw new AssertionError("unbounded repeated frontier work: " + graph.queryWork);
        }
    }
}
