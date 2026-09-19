package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;

/** Algebraic identity and neutral-update reductions. */
final class JavaDepthMinimumIdentities {
    private JavaDepthMinimumIdentities() { }
    static ExpressionTree identity(BinaryTree tree, TreePath path, Trees trees) {
        Object left = JavaDepthMinimumExpressions.constant(tree.getLeftOperand(), path, trees);
        Object right = JavaDepthMinimumExpressions.constant(tree.getRightOperand(), path, trees);
        if (tree.getKind() == Tree.Kind.PLUS) {
            TreePath expressionPath = TreePath.getPath(path, tree);
            if (expressionPath == null || !trees.getTypeMirror(expressionPath).getKind().isPrimitive()) return null;
        }
        return switch (tree.getKind()) {
            case PLUS -> zero(right) ? tree.getLeftOperand() : zero(left) ? tree.getRightOperand() : null;
            case MINUS, LEFT_SHIFT, RIGHT_SHIFT, UNSIGNED_RIGHT_SHIFT, OR, XOR -> zero(right) ? tree.getLeftOperand() : null;
            case MULTIPLY -> one(right) ? tree.getLeftOperand() : one(left) ? tree.getRightOperand() : null;
            case DIVIDE -> one(right) ? tree.getLeftOperand() : null;
            case CONDITIONAL_AND -> Boolean.TRUE.equals(right) ? tree.getLeftOperand() : Boolean.TRUE.equals(left) ? tree.getRightOperand() : null;
            case CONDITIONAL_OR -> Boolean.FALSE.equals(right) ? tree.getLeftOperand() : Boolean.FALSE.equals(left) ? tree.getRightOperand() : null;
            default -> null;
        };
    }
    static boolean neutralUpdate(CompoundAssignmentTree tree, TreePath path, Trees trees) {
        Object right = JavaDepthMinimumExpressions.constant(tree.getExpression(), path, trees);
        TreePath variablePath = TreePath.getPath(path, tree.getVariable());
        if (variablePath == null || !trees.getTypeMirror(variablePath).getKind().isPrimitive()) return false;
        return switch (tree.getKind()) {
            case PLUS_ASSIGNMENT, MINUS_ASSIGNMENT, LEFT_SHIFT_ASSIGNMENT, RIGHT_SHIFT_ASSIGNMENT,
                 UNSIGNED_RIGHT_SHIFT_ASSIGNMENT, OR_ASSIGNMENT, XOR_ASSIGNMENT -> zero(right);
            case MULTIPLY_ASSIGNMENT, DIVIDE_ASSIGNMENT -> one(right);
            default -> false;
        };
    }
    private static boolean zero(Object value) { return value instanceof Number n && n.doubleValue() == 0; }
    private static boolean one(Object value) { return value instanceof Number n && n.doubleValue() == 1; }
}
