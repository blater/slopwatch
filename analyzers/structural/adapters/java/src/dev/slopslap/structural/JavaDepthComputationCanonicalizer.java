package dev.slopslap.structural;

import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.TypeElement;

/** Orchestrates the bounded expression, binding, call, and algebra collaborators. */
final class JavaDepthComputationCanonicalizer implements JavaDepthComputationExpressions.Resolver {
    private final JavaDepthComputationState state;
    private final JavaDepthComputationExpressions expressions;
    private final JavaDepthComputationBindings bindings;
    private final JavaDepthComputationCallExpansion calls;
    JavaDepthComputationCanonicalizer(Trees trees, TypeElement owner, TreePath ownerPath, TreePath methodPath) {
        state = new JavaDepthComputationState(trees, owner, ownerPath, methodPath);
        JavaDepthComputationAlgebra algebra = new JavaDepthComputationAlgebra();
        expressions = new JavaDepthComputationExpressions(state, algebra, this);
        bindings = new JavaDepthComputationBindings(state, expressions);
        calls = new JavaDepthComputationCallExpansion(state, expressions);
    }
    JavaDepthComputationIdentity.Result result(TreePath path) {
        // Budgets apply to each root computation while binding/index state is shared by the method.
        // Keep this reset at the orchestrator boundary, matching the original per-result semantics.
        state.nodes = 0;
        state.helpers = 0;
        try {
            JavaDepthComputationState.Node node = expressions.canonical(path);
            return node == null ? empty() : new JavaDepthComputationIdentity.Result(
                    node.text(), node.connected(), node.transformed(), node.stateReads());
        } catch (JavaDepthComputationSupport.Limit ignored) {
            return empty();
        }
    }
    @Override public JavaDepthComputationState.Node variable(TreePath path, IdentifierTree tree) { return bindings.variable(path, tree); }
    @Override public JavaDepthComputationState.Node member(TreePath path, MemberSelectTree tree) { return bindings.member(path, tree); }
    @Override public JavaDepthComputationState.Node invocation(TreePath path, MethodInvocationTree tree) { return calls.invocation(path, tree); }
    private static JavaDepthComputationIdentity.Result empty() {
        return new JavaDepthComputationIdentity.Result(null, false, false, java.util.Set.of());
    }
}
