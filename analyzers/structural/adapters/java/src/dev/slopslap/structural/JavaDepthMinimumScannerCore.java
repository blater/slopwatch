package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.*;

final class JavaDepthMinimumScannerCore {
    final JavaDepthMinimumBehavior host;
    final Trees trees;
    final JavaDepthComputationIdentity.Session computations;
    final TreePath ownerPath;
    final Map<ExecutableElement, JavaDepthMinimumSurface.Callable> allMethods;
    final Set<ExecutableElement> active;
    final Set<String> limitations;
    final Map<TypeElement, JavaDepthMinimumMonitors> monitorCache;
    final Set<String> sourceDelegations;
    final JavaDepthMinimumBehavior.Summary result;
    final TypeElement owner;
    final JavaDepthMinimumMonitors monitors;
    final Set<Element> connected = Collections.newSetFromMap(new IdentityHashMap<>());
    final Map<Element, JavaDepthMinimumBehavior.ExprFacts> locals = new IdentityHashMap<>();
    final Set<VariableElement> controllingFields = Collections.newSetFromMap(new IdentityHashMap<>());
    final Map<VariableElement, Boolean> booleanState = new IdentityHashMap<>();
    List<JavaDepthMinimumBehavior.PathAlternative> paths = new ArrayList<>();
    final Deque<Tree> breakTargets = new ArrayDeque<>();
    final boolean constructor;
    int stateEffects;

    JavaDepthMinimumScannerCore(JavaDepthMinimumBehavior host, JavaDepthMinimumBehavior.Summary result, ExecutableElement method) {
        this.host = host;
        this.trees = host.trees;
        this.computations = host.computations;
        this.ownerPath = host.ownerPath;
        this.allMethods = host.allMethods;
        this.active = host.active;
        this.limitations = host.limitations;
        this.monitorCache = host.monitorCache;
        this.sourceDelegations = host.sourceDelegations;
        this.result = result;
        this.owner = (TypeElement) method.getEnclosingElement();
        this.monitors = monitorCache.computeIfAbsent(owner,
                type -> new JavaDepthMinimumMonitors(trees, trees.getPath(type), type));
        constructor = method.getKind() == ElementKind.CONSTRUCTOR;
        paths.add(new JavaDepthMinimumBehavior.PathAlternative());
        connected.addAll(method.getParameters());
        for (Element enclosed : owner.getEnclosedElements()) {
            if (enclosed instanceof VariableElement field && field.getConstantValue() == null && !constructor) connected.add(field);
        }
    }

    boolean supportedStatement(Tree.Kind kind) {
        return switch (kind) {
            case BLOCK, VARIABLE, EXPRESSION_STATEMENT, IF, RETURN, THROW,
                    WHILE_LOOP, FOR_LOOP, ENHANCED_FOR_LOOP, SYNCHRONIZED,
                    SWITCH, TRY, BREAK, EMPTY_STATEMENT -> true;
            default -> false;
        };
    }

    void unsupportedControl(Tree tree) {
        tick();
        limitations.add(result.id + ": unsupported control transfer " + tree.getKind()
                + "; nested and subsequent effects are not assumed to execute");
        List<JavaDepthMinimumBehavior.PathAlternative> opaqueExits = copyPaths(paths);
        for (JavaDepthMinimumBehavior.PathAlternative path : opaqueExits) if (path.live) path.live = false;
        mergePaths(paths, opaqueExits);
        locals.clear();
        booleanState.clear();
    }

    void creditCoordination(String monitor) {
        if (monitor == null) return;
        for (JavaDepthMinimumBehavior.PathAlternative path : paths) {
            for (String credit : new ArrayList<>(path.credits)) {
                int separator = credit.indexOf('\u0000');
                if (separator <= 0 || !credit.startsWith("C\u0000")) continue;
                String field = credit.substring(separator + 1);
                if (!monitors.protects(field, monitor)) continue;
                String root = field + "/monitor/" + monitor;
                result.credit("Y", root);
                path.credits.add("Y\u0000" + root);
                break;
            }
        }
    }

