package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.VariableElement;

/** Small source reductions; unknown values are never assumed constant. */
final class JavaDepthMinimumExpressions {
    static Object constant(ExpressionTree expression, TreePath parent, Trees trees) {
        if (expression == null) return null;
        if (expression instanceof ParenthesizedTree p) return constant(p.getExpression(), parent, trees);
        if (expression instanceof LiteralTree literal) return literal.getValue();
        TreePath path = TreePath.getPath(parent, expression);
        if (path != null && trees.getElement(path) instanceof VariableElement variable
                && variable.getConstantValue() != null) return variable.getConstantValue();
        if (expression instanceof UnaryTree unary) {
            Object value = constant(unary.getExpression(), parent, trees);
            if (unary.getKind() == Tree.Kind.LOGICAL_COMPLEMENT && value instanceof Boolean b) return !b;
            if (unary.getKind() == Tree.Kind.UNARY_MINUS && value instanceof Number n) return -n.doubleValue();
        }
        if (expression instanceof BinaryTree binary) {
            Object left = constant(binary.getLeftOperand(), parent, trees);
            Object right = constant(binary.getRightOperand(), parent, trees);
            if (sameIntegralVariable(binary, parent, trees)) {
                switch (binary.getKind()) {
                    case EQUAL_TO, LESS_THAN_EQUAL, GREATER_THAN_EQUAL: return true;
                    case NOT_EQUAL_TO, LESS_THAN, GREATER_THAN: return false;
                    case MINUS, XOR: return 0;
                    default: break;
                }
            }
            TreePath binaryPath = TreePath.getPath(parent, binary);
            boolean integral = binaryPath != null && switch (trees.getTypeMirror(binaryPath).getKind()) {
                case BYTE, SHORT, INT, LONG, CHAR -> true;
                default -> false;
            };
            if (integral && binary.getKind() == Tree.Kind.MULTIPLY && (zero(left) || zero(right))) return 0;
            if (binary.getKind() == Tree.Kind.CONDITIONAL_AND) {
                if (Boolean.FALSE.equals(left) || Boolean.FALSE.equals(right)) return false;
                if (left instanceof Boolean && right instanceof Boolean) return true;
            }
            if (binary.getKind() == Tree.Kind.CONDITIONAL_OR) {
                if (Boolean.TRUE.equals(left) || Boolean.TRUE.equals(right)) return true;
                if (left instanceof Boolean && right instanceof Boolean) return false;
            }
            if (left instanceof Number l && right instanceof Number r) {
                double a = l.doubleValue(), b = r.doubleValue();
                // Floating conversion is exact for small integers; avoid guessing long comparisons.
                if (Math.abs(a) > 9007199254740991d || Math.abs(b) > 9007199254740991d) return null;
                return switch (binary.getKind()) {
                    case EQUAL_TO -> a == b;
                    case NOT_EQUAL_TO -> a != b;
                    case LESS_THAN -> a < b;
                    case LESS_THAN_EQUAL -> a <= b;
                    case GREATER_THAN -> a > b;
                    case GREATER_THAN_EQUAL -> a >= b;
                    default -> null;
                };
            }
        }
        return null;
    }

    static ExpressionTree identity(BinaryTree tree, TreePath path, Trees trees) {
        Object left = constant(tree.getLeftOperand(), path, trees);
        Object right = constant(tree.getRightOperand(), path, trees);
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

    private static boolean zero(Object value) { return value instanceof Number n && n.doubleValue() == 0; }
    private static boolean one(Object value) { return value instanceof Number n && n.doubleValue() == 1; }
    private static boolean sameIntegralVariable(BinaryTree tree, TreePath path, Trees trees) {
        if (!(tree.getLeftOperand() instanceof IdentifierTree) || !(tree.getRightOperand() instanceof IdentifierTree)) return false;
        TreePath left = TreePath.getPath(path, tree.getLeftOperand());
        TreePath right = TreePath.getPath(path, tree.getRightOperand());
        if (left == null || right == null || !(trees.getElement(left) instanceof VariableElement variable)
                || !variable.equals(trees.getElement(right)) || variable.getModifiers().contains(javax.lang.model.element.Modifier.VOLATILE)) return false;
        return switch (variable.asType().getKind()) {
            case BYTE, SHORT, INT, LONG, CHAR -> true;
            default -> false;
        };
    }
    static boolean neutralUpdate(CompoundAssignmentTree tree, TreePath path, Trees trees) {
        Object right = constant(tree.getExpression(), path, trees);
        TreePath variablePath = TreePath.getPath(path, tree.getVariable());
        if (variablePath == null || !trees.getTypeMirror(variablePath).getKind().isPrimitive()) return false;
        return switch (tree.getKind()) {
            case PLUS_ASSIGNMENT, MINUS_ASSIGNMENT, LEFT_SHIFT_ASSIGNMENT, RIGHT_SHIFT_ASSIGNMENT,
                    UNSIGNED_RIGHT_SHIFT_ASSIGNMENT, OR_ASSIGNMENT, XOR_ASSIGNMENT -> zero(right);
            case MULTIPLY_ASSIGNMENT, DIVIDE_ASSIGNMENT -> one(right);
            default -> false;
        };
    }
    private JavaDepthMinimumExpressions() { }
}
