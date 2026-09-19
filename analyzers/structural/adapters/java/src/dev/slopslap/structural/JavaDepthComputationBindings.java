package dev.slopslap.structural;

import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.util.TreePath;
import javax.lang.model.element.*;

/** Local and field binding stage for computation identity. */
final class JavaDepthComputationBindings {
    private final JavaDepthComputationState state;
    private final JavaDepthComputationExpressions expressions;
    JavaDepthComputationBindings(JavaDepthComputationState state, JavaDepthComputationExpressions expressions) {
        this.state = state; this.expressions = expressions;
    }
    JavaDepthComputationState.Node member(TreePath path, MemberSelectTree tree) {
        Element element = state.trees.getElement(path);
        if (!(element instanceof VariableElement field)) return null;
        if (field.getConstantValue() != null && field.getModifiers().contains(Modifier.STATIC)
                && state.typeReceiver(path, tree.getExpression(), field.getEnclosingElement())) return state.fieldNode(field);
        if (!state.owner.equals(field.getEnclosingElement()) || field.getConstantValue() == null && !state.ownedReceiver(tree.getExpression())) return null;
        return state.fieldNode(field);
    }
    JavaDepthComputationState.Node variable(TreePath path, IdentifierTree tree) {
        Element element = state.trees.getElement(path); if (!(element instanceof VariableElement variable)) return null;
        JavaDepthComputationState.Node replacement = state.substitutions.get(variable); if (replacement != null) return replacement;
        if (variable.getConstantValue() != null && variable.getModifiers().contains(Modifier.STATIC)) return state.fieldNode(variable);
        if (state.owner.equals(variable.getEnclosingElement())) return state.fieldNode(variable);
        if (state.assigned.contains(variable)) return null;
        if (variable.getKind() == ElementKind.PARAMETER) return state.node(state.parameter(variable), true, false);
        JavaDepthComputationState.Binding binding = state.locals.get(variable);
        if (binding == null || binding.initializer == null || !binding.assigned.isEmpty() || binding.active) return null;
        binding.active = true;
        try { return expressions.canonical(binding.initializer); }
        finally { binding.active = false; }
    }
}
