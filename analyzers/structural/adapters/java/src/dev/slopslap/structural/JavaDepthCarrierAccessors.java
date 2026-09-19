package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.ReturnTree;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.VariableElement;

final class JavaDepthCarrierAccessors {
    private JavaDepthCarrierAccessors() { }

    static boolean check(JavaDepthCarrier.Inspection inspection) {
        for (ExecutableElement method : JavaDepthCarrierMembers.methods(inspection)) {
            if (method.getKind() == javax.lang.model.element.ElementKind.CONSTRUCTOR) continue;
            if (!getter(inspection, method)) return false;
            if (JavaDepthCarrierMembers.exposed(inspection, method)) inspection.accessors.add(method);
        }
        return true;
    }

    private static boolean getter(JavaDepthCarrier.Inspection inspection, ExecutableElement method) {
        if (method.getModifiers().contains(Modifier.STATIC)
                || method.getModifiers().contains(Modifier.SYNCHRONIZED)
                || method.getModifiers().contains(Modifier.ABSTRACT)
                || !method.getParameters().isEmpty() || !method.getThrownTypes().isEmpty()) return false;
        MethodTree tree = (MethodTree) inspection.treesByElement.get(method);
        if (tree == null || tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        VariableElement field = field(inspection, method, returned.getExpression());
        return field != null && JavaDepthCarrierFields.sameType(method.getReturnType(), field.asType())
                && directField(inspection, method, returned.getExpression(), field);
    }

    private static VariableElement field(JavaDepthCarrier.Inspection inspection, ExecutableElement method,
                                         ExpressionTree expression) {
        Element element = inspection.trees.getElement(
                com.sun.source.util.TreePath.getPath(inspection.methodPath(method), expression));
        if (!(element instanceof VariableElement field) || !inspection.fields.contains(field)
                || field.getModifiers().contains(Modifier.STATIC)) return null;
        return field;
    }

    private static boolean directField(JavaDepthCarrier.Inspection inspection, ExecutableElement method,
                                       ExpressionTree expression, VariableElement expected) {
        if (expression instanceof IdentifierTree) return true;
        if (!(expression instanceof MemberSelectTree member)
                || !(member.getExpression() instanceof IdentifierTree thisReference)
                || !thisReference.getName().contentEquals("this")) return false;
        Element element = inspection.trees.getElement(
                com.sun.source.util.TreePath.getPath(inspection.methodPath(method), member));
        return element == expected;
    }
}
