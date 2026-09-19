package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Constructor assignment proof for passive enums. */
final class JavaDepthPassiveEnumConstructors {
    private JavaDepthPassiveEnumConstructors() { }
    static boolean constructor(Trees trees, ExecutableElement constructor, List<VariableElement> fields,
                               Map<String, VariableElement> byName) {
        if (constructor.isVarArgs() || !constructor.getTypeParameters().isEmpty() || !constructor.getThrownTypes().isEmpty()) return false;
        TreePath path = trees.getPath(constructor);
        if (path == null || !(path.getLeaf() instanceof MethodTree method) || method.getBody() == null) return fields.isEmpty();
        Map<Element, VariableElement> parameters = new HashMap<>();
        for (VariableElement parameter : constructor.getParameters()) parameters.put(parameter, parameter);
        Set<VariableElement> assigned = new HashSet<>();
        for (StatementTree statement : method.getBody().getStatements()) {
            if (enumSuper(trees, path, statement)) continue;
            if (!(statement instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)) return false;
            TreePath statementPath = TreePath.getPath(path, statement);
            VariableElement field = JavaDepthPassiveEnumAccessors.directField(
                    trees, statementPath, assignment.getVariable(), byName);
            Element value = trees.getElement(TreePath.getPath(statementPath, assignment.getExpression()));
            if (field == null || !assigned.add(field) || !(value instanceof VariableElement parameter)
                    || !parameters.containsKey(parameter) || !sameType(field.asType(), parameter.asType())) return false;
        }
        return assigned.size() == fields.size();
    }
    private static boolean enumSuper(Trees trees, TreePath path, StatementTree statement) {
        if (!(statement instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof MethodInvocationTree invocation)
                || !invocation.getMethodSelect().toString().contentEquals("super")) return false;
        Element target = trees.getElement(TreePath.getPath(path, invocation));
        return target instanceof ExecutableElement constructor && constructor.getKind() == ElementKind.CONSTRUCTOR
                && constructor.getEnclosingElement() instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.Enum");
    }
    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
