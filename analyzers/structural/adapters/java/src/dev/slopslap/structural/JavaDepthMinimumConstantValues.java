package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.VariableElement;

/** Conservative literal evaluation for Java minimum-behavior guards. */
final class JavaDepthMinimumConstantValues {
    private JavaDepthMinimumConstantValues() { }
    static Object constant(ExpressionTree expression, TreePath parent, Trees trees) {
        if (expression == null) return null;
        if (expression instanceof ParenthesizedTree p) return constant(p.getExpression(), parent, trees);
        if (expression instanceof LiteralTree literal) return literal.getValue();
        TreePath path = TreePath.getPath(parent, expression);
        if (path != null && trees.getElement(path) instanceof VariableElement variable && variable.getConstantValue() != null) {
            return variable.getConstantValue();
        }
        if (expression instanceof UnaryTree unary) return unary(unary, parent, trees);
        if (expression instanceof BinaryTree binary) return binary(binary, parent, trees);
        return null;
    }
    private static Object unary(UnaryTree unary, TreePath parent, Trees trees) {
        Object value = constant(unary.getExpression(), parent, trees);
        if (unary.getKind() == Tree.Kind.LOGICAL_COMPLEMENT && value instanceof Boolean b) return !b;
        if (unary.getKind() == Tree.Kind.UNARY_MINUS && value instanceof Number n) return -n.doubleValue();
        return null;
    }
    private static Object binary(BinaryTree binary, TreePath parent, Trees trees) {
        Object left = constant(binary.getLeftOperand(), parent, trees), right = constant(binary.getRightOperand(), parent, trees);
        if (JavaDepthMinimumConstantGuards.sameIntegralVariable(binary, parent, trees)) {
            switch (binary.getKind()) {
                case EQUAL_TO, LESS_THAN_EQUAL, GREATER_THAN_EQUAL -> { return true; }
                case NOT_EQUAL_TO, LESS_THAN, GREATER_THAN -> { return false; }
                case MINUS, XOR -> { return 0; }
                default -> { }
            }
        }
        TreePath binaryPath = TreePath.getPath(parent, binary);
        boolean integral = binaryPath != null && trees.getTypeMirror(binaryPath) != null
                && trees.getTypeMirror(binaryPath).getKind().isPrimitive()
                && switch (trees.getTypeMirror(binaryPath).getKind()) {
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
        if (!(left instanceof Number l) || !(right instanceof Number r)) return null;
        double a = l.doubleValue(), b = r.doubleValue();
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
    private static boolean zero(Object value) { return value instanceof Number n && n.doubleValue() == 0; }
}
