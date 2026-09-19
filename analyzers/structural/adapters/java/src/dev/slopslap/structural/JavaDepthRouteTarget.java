package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;

/** Recognizes same-owner static targets and their optional validation guards. */
final class JavaDepthRouteTarget {
    private JavaDepthRouteTarget() { }
    static ExecutableElement forwardedTarget(Trees trees, TreePath ownerPath, TypeElement owner, ExecutableElement method) {
        if (method.getModifiers().contains(Modifier.ABSTRACT) || method.getModifiers().contains(Modifier.NATIVE)
                || method.getModifiers().contains(Modifier.SYNCHRONIZED)) return null;
        TreePath path = trees.getPath(method);
        if (path == null || !(path.getLeaf() instanceof MethodTree tree) || tree.getBody() == null) return null;
        ReturnTree returned = returnedCall(trees, path, method, tree.getBody());
        if (returned == null || !(returned.getExpression() instanceof MethodInvocationTree call)) return null;
        ExecutableElement target = JavaDepthCalls.resolve(trees, path, call);
        if (target == null || target.getKind() != ElementKind.METHOD || target.equals(method)
                || !owner.equals(target.getEnclosingElement()) || !target.getModifiers().contains(Modifier.STATIC)
                || !JavaDepthCalls.staticSelection(trees, path, call, target)
                || !JavaDepthCalls.exactArgumentTypes(trees, path, call, target)) return null;
        return target;
    }
    static ReturnTree returnedCall(Trees trees, TreePath methodPath, ExecutableElement method, BlockTree body) {
        java.util.List<? extends StatementTree> statements = body.getStatements();
        if (statements.isEmpty()) return null;
        StatementTree last = statements.get(statements.size() - 1);
        if (!(last instanceof ReturnTree returned) || !(returned.getExpression() instanceof MethodInvocationTree)) return null;
        for (int index = 0; index + 1 < statements.size(); index++) {
            if (!validationGuard(trees, methodPath, method, statements.get(index))) return null;
        }
        return returned;
    }
    private static boolean validationGuard(Trees trees, TreePath path, ExecutableElement method, StatementTree statement) {
        return statement instanceof IfTree branch && branch.getElseStatement() == null
                && JavaDepthRoutePurity.predicate(trees, path, method, branch.getCondition())
                && throwsIllegalArgument(trees, path, branch.getThenStatement());
    }
    private static boolean throwsIllegalArgument(Trees trees, TreePath path, Tree tree) {
        if (tree instanceof ThrowTree thrown) return isIllegalArgument(trees, path, thrown.getExpression());
        if (!(tree instanceof BlockTree block) || block.getStatements().size() != 1) return false;
        StatementTree statement = block.getStatements().get(0);
        return statement instanceof ThrowTree thrown && isIllegalArgument(trees, path, thrown.getExpression());
    }
    private static boolean isIllegalArgument(Trees trees, TreePath path, ExpressionTree expression) {
        return expression instanceof NewClassTree created && isIllegalArgumentType(trees, path, created)
                && created.getArguments().stream().allMatch(value -> value instanceof LiteralTree);
    }
    private static boolean isIllegalArgumentType(Trees trees, TreePath path, NewClassTree created) {
        Element element = trees.getElement(TreePath.getPath(path, created.getIdentifier()));
        return element instanceof TypeElement type && type.getQualifiedName().contentEquals("java.lang.IllegalArgumentException");
    }
}
