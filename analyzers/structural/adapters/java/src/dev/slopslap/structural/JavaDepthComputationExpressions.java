package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import java.util.*;

/** Expression grammar and reductions for canonical computation identity. */
final class JavaDepthComputationExpressions {
    interface Resolver {
        JavaDepthComputationState.Node variable(TreePath path, IdentifierTree tree);
        JavaDepthComputationState.Node member(TreePath path, MemberSelectTree tree);
        JavaDepthComputationState.Node invocation(TreePath path, MethodInvocationTree tree);
    }
    private final JavaDepthComputationState state;
    private final JavaDepthComputationAlgebra algebra;
    private final Resolver resolver;
    JavaDepthComputationExpressions(JavaDepthComputationState state, JavaDepthComputationAlgebra algebra, Resolver resolver) {
        this.state = state; this.algebra = algebra; this.resolver = resolver;
    }
    JavaDepthComputationState.Node canonical(TreePath path) {
        if (path == null || ++state.nodes > JavaDepthComputationState.MAX_NODES) throw new JavaDepthComputationSupport.Limit();
        Tree tree = path.getLeaf();
        if (tree instanceof ParenthesizedTree parenthesized) return canonical(state.child(path, parenthesized.getExpression()));
        if (tree instanceof LiteralTree literal) return state.literal(literal, path);
        if (tree instanceof IdentifierTree identifier) return resolver.variable(path, identifier);
        if (tree instanceof MemberSelectTree member) return resolver.member(path, member);
        if (tree instanceof BinaryTree binary) return binary(path, binary);
        if (tree instanceof UnaryTree unary) return operator(path, unary.getKind().name(), List.of(unary.getExpression()));
        if (tree instanceof ConditionalExpressionTree conditional) return conditional(path, conditional);
        if (tree instanceof ArrayAccessTree array) return operator(path, "index", List.of(array.getExpression(), array.getIndex()));
        if (tree instanceof MethodInvocationTree invocation) return resolver.invocation(path, invocation);
        return null;
    }
    private JavaDepthComputationState.Node binary(TreePath path, BinaryTree tree) {
        Object constant = JavaDepthMinimumExpressions.constant(tree, path.getParentPath(), state.trees);
        String type = state.type(path);
        if (constant != null && type != null) return state.node("literal(" + type + ":" + JavaDepthComputationState.value(constant) + ")", false, false, constant);
        ExpressionTree identity = JavaDepthMinimumExpressions.identity(tree, path.getParentPath(), state.trees);
        if (identity != null) return canonical(state.child(path, identity));
        return operator(path, tree.getKind().name(), List.of(tree.getLeftOperand(), tree.getRightOperand()));
    }
    private JavaDepthComputationState.Node conditional(TreePath path, ConditionalExpressionTree tree) {
        JavaDepthComputationState.Node condition = canonical(state.child(path, tree.getCondition()));
        if (condition != null && condition.constant() instanceof Boolean value) return canonical(
                state.child(path, value ? tree.getTrueExpression() : tree.getFalseExpression()));
        JavaDepthComputationState.Node trueValue = canonical(state.child(path, tree.getTrueExpression()));
        JavaDepthComputationState.Node falseValue = canonical(state.child(path, tree.getFalseExpression()));
        if (trueValue == null || falseValue == null || condition == null) return null;
        if (trueValue.text().equals(falseValue.text())) return trueValue;
        return operator(path, "conditional", List.of(tree.getCondition(), tree.getTrueExpression(), tree.getFalseExpression()));
    }
    private JavaDepthComputationState.Node operator(TreePath path, String operation, List<? extends Tree> operands) {
        String type = state.type(path); if (type == null) return null;
        List<JavaDepthComputationState.Node> values = new ArrayList<>();
        for (Tree operand : operands) { JavaDepthComputationState.Node value = canonical(state.child(path, operand)); if (value == null) return null; values.add(value); }
        boolean connected = values.stream().anyMatch(JavaDepthComputationState.Node::connected);
        JavaDepthComputationState.Node identity = algebra.identity(operation, values, type); if (identity != null) return identity;
        Object constant = algebra.constant(operation, values);
        if (constant != null) return state.node("literal(" + type + ":" + JavaDepthComputationState.value(constant) + ")", false, false, constant);
        Set<javax.lang.model.element.VariableElement> stateReads = new HashSet<>(); for (var operand : values) stateReads.addAll(operand.stateReads());
        String prefix = connected ? "op[dyn](" : "op[const](";
        String operandsText = values.stream().map(JavaDepthComputationState.Node::text).reduce((left, right) -> left + "," + right).orElse("");
        return new JavaDepthComputationState.Node(prefix + operation + ":" + type + ";" + operandsText + ")", connected, connected, null, Set.copyOf(stateReads));
    }
}
