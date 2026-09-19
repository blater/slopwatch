package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.element.RecordComponentElement;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Canonical and compact-constructor assignment proof for passive records. */
final class JavaDepthPassiveValueObjectConstructorBody {
    private JavaDepthPassiveValueObjectConstructorBody() { }

    static boolean canonical(Trees trees, ExecutableElement constructor,
                             List<RecordComponentElement> components,
                             Map<String, RecordComponentElement> byName) {
        if (constructor.isVarArgs() || !constructor.getTypeParameters().isEmpty()
                || !constructor.getThrownTypes().isEmpty() || constructor.getParameters().size() != components.size()) return false;
        for (int index = 0; index < components.size(); index++) {
            if (!sameType(constructor.getParameters().get(index).asType(), components.get(index).asType())) return false;
        }
        TreePath path = trees.getPath(constructor);
        if (path == null || !(path.getLeaf() instanceof MethodTree method) || method.getBody() == null) return true;
        List<? extends StatementTree> statements = method.getBody().getStatements();
        if (statements.isEmpty()) return true;
        Map<Element, VariableElement> parameters = new HashMap<>();
        for (VariableElement parameter : constructor.getParameters()) parameters.put(parameter, parameter);
        Set<String> assigned = new HashSet<>();
        Set<Element> copied = new HashSet<>();
        boolean assignmentSeen = false;
        for (StatementTree statement : statements) {
            if (recordSuper(trees, path, statement)) continue;
            if (!(statement instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)) return false;
            TreePath statementPath = TreePath.getPath(path, statement);
            String field = JavaDepthPassiveValueObjectAccessors.directField(
                    trees, statementPath, assignment.getVariable(), byName);
            Element target = trees.getElement(TreePath.getPath(statementPath, assignment.getVariable()));
            Tree valueExpression = assignment.getExpression();
            boolean defensiveCopy = valueExpression instanceof MethodInvocationTree;
            if (defensiveCopy) {
                MethodInvocationTree call = (MethodInvocationTree) valueExpression;
                Element resolved = trees.getElement(TreePath.getPath(statementPath, call));
                if (!(resolved instanceof ExecutableElement methodTarget)
                        || !methodTarget.getModifiers().contains(Modifier.STATIC)
                        || !methodTarget.getSimpleName().contentEquals("copyOf")
                        || !(methodTarget.getEnclosingElement() instanceof TypeElement type)
                        || !type.getQualifiedName().contentEquals("java.util.List")
                        || call.getArguments().size() != 1) return false;
                valueExpression = call.getArguments().get(0);
            }
            Element value = trees.getElement(TreePath.getPath(statementPath, valueExpression));
            if (field == null || !(valueExpression instanceof IdentifierTree)
                    || !(value instanceof VariableElement parameter) || !parameters.containsKey(parameter)
                    || !parameter.getSimpleName().contentEquals(field)
                    || !sameType(parameter.asType(), byName.get(field).asType())) return false;
            if (target != null && target.getKind() == ElementKind.PARAMETER) {
                if (!defensiveCopy || !target.equals(parameter) || !copied.add(target)) return false;
            } else {
                if (target == null || target.getKind() != ElementKind.FIELD
                        || !target.getEnclosingElement().equals(constructor.getEnclosingElement())
                        || !assigned.add(field)) return false;
                assignmentSeen = true;
            }
        }
        return !assignmentSeen || assigned.size() == components.size();
    }

    private static boolean recordSuper(Trees trees, TreePath constructorPath, StatementTree statement) {
        if (!(statement instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof MethodInvocationTree invocation)
                || !invocation.getMethodSelect().toString().contentEquals("super")
                || !invocation.getArguments().isEmpty()) return false;
        Element target = trees.getElement(TreePath.getPath(constructorPath, invocation));
        return target instanceof ExecutableElement constructor && constructor.getKind() == ElementKind.CONSTRUCTOR
                && constructor.getEnclosingElement() instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.Record");
    }

    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
