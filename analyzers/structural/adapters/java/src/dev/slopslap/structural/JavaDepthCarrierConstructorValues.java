package dev.slopslap.structural;

import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.util.TreePath;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import java.util.List;
import java.util.Set;

final class JavaDepthCarrierConstructorValues {
    private JavaDepthCarrierConstructorValues() { }

    static int superIndex(JavaDepthCarrier.Inspection inspection, ExecutableElement constructor,
                          List<? extends StatementTree> statements) {
        if (statements.isEmpty()) return 0;
        if (!(statements.get(0) instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof MethodInvocationTree call)) return 0;
        Element target = inspection.trees.getElement(TreePath.getPath(inspection.methodPath(constructor), call));
        if (!(target instanceof ExecutableElement superConstructor)
                || superConstructor.getKind() != ElementKind.CONSTRUCTOR
                || !(superConstructor.getEnclosingElement() instanceof TypeElement type)
                || !"java.lang.Object".equals(type.getQualifiedName().toString())
                || !call.getArguments().isEmpty()) return -1;
        return 1;
    }

    static boolean initialize(JavaDepthCarrier.Inspection inspection, ExecutableElement constructor,
                              AssignmentTree assignment, Set<VariableElement> initialized) {
        Element target = inspection.trees.getElement(TreePath.getPath(inspection.methodPath(constructor), assignment.getVariable()));
        if (!(target instanceof VariableElement field) || !inspection.fields.contains(field)
                || field.getModifiers().contains(javax.lang.model.element.Modifier.STATIC)
                || initialized.contains(field)) return false;
        if (!storedType(inspection, field, assignment.getExpression(), constructor)) return false;
        if (!value(inspection, constructor, assignment.getExpression(), field.asType())) return false;
        initialized.add(field);
        return true;
    }

    private static boolean storedType(JavaDepthCarrier.Inspection inspection, VariableElement field,
                                      ExpressionTree expression, ExecutableElement constructor) {
        var actual = inspection.type(expression, constructor);
        return JavaDepthCarrierFields.sameType(field.asType(), actual)
                || (actual != null && actual.getKind() == javax.lang.model.type.TypeKind.NULL
                && JavaDepthCarrierFields.isString(field.asType()));
    }

    private static boolean value(JavaDepthCarrier.Inspection inspection, ExecutableElement constructor,
                                 ExpressionTree expression, javax.lang.model.type.TypeMirror expected) {
        Element element = inspection.trees.getElement(TreePath.getPath(inspection.methodPath(constructor), expression));
        if (element instanceof VariableElement parameter && constructor.getParameters().contains(parameter)) {
            return JavaDepthCarrierFields.sameType(parameter.asType(), inspection.type(expression, constructor));
        }
        return JavaDepthCarrierFields.constantExpression(inspection, expression, expected,
                inspection.methodPath(constructor));
    }
}
