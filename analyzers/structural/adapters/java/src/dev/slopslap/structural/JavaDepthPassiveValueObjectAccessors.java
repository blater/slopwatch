package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.element.RecordComponentElement;
import javax.lang.model.type.TypeKind;
import java.util.Map;

/** Direct component accessor and field spelling checks for passive records. */
final class JavaDepthPassiveValueObjectAccessors {
    private JavaDepthPassiveValueObjectAccessors() { }

    static boolean isAccessor(Trees trees, ExecutableElement method,
                              Map<String, RecordComponentElement> components) {
        if (method.getKind() != ElementKind.METHOD || !method.getParameters().isEmpty()
                || method.getReturnType().getKind() == TypeKind.VOID) return false;
        RecordComponentElement component = components.get(method.getSimpleName().toString());
        if (component == null || !sameType(component.asType(), method.getReturnType())) return false;
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree)) return true;
        if (tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        String field = directField(trees, TreePath.getPath(methodPath, returned), returned.getExpression(), components);
        return field != null && component.getSimpleName().contentEquals(field);
    }

    static String directField(Trees trees, TreePath parent, Tree expression,
                              Map<String, ? extends Element> components) {
        if (parent == null) return null;
        Element element = trees.getElement(TreePath.getPath(parent, expression));
        if (!(element instanceof VariableElement field) || !components.containsKey(field.getSimpleName().toString())) return null;
        if (expression instanceof IdentifierTree) return field.getSimpleName().toString();
        return expression instanceof MemberSelectTree member && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this") ? field.getSimpleName().toString() : null;
    }

    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
