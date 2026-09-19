package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.ParenthesizedTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.UnaryTree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.TreePath;
import javax.lang.model.element.Element;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeMirror;

final class JavaDepthCarrierFields {
    private JavaDepthCarrierFields() { }

    static boolean check(JavaDepthCarrier.Inspection inspection) {
        for (VariableElement field : inspection.fields) {
            if (field.getModifiers().contains(Modifier.STATIC)) {
                if (!constantField(field)) return false;
                continue;
            }
            if (!privateFinalCarrier(field)) return false;
            VariableTree tree = (VariableTree) inspection.treesByElement.get(field);
            if (tree.getInitializer() != null && !constantExpression(inspection, tree.getInitializer(),
                    field.asType(), inspection.ownerPath)) return false;
        }
        return true;
    }

    static boolean allInitialized(JavaDepthCarrier.Inspection inspection) {
        for (VariableElement field : inspection.fields) {
            if (!field.getModifiers().contains(Modifier.STATIC) && !hasInitializer(inspection, field)) return false;
        }
        return true;
    }

    static boolean hasInitializer(JavaDepthCarrier.Inspection inspection, VariableElement field) {
        VariableTree tree = (VariableTree) inspection.treesByElement.get(field);
        return tree != null && tree.getInitializer() != null;
    }

    static boolean constantExpression(JavaDepthCarrier.Inspection inspection, ExpressionTree expression,
                                      TypeMirror expected, TreePath root) {
        if (expression instanceof ParenthesizedTree nested) {
            return constantExpression(inspection, nested.getExpression(), expected, root);
        }
        if (expression instanceof LiteralTree literal) {
            TypeMirror actual = inspection.type(expression, root);
            return (literal.getValue() != null && sameType(expected, actual))
                    || (literal.getValue() == null && actual != null
                    && actual.getKind() == javax.lang.model.type.TypeKind.NULL && isString(expected));
        }
        if (expression instanceof UnaryTree unary && (unary.getKind() == Tree.Kind.UNARY_PLUS
                || unary.getKind() == Tree.Kind.UNARY_MINUS)) {
            return constantExpression(inspection, unary.getExpression(), expected, root);
        }
        Element element = inspection.trees.getElement(TreePath.getPath(root, expression));
        return element instanceof VariableElement field && field.getConstantValue() != null
                && safeReference(inspection, expression, root)
                && sameType(expected, inspection.type(expression, root));
    }

    static boolean sameType(TypeMirror left, TypeMirror right) {
        return left != null && right != null && left.getKind() == right.getKind()
                && left.toString().equals(right.toString());
    }

    static boolean isString(TypeMirror type) {
        return type != null && "java.lang.String".equals(type.toString());
    }

    private static boolean privateFinalCarrier(VariableElement field) {
        return field.getModifiers().contains(Modifier.PRIVATE)
                && field.getModifiers().contains(Modifier.FINAL) && carrierType(field.asType());
    }

    private static boolean constantField(VariableElement field) {
        return field.getModifiers().contains(Modifier.FINAL) && field.getConstantValue() != null;
    }

    private static boolean carrierType(TypeMirror type) {
        return type.getKind().isPrimitive() || isString(type);
    }

    private static boolean safeReference(JavaDepthCarrier.Inspection inspection, ExpressionTree expression,
                                         TreePath root) {
        if (expression instanceof IdentifierTree) return true;
        if (!(expression instanceof MemberSelectTree member)) return false;
        return inspection.trees.getElement(TreePath.getPath(root, member.getExpression())) instanceof TypeElement;
    }
}
