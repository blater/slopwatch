package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.TypeKind;
import java.util.Map;

/** Direct getter checks for passive enum metadata fields. */
final class JavaDepthPassiveEnumAccessors {
    private JavaDepthPassiveEnumAccessors() { }
    static boolean instance(Trees trees, ExecutableElement method, Map<String, VariableElement> fields) {
        if (method.getKind() != ElementKind.METHOD || method.getModifiers().contains(Modifier.STATIC)
                || !method.getParameters().isEmpty() || method.getReturnType().getKind() == TypeKind.VOID) return false;
        VariableElement field = fields.get(method.getSimpleName().toString());
        if (field == null || !sameType(field.asType(), method.getReturnType())) return false;
        TreePath path = trees.getPath(method);
        if (path == null || !(path.getLeaf() instanceof MethodTree tree)) return true;
        if (tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        return directField(trees, TreePath.getPath(path, returned), returned.getExpression(), fields) == field;
    }
    static boolean staticAccessor(Trees trees, ExecutableElement method, Map<String, VariableElement> fields) {
        if (method.getKind() != ElementKind.METHOD || !method.getModifiers().contains(Modifier.STATIC)
                || !method.getParameters().isEmpty() || method.getReturnType().getKind() == TypeKind.VOID) return false;
        TreePath path = trees.getPath(method);
        if (path == null || !(path.getLeaf() instanceof MethodTree tree) || tree.getBody() == null
                || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        VariableElement field = directField(trees, TreePath.getPath(path, returned), returned.getExpression(), fields);
        return field != null && sameType(field.asType(), method.getReturnType());
    }
    static VariableElement directField(Trees trees, TreePath parent, Tree expression, Map<String, VariableElement> fields) {
        if (parent == null) return null;
        Element element = trees.getElement(TreePath.getPath(parent, expression));
        if (!(element instanceof VariableElement field) || !fields.containsValue(field)) return null;
        if (expression instanceof IdentifierTree) return field;
        return expression instanceof MemberSelectTree member && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this") ? field : null;
    }
    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