    void credit(String category, String root) {
        result.credit(category, root);
        if (root == null || root.isEmpty()) return;
        String value = category + "\u0000" + root;
        for (JavaDepthMinimumBehavior.PathAlternative path : paths) if (path.live) path.credits.add(value);
    }

    void terminatePaths(boolean exceptional) {
        for (JavaDepthMinimumBehavior.PathAlternative path : paths) if (path.live) {
            path.live = false;
            path.exceptional = exceptional;
        }
    }

    List<JavaDepthMinimumBehavior.PathAlternative> copyPaths(List<JavaDepthMinimumBehavior.PathAlternative> source) {
        List<JavaDepthMinimumBehavior.PathAlternative> result = new ArrayList<>(source.size());
        for (JavaDepthMinimumBehavior.PathAlternative path : source) result.add(path.copy());
        return result;
    }

    void mergePaths(List<JavaDepthMinimumBehavior.PathAlternative> thenPaths, List<JavaDepthMinimumBehavior.PathAlternative> elsePaths) {
        Map<String, JavaDepthMinimumBehavior.PathAlternative> unique = new TreeMap<>();
        for (JavaDepthMinimumBehavior.PathAlternative path : thenPaths) unique.put(path.key(), path);
        for (JavaDepthMinimumBehavior.PathAlternative path : elsePaths) unique.putIfAbsent(path.key(), path);
        paths = new ArrayList<>(unique.values());
        if (paths.size() > 32) {
            host.exhausted = true;
            limitations.add(result.id + ": branch alternatives exceeded 32");
            paths = new ArrayList<>(paths.subList(0, 32));
        }
    }

    List<JavaDepthMinimumBehavior.PathAlternative> deduplicatePaths(List<JavaDepthMinimumBehavior.PathAlternative> candidates) {
        Map<String, JavaDepthMinimumBehavior.PathAlternative> unique = new TreeMap<>();
        for (JavaDepthMinimumBehavior.PathAlternative path : candidates) unique.putIfAbsent(path.key(), path);
        List<JavaDepthMinimumBehavior.PathAlternative> result = new ArrayList<>(unique.values());
        if (result.size() > 32) {
            host.exhausted = true;
            limitations.add(resultId() + ": branch alternatives exceeded 32");
            return new ArrayList<>(result.subList(0, 32));
        }
        return result;
    }

    String resultId() { return result.id; }

    void mergeHelperAlternatives(List<Set<String>> helperPaths) {
        if (helperPaths.isEmpty()) return;
        Map<String, JavaDepthMinimumBehavior.PathAlternative> merged = new TreeMap<>();
        for (JavaDepthMinimumBehavior.PathAlternative caller : paths) {
            if (!caller.live) {
                merged.putIfAbsent(caller.key(), caller.copy());
                continue;
            }
            for (Set<String> helper : helperPaths) {
                JavaDepthMinimumBehavior.PathAlternative combined = caller.copy();
                combined.credits.addAll(helper);
                merged.putIfAbsent(combined.key(), combined);
            }
        }
        paths = new ArrayList<>(merged.values());
        if (paths.size() > 32) {
            host.exhausted = true;
            limitations.add(result.id + ": delegated alternatives exceeded 32");
            paths = new ArrayList<>(paths.subList(0, 32));
        }
    }

    boolean sameExpressionFacts(JavaDepthMinimumBehavior.ExprFacts first, JavaDepthMinimumBehavior.ExprFacts second) {
        return second != null && first.connected == second.connected && first.transform == second.transform
                && first.reads.equals(second.reads) && first.categories.equals(second.categories);
    }

    Element element(Tree tree, TreePath parent) {
        TreePath path = TreePath.getPath(parent, tree);
        return path == null ? null : trees.getElement(path);
    }

    void tick() {
        if (++host.work > JavaDepthMinimumBehavior.MAX_WORK) {
            host.exhausted = true;
            throw new WorkLimit();
        }
    }

}
