package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.JavacTask;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.util.Elements;
import javax.lang.model.util.Types;
import javax.tools.Diagnostic;
import javax.tools.DiagnosticCollector;
import javax.tools.JavaFileObject;
import java.nio.file.Path;
import java.util.*;

final class JavaDepth {
    private static final int MAX_BOUNDARY_CHARS = 1 << 20;

    static List<String> collect(JavacTask task, List<CompilationUnitTree> units, Path workspace,
                                Facts.Program program, DiagnosticCollector<JavaFileObject> diagnostics) throws Exception {
        return collect(task, units, workspace, program, diagnostics, null);
    }

    static List<String> collect(JavacTask task, List<CompilationUnitTree> units, Path workspace,
            Facts.Program program, DiagnosticCollector<JavaFileObject> diagnostics, DepthStream.Writer stream) throws Exception {
        if (stream != null) DepthStream.observe(task, stream);
        task.analyze();
        Trees trees = Trees.instance(task);
        Elements elements = task.getElements();
        Types types = task.getTypes();
        List<Object> boundaries = new ArrayList<>();
        List<Map<String, Object>> boundaryFlows = new ArrayList<>();
        Set<String> included = new HashSet<>(program.files);
        Set<String> failedFiles = new HashSet<>();
        for (Facts.FileFailure failure : program.failures) failedFiles.add(failure.path);
        Set<String> parsedFiles = new HashSet<>();
        for (CompilationUnitTree unit : units) {
            parsedFiles.add(workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/'));
        }
        JavaDepthDiagnostics errors = new JavaDepthDiagnostics(workspace, diagnostics, units);
        Map<ExecutableElement, TreePath> sourceMethodPaths = JavaDepthCalls.sourceMethodPaths(trees, units, workspace, included);
        boolean globallyUnknown = errors.unlocatable()
                || program.failures.stream().anyMatch(failure -> !parsedFiles.contains(failure.path));
        Map<String, Map<String, Object>> roles = JavaDepthRoles.collect(
                trees, elements, types, units, workspace, included, failedFiles, globallyUnknown, errors);
        Map<String, Integer> remaining = new HashMap<>();
        Map<String, List<String>> packageFiles = new HashMap<>();
        for (CompilationUnitTree unit : units) {
            String file = workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/');
            if (included.contains(file)) remaining.merge(String.valueOf(unit.getPackageName()), 1, Integer::sum);
        }
        int completed = 0;
        for (CompilationUnitTree unit : units) {
            int first = boundaries.size();
            String file = workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/');
            if (!included.contains(file)) continue;
            for (Tree declaration : unit.getTypeDecls()) {
                if (!(declaration instanceof ClassTree)) continue;
                TreePath path = TreePath.getPath(unit, declaration);
                if (!(trees.getElement(path) instanceof TypeElement type)) continue;
                JavaDepthBoundary boundary = new JavaDepthBoundary(trees, path, unit, file, type,
                        roles.get(type.getQualifiedName().toString()), sourceMethodPaths);
                boolean sourceIncomplete = globallyUnknown || errors.unlocatable()
                        || failedFiles.contains(file) || errors.affectsFile(file);
                if (sourceIncomplete) boundary.gap("incomplete_source_inventory");
                if (errors.affectsFile(file)) boundary.gap("attribution_failed", errors.summary(file));
                boundary.collect();
                boundaries.add(boundary.assessment());
                // A boundary whose source inventory is already incomplete cannot
                // safely resolve same-owner calls. Keeping its flow only spends
                // the bounded payload budget and can hide healthy boundaries in
                // the synthetic global-limit fallback.
                boundaryFlows.add(sourceIncomplete ? null : boundary.flow());
            }
            if (stream != null) {
                for (String chunk : encodeChunks(boundaries.subList(first, boundaries.size()), boundaryFlows.subList(first, boundaryFlows.size()), List.of(), program.files)) {
                    stream.write(Map.of("type", "depth", "group", String.valueOf(unit.getPackageName()), "payload", chunk));
                }
                String group = String.valueOf(unit.getPackageName());
                packageFiles.computeIfAbsent(group, key -> new ArrayList<>()).add(file);
                if (remaining.merge(group, -1, Integer::sum) == 0) stream.write(Map.of("type", "group_done", "group", group, "paths", packageFiles.remove(group)));
                stream.write(Map.of("type", "progress", "stage", "semantic", "completed", ++completed, "total", program.files.size()));
            }
        }
        if (stream != null) {
            if (!errors.reasons().isEmpty()) stream.write(Map.of("type", "depth", "payload", DepthJson.encode(Map.of("boundaries", List.of(), "flows", List.of(), "reasons", errors.reasons()))));
            return List.of();
        }
        return encodeChunks(boundaries, boundaryFlows, errors.reasons(), program.files);
    }

    private static List<String> encodeChunks(List<Object> boundaries, List<Map<String, Object>> boundaryFlows,
                                              List<Object> reasons, List<String> files) {
        List<String> chunks = new ArrayList<>();
        for (int index = 0; index < boundaries.size(); index++) {
            Object boundary = boundaries.get(index);
            Map<String, Object> flow = boundaryFlows.get(index);
            Map<String, Object> payload = Map.of("boundaries", List.of(boundary),
                    "flows", flow == null ? List.of() : List.of(flow),
                    "reasons", List.of());
            try {
                String encoded = DepthJson.encode(payload);
                if (encoded.length() <= MAX_BOUNDARY_CHARS) chunks.add(encoded);
                else chunks.add(JavaDepthLimited.compact(boundary, files));
            } catch (DepthJson.LimitExceeded limit) {
                chunks.add(JavaDepthLimited.compact(boundary, files));
            }
        }
        if (!reasons.isEmpty()) {
            chunks.add(DepthJson.encode(Map.of("boundaries", List.of(), "flows", List.of(), "reasons", reasons)));
        }
        return chunks;
    }

}
