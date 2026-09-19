package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import javax.lang.model.element.*;
import java.util.*;

/** Bounded source-helper expansion with argument substitution and cleanup. */
final class JavaDepthComputationCallExpansion {
    private final JavaDepthComputationState state;
    private final JavaDepthComputationExpressions expressions;
    JavaDepthComputationCallExpansion(JavaDepthComputationState state, JavaDepthComputationExpressions expressions) {
        this.state = state; this.expressions = expressions;
    }
    JavaDepthComputationState.Node invocation(TreePath path, MethodInvocationTree tree) {
        Element element = state.trees.getElement(path);
        if (!(element instanceof ExecutableElement method) || !sourceHelper(method, path, tree.getMethodSelect())) return null;
        if (++state.helpers > JavaDepthComputationState.MAX_HELPERS || state.activeHelpers.contains(method)) throw new JavaDepthComputationSupport.Limit();
        TreePath targetPath = state.methodPaths.get(method);
        if (targetPath == null && method.getModifiers().contains(Modifier.STATIC)) targetPath = state.trees.getPath(method);
        if (targetPath == null || !(targetPath.getLeaf() instanceof MethodTree target) || target.getBody() == null || target.getBody().getStatements().isEmpty()) return null;
        List<? extends StatementTree> statements = target.getBody().getStatements();
        if (!(statements.get(statements.size() - 1) instanceof ReturnTree returned) || returned.getExpression() == null || tree.getArguments().size() != method.getParameters().size()) return null;
        List<JavaDepthComputationState.Node> arguments = new ArrayList<>();
        for (ExpressionTree argument : tree.getArguments()) { JavaDepthComputationState.Node value = expressions.canonical(state.child(path, argument)); if (value == null) return null; arguments.add(value); }
        Map<VariableElement, TreePath> helperInitializers = new IdentityHashMap<>();
        for (int index = 0; index < statements.size() - 1; index++) {
            if (!(statements.get(index) instanceof VariableTree variable) || variable.getInitializer() == null) return null;
            TreePath variablePath = TreePath.getPath(targetPath, variable); Element localElement = variablePath == null ? null : state.trees.getElement(variablePath);
            if (!(localElement instanceof VariableElement local)) return null;
            TreePath initializer = TreePath.getPath(variablePath, variable.getInitializer()); if (initializer == null) return null;
            helperInitializers.put(local, initializer);
        }
        Map<VariableElement, JavaDepthComputationState.Node> previous = new IdentityHashMap<>();
        for (int index = 0; index < arguments.size(); index++) { VariableElement parameter = method.getParameters().get(index); previous.put(parameter, state.substitutions.put(parameter, arguments.get(index))); }
        Map<VariableElement, JavaDepthComputationState.Binding> previousLocals = new IdentityHashMap<>();
        for (Map.Entry<VariableElement, TreePath> entry : helperInitializers.entrySet()) previousLocals.put(entry.getKey(), state.locals.put(entry.getKey(), new JavaDepthComputationState.Binding(entry.getValue())));
        state.activeHelpers.add(method); JavaDepthComputationState.Node result;
        try { result = expressions.canonical(TreePath.getPath(targetPath, returned.getExpression())); }
        finally {
            state.activeHelpers.remove(method);
            for (Map.Entry<VariableElement, JavaDepthComputationState.Binding> entry : previousLocals.entrySet()) { if (entry.getValue() == null) state.locals.remove(entry.getKey()); else state.locals.put(entry.getKey(), entry.getValue()); }
            for (Map.Entry<VariableElement, JavaDepthComputationState.Node> entry : previous.entrySet()) { if (entry.getValue() == null) state.substitutions.remove(entry.getKey()); else state.substitutions.put(entry.getKey(), entry.getValue()); }
        }
        return result;
    }
    private boolean sourceHelper(ExecutableElement method, TreePath invocationPath, ExpressionTree select) {
        if (!method.getModifiers().contains(Modifier.STATIC)) return state.owner.equals(method.getEnclosingElement()) && method.getModifiers().contains(Modifier.PRIVATE) && state.helperReceiver(select);
        TreePath targetPath = state.trees.getPath(method); if (targetPath == null || !(method.getEnclosingElement() instanceof TypeElement targetOwner)) return false;
        if (select instanceof IdentifierTree) return true; if (!(select instanceof MemberSelectTree member)) return false;
        TreePath receiverPath = TreePath.getPath(invocationPath, member.getExpression()); Element receiver = receiverPath == null ? null : state.trees.getElement(receiverPath);
        return receiver != null && receiver.equals(targetOwner);
    }
}
