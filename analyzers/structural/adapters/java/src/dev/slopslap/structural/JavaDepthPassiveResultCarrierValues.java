package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;

/** Getter and value-expression checks for result carriers. */
final class JavaDepthPassiveResultCarrierValues {
    private JavaDepthPassiveResultCarrierValues() { }

    static boolean pureInitializer(Trees trees, TreePath ownerPath, VariableTree variable,
                                   Map<Element, VariableElement> fields) {
        Element element = trees.getElement(new TreePath(ownerPath, variable));
        if (!(element instanceof VariableElement field) || !fields.containsKey(field)) return false;
        return variable.getInitializer() == null || JavaDepthPassiveResultCarrierValueExpressions.allowedValue(trees, new TreePath(ownerPath, variable),
                variable.getInitializer(), field.asType(), Map.of());
    }

    static boolean getter(Trees trees, TreePath methodPath, ExecutableElement method,
                          Map<Element, VariableElement> fields, Set<VariableElement> getters) {
        if (method.getReturnType().getKind() == TypeKind.VOID || !method.getParameters().isEmpty()) return false;
        MethodTree tree = (MethodTree) methodPath.getLeaf();
        if (tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        VariableElement field = directField(trees, methodPath, returned.getExpression(), fields);
        if (field == null || !sameType(field.asType(), method.getReturnType())) return false;
        getters.add(field); return true;
    }

    static VariableElement directField(Trees trees, TreePath parent, Tree expression,
                                       Map<Element, VariableElement> fields) {
        TreePath path = TreePath.getPath(parent, expression);
        Element element = path == null ? null : trees.getElement(path);
        if (!(element instanceof VariableElement field) || !fields.containsKey(field)) return null;
        if (expression instanceof IdentifierTree) return field;
        return expression instanceof MemberSelectTree member && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this") ? field : null;
    }

    private static boolean sameType(TypeMirror left, TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
