package dev.slopslap.structural;

import java.util.List;

/** Algebraic identity and two-operand constant reductions. */
final class JavaDepthComputationAlgebra {
    JavaDepthComputationState.Node identity(String operation, List<JavaDepthComputationState.Node> nodes, String type) {
        if (nodes.size() != 2) return null;
        JavaDepthComputationState.Node left = nodes.get(0), right = nodes.get(1);
        return switch (operation) {
            case "PLUS" -> numeric(type) && zero(right) ? left : null;
            case "MINUS", "LEFT_SHIFT", "RIGHT_SHIFT", "UNSIGNED_RIGHT_SHIFT", "OR", "XOR" -> zero(right) ? left : null;
            case "MULTIPLY" -> one(right) ? left : one(left) ? right : null;
            case "DIVIDE" -> one(right) ? left : null;
            case "CONDITIONAL_AND" -> Boolean.TRUE.equals(right.constant()) ? left
                    : Boolean.TRUE.equals(left.constant()) ? right : null;
            case "CONDITIONAL_OR" -> Boolean.FALSE.equals(right.constant()) ? left
                    : Boolean.FALSE.equals(left.constant()) ? right : null;
            default -> null;
        };
    }
    private boolean numeric(String type) {
        return switch (type) {
            case "byte", "short", "int", "long", "char", "float", "double" -> true;
            default -> false;
        };
    }
    Object constant(String operation, List<JavaDepthComputationState.Node> nodes) {
        if (nodes.size() != 2 || nodes.get(0).constant() == null || nodes.get(1).constant() == null) return null;
        Object left = nodes.get(0).constant(), right = nodes.get(1).constant();
        if (left instanceof Boolean l && right instanceof Boolean r) {
            if (operation.equals("CONDITIONAL_AND")) return l && r;
            if (operation.equals("CONDITIONAL_OR")) return l || r;
            if (operation.equals("EQUAL_TO")) return l == r;
            if (operation.equals("NOT_EQUAL_TO")) return l != r;
        }
        if (left instanceof Number l && right instanceof Number r) {
            double a = l.doubleValue(), b = r.doubleValue();
            return switch (operation) {
                case "EQUAL_TO" -> a == b; case "NOT_EQUAL_TO" -> a != b;
                case "LESS_THAN" -> a < b; case "LESS_THAN_EQUAL" -> a <= b;
                case "GREATER_THAN" -> a > b; case "GREATER_THAN_EQUAL" -> a >= b;
                default -> null;
            };
        }
        return null;
    }
    private boolean zero(JavaDepthComputationState.Node node) { return node.constant() instanceof Number number && number.doubleValue() == 0; }
    private boolean one(JavaDepthComputationState.Node node) { return node.constant() instanceof Number number && number.doubleValue() == 1; }
}
