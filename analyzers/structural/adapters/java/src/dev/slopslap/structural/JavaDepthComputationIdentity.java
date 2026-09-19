package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.VariableElement;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.Map;
import java.util.Set;

/** Canonical structural identity façade with a per-boundary session cache. */
final class JavaDepthComputationIdentity {
    record Result(String identity, boolean connected, boolean transformed, Set<VariableElement> stateReads) { }

    static String of(Trees trees, TreePath expressionPath, javax.lang.model.element.TypeElement owner) {
        return assess(trees, expressionPath, owner).identity();
    }

    static Result assess(Trees trees, TreePath expressionPath, javax.lang.model.element.TypeElement owner) {
        if (trees == null || expressionPath == null || owner == null
                || !(expressionPath.getLeaf() instanceof ExpressionTree)) return empty();
        TreePath methodPath = JavaDepthComputationSupport.enclosing(expressionPath, MethodTree.class);
        TreePath ownerPath = JavaDepthComputationSupport.enclosing(expressionPath, ClassTree.class);
        if (methodPath == null || ownerPath == null) return empty();
        try {
            return new JavaDepthComputationCanonicalizer(trees, owner, ownerPath, methodPath).result(expressionPath);
        } catch (JavaDepthComputationSupport.Limit ignored) {
            return empty();
        }
    }

    static final class Session {
        private final Trees trees;
        private final Map<Tree, JavaDepthComputationCanonicalizer> methods = new IdentityHashMap<>();
        private final Set<Tree> unsupported = Collections.newSetFromMap(new IdentityHashMap<>());
        private final Map<Tree, Result> results = new IdentityHashMap<>();
        Session(Trees trees) { this.trees = trees; }
        Result assess(TreePath expressionPath, javax.lang.model.element.TypeElement owner) {
            if (expressionPath == null) return empty();
            Result known = results.get(expressionPath.getLeaf());
            if (known != null) return known;
            TreePath methodPath = JavaDepthComputationSupport.enclosing(expressionPath, MethodTree.class);
            TreePath ownerPath = JavaDepthComputationSupport.enclosing(expressionPath, ClassTree.class);
            Result result = empty();
            if (methodPath != null && ownerPath != null && !unsupported.contains(methodPath.getLeaf())) {
                try {
                    JavaDepthComputationCanonicalizer method = methods.get(methodPath.getLeaf());
                    if (method == null) {
                        method = new JavaDepthComputationCanonicalizer(trees, owner, ownerPath, methodPath);
                        methods.put(methodPath.getLeaf(), method);
                    }
                    result = method.result(expressionPath);
                } catch (JavaDepthComputationSupport.Limit ignored) {
                    unsupported.add(methodPath.getLeaf());
                }
            }
            results.put(expressionPath.getLeaf(), result);
            return result;
        }
    }

    static boolean meaningful(String identity) {
        return identity != null && identity.startsWith("op[dyn](");
    }

    private static Result empty() { return new Result(null, false, false, Set.of()); }
    private JavaDepthComputationIdentity() { }
}
