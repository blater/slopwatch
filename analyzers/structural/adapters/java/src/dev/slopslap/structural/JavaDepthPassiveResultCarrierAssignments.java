package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/** Direct assignment and constructor-super checks for result carriers. */
final class JavaDepthPassiveResultCarrierAssignments {
    private JavaDepthPassiveResultCarrierAssignments() { }

    static boolean directAssignments(Trees trees, TreePath methodPath, BlockTree body,
                                     ExecutableElement method, Map<Element, VariableElement> fields) {
        if (body == null) return false;
        Map<Element, VariableElement> parameters = new HashMap<>();
        for (VariableElement parameter : method.getParameters()) parameters.put(parameter, parameter);
        List<? extends StatementTree> statements = body.getStatements();
        if (statements.isEmpty()) return false;
        for (StatementTree statement : statements) {
            if (method.getKind() == ElementKind.CONSTRUCTOR && objectSuper(trees, methodPath, statement)) continue;
            if (!(statement instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)) return false;
            TreePath statementPath = TreePath.getPath(methodPath, statement);
            if (statementPath == null) return false;
            VariableElement field = JavaDepthPassiveResultCarrierValues.directField(
                    trees, statementPath, assignment.getVariable(), fields);
            if (field == null || !JavaDepthPassiveResultCarrierValueExpressions.allowedValue(
                    trees, statementPath, assignment.getExpression(), field.asType(), parameters)) return false;
        }
        return true;
    }

    private static boolean objectSuper(Trees trees, TreePath methodPath, StatementTree statement) {
        if (!(statement instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof MethodInvocationTree invocation)
                || !invocation.getMethodSelect().toString().contentEquals("super")
                || !invocation.getArguments().isEmpty()) return false;
        Element target = trees.getElement(TreePath.getPath(methodPath, invocation));
        return target instanceof ExecutableElement constructor && constructor.getKind() == ElementKind.CONSTRUCTOR
                && constructor.getEnclosingElement() instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.Object");
    }
}
