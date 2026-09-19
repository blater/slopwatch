package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;

/** Bounded side-effect-free predicate recognizer for forwarding guards. */
final class JavaDepthRoutePurity {
    private JavaDepthRoutePurity() { }
    static boolean predicate(Trees trees, TreePath methodPath, ExecutableElement method, ExpressionTree condition) {
        javax.lang.model.type.TypeMirror type = trees.getTypeMirror(TreePath.getPath(methodPath, condition));
        if (type == null || type.getKind() != javax.lang.model.type.TypeKind.BOOLEAN) return false;
        final boolean[] pure = {true};
        new TreeScanner<Void, Void>() {
            int remaining = 256;
            @Override public Void scan(Tree node, Void ignored) {
                if (node == null || --remaining < 0) { pure[0] &= node == null; return null; }
                switch (node.getKind()) {
                    case IDENTIFIER, PARENTHESIZED, BOOLEAN_LITERAL, CHAR_LITERAL, INT_LITERAL, LONG_LITERAL,
                         FLOAT_LITERAL, DOUBLE_LITERAL, STRING_LITERAL, NULL_LITERAL, UNARY_PLUS, UNARY_MINUS,
                         LOGICAL_COMPLEMENT, BITWISE_COMPLEMENT, PLUS, MINUS, MULTIPLY, DIVIDE, REMAINDER,
                         LESS_THAN, LESS_THAN_EQUAL, GREATER_THAN, GREATER_THAN_EQUAL, EQUAL_TO, NOT_EQUAL_TO,
                         AND, OR, XOR, CONDITIONAL_AND, CONDITIONAL_OR, LEFT_SHIFT, RIGHT_SHIFT, UNSIGNED_RIGHT_SHIFT -> { }
                    default -> { pure[0] = false; return null; }
                }
                return super.scan(node, ignored);
            }
            @Override public Void visitIdentifier(IdentifierTree node, Void ignored) {
                Element element = trees.getElement(TreePath.getPath(methodPath, node));
                boolean parameter = method.getParameters().stream().anyMatch(candidate -> candidate.equals(element));
                if (!parameter && !node.getName().contentEquals("true") && !node.getName().contentEquals("false")) pure[0] = false;
                return super.visitIdentifier(node, ignored);
            }
        }.scan(condition, null);
        return pure[0];
    }
}
