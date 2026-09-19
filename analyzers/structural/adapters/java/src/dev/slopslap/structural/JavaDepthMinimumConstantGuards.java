package dev.slopslap.structural;

import com.sun.source.tree.BinaryTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.VariableElement;

/** Variable-shape guard used by conservative constant evaluation. */
final class JavaDepthMinimumConstantGuards {
    private JavaDepthMinimumConstantGuards() { }
    static boolean sameIntegralVariable(BinaryTree tree, TreePath path, Trees trees) {
        if (!(tree.getLeftOperand() instanceof IdentifierTree) || !(tree.getRightOperand() instanceof IdentifierTree)) return false;
        TreePath left = TreePath.getPath(path, tree.getLeftOperand()), right = TreePath.getPath(path, tree.getRightOperand());
        if (left == null || right == null || !(trees.getElement(left) instanceof VariableElement variable)
                || !variable.equals(trees.getElement(right)) || variable.getModifiers().contains(Modifier.VOLATILE)) return false;
        return switch (variable.asType().getKind()) {
            case BYTE, SHORT, INT, LONG, CHAR -> true;
            default -> false;
        };
    }
}
