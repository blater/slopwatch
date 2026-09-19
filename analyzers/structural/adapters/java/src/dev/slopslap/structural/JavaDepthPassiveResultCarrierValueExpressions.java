package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import java.util.Map;

/** Whitelisted constant and parameter expressions accepted by result carriers. */
final class JavaDepthPassiveResultCarrierValueExpressions {
    private JavaDepthPassiveResultCarrierValueExpressions() { }
    static boolean allowedValue(Trees trees, TreePath parent, Tree expression, TypeMirror expected,
                                Map<Element, VariableElement> parameters) {
        TreePath path = TreePath.getPath(parent, expression);
        if (path == null) return false;
        Element element = trees.getElement(path);
        if (element instanceof VariableElement variable && parameters.containsKey(variable)) return sameType(variable.asType(), expected);
        if (expression instanceof UnaryTree unary && (unary.getKind() == Tree.Kind.UNARY_MINUS
                || unary.getKind() == Tree.Kind.UNARY_PLUS) && unary.getExpression() instanceof LiteralTree literal
                && literal.getValue() instanceof Number) return literalTypeMatches(trees.getTypeMirror(path), expected);
        if (expression instanceof LiteralTree literal) {
            return literal.getValue() == null ? expected.getKind() != TypeKind.NULL && !expected.getKind().isPrimitive()
                    : literalTypeMatches(trees.getTypeMirror(path), expected);
        }
        if (element instanceof VariableElement constant && constant.getKind() == ElementKind.ENUM_CONSTANT) {
            return constant.getEnclosingElement() instanceof TypeElement enumType && expected instanceof DeclaredType declared
                    && declared.asElement().equals(enumType);
        }
        if (element instanceof VariableElement constant && constant.getConstantValue() != null
                && constant.getModifiers().contains(Modifier.STATIC) && constant.getModifiers().contains(Modifier.FINAL)) {
            return literalTypeMatches(constant.asType(), expected);
        }
        return false;
    }
    private static boolean literalTypeMatches(TypeMirror actual, TypeMirror expected) {
        if (actual == null || expected == null) return false;
        return sameType(actual, expected) || actual.getKind() == TypeKind.INT && expected.getKind() == TypeKind.LONG;
    }
    private static boolean sameType(TypeMirror left, TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
