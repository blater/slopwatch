package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import javax.lang.model.util.Elements;
import javax.lang.model.util.Types;
import java.nio.file.Path;
import java.util.*;

/**
 * Extracts the small, resolved proof used by supporting-contract-v1.
 *
 * This deliberately only follows constructor injection into a final field and
 * a subsequent contract call on that field. It is not a general Java dataflow
 * engine and does not infer roles from names.
 */
final class JavaDepthRoles {
    static final String RULE = "supporting-contract-v1";
    // Keep returned-call tracing bounded for every root.  This is deliberately
    // large enough for ordinary production forwarding, while still preventing
    // an accidental repository-wide method walk.
    static final int MAX_REFERENCE_METHODS = 256;

    record SourceType(TypeElement element, ClassTree tree, TreePath path,
                              CompilationUnitTree unit, String file) { }

    final Trees trees;
    final Elements elements;
    final Types types;
    final List<SourceType> sources;
    final Map<TypeElement, SourceType> sourcesByType = new IdentityHashMap<>();
    final Map<Element, VariableTree> fieldTrees = new IdentityHashMap<>();
    final Map<Element, TreePath> fieldPaths = new IdentityHashMap<>();
    private final SourcePositions positions;
    private final JavaDepthRoleBindings bindings;
    private final JavaDepthRoleExposure exposure;
    private final JavaDepthRoleCandidate candidate;
    private final JavaDepthRoleProvenance provenanceHelper;
    boolean exposureCutoff;

    JavaDepthRoles(Trees trees, Elements elements, Types types, List<SourceType> sources) {
        this.trees = trees;
        this.elements = elements;
        this.types = types;
        this.sources = sources;
        this.positions = trees.getSourcePositions();
        for (SourceType source : sources) {
            sourcesByType.put(source.element(), source);
            JavaDepthRoleSourceIndex.indexFields(this, source);
        }
        this.bindings = new JavaDepthRoleBindings(this);
        this.exposure = new JavaDepthRoleExposure(this);
        this.candidate = new JavaDepthRoleCandidate(this);
        this.provenanceHelper = new JavaDepthRoleProvenance(this);
    }

    static Map<String, Map<String, Object>> collect(
            Trees trees, Elements elements, Types types, List<CompilationUnitTree> units,
            Path workspace, Set<String> included, Set<String> failedFiles,
            boolean globallyUnknown, JavaDepthDiagnostics errors) {
        List<SourceType> sources = new ArrayList<>();
        for (CompilationUnitTree unit : units) {
            String file = relative(workspace, unit);
            if (!included.contains(file) || isTestPath(file)) continue;
            JavaDepthRoleSourceIndex.collectTypes(trees, unit, unit.getTypeDecls(), null, file, sources);
        }
        JavaDepthRoles extractor = new JavaDepthRoles(trees, elements, types, sources);
        Map<String, Map<String, Object>> result = new TreeMap<>();
        for (SourceType source : sources) {
            Map<String, Object> role = extractor.role(source, failedFiles.contains(source.file())
                    || globallyUnknown || errors.affectsPackage(packageName(source.element())));
            if (role != null) result.put(source.element.getQualifiedName().toString(), role);
        }
        return result;
    }

    static String packageName(TypeElement type) {
        String qualified = type.getQualifiedName().toString();
        int dot = qualified.lastIndexOf('.');
        return dot < 0 ? "" : qualified.substring(0, dot);
    }

    static String typeKey(TypeMirror type) {
        return type == null ? "" : type.toString();
    }

    private Map<String, Object> role(SourceType source, boolean incomplete) {
        exposureCutoff = false;
        JavaDepthRoleCandidate.Result candidateResult = candidate.analyze(source);
        if (candidateResult == null) return null;
        TypeElement owner = source.element();
        Set<ExecutableElement> exposedMethods = candidateExposureMethods(owner, candidateResult.contracts());
        List<String> externalRoutes = exposure.publicRoutes(owner, candidateResult.contracts(), exposedMethods);
        List<Map<String, Object>> bindings = new ArrayList<>();
        for (TypeElement contract : candidateResult.contracts()) {
            bindings.addAll(bindingsFor(source, contract, candidateResult.matches()));
        }
        List<String> memberIDs = candidateResult.memberIDs();
        memberIDs.sort(String::compareTo);
        externalRoutes.sort(String::compareTo);
        bindings.sort(Comparator.comparing((Map<String, Object> item) -> (String) item.get("member"))
                .thenComparing((Map<String, Object> item) -> (String) item.get("use")));
        String inventoryState = incomplete ? "partial" : "measured";
        String exposureState = incomplete || exposureCutoff ? "partial" : "measured";
        return Map.of(
                "inventory", inventoryState,
                "exposure", exposureState,
                "members", memberIDs,
                "external_routes", externalRoutes,
                "bindings", bindings
        );
    }

    private List<Map<String, Object>> bindingsFor(SourceType implementation, TypeElement contract,
                                                  Map<Element, ExecutableElement> matches) {
        return bindings.findBindings(implementation, contract, matches);
    }

    private Set<ExecutableElement> candidateExposureMethods(TypeElement implementation,
                                                             List<TypeElement> contracts) {
        return exposure.candidateExposureMethods(implementation, contracts);
    }

    SourceType sourceForType(Element element) {
        if (!(element instanceof TypeElement type)) return null;
        return sourcesByType.get(type);
    }

    boolean publiclyAccessible(TypeElement type) {
        Element current = type;
        while (current instanceof TypeElement enclosing) {
            if (!enclosing.getModifiers().contains(Modifier.PUBLIC)) return false;
            current = enclosing.getEnclosingElement();
        }
        return true;
    }

    boolean samePackage(TypeElement left, TypeElement right) {
        String leftName = left.getQualifiedName().toString();
        String rightName = right.getQualifiedName().toString();
        int leftDot = leftName.lastIndexOf('.');
        int rightDot = rightName.lastIndexOf('.');
        String leftPackage = leftDot < 0 ? "" : leftName.substring(0, leftDot);
        String rightPackage = rightDot < 0 ? "" : rightName.substring(0, rightDot);
        return leftPackage.equals(rightPackage);
    }

    boolean sameType(TypeMirror left, TypeMirror right) {
        return types.isSameType(types.erasure(left), types.erasure(right));
    }

    static String methodID(ExecutableElement method) {
        return ((TypeElement) method.getEnclosingElement()).getQualifiedName() + "#" + method;
    }
    static String constructorID(ExecutableElement constructor) {
        return ((TypeElement) constructor.getEnclosingElement()).getQualifiedName() + "#<init>(" +
                constructor.getParameters().stream().map(parameter -> parameter.asType().toString()).reduce((a, b) -> a + "," + b).orElse("") + ")";
    }
    static String fieldID(VariableElement field) {
        return ((TypeElement) field.getEnclosingElement()).getQualifiedName() + "#" + field.getSimpleName();
    }
    Map<String, Object> provenance(SourceType source, Tree tree, ExecutableElement method) {
        return provenanceHelper.evidence(source, tree, method);
    }

    private static String relative(Path workspace, CompilationUnitTree unit) {
        return workspace.relativize(Path.of(unit.getSourceFile().toUri())).toString().replace('\\', '/');
    }

    static boolean isTestPath(String path) {
        String normalized = "/" + path.replace('\\', '/') + "/";
        return normalized.contains("/src/test/") || normalized.contains("/test/") || normalized.contains("/tests/");
    }
}
