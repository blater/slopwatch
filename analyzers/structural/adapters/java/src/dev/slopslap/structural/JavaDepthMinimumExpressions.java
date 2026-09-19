package dev.slopslap.structural;

import com.sun.source.tree.BinaryTree;
import com.sun.source.tree.CompoundAssignmentTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;

/** Small source reductions; unknown values are never assumed constant. */
final class JavaDepthMinimumExpressions {
    static Object constant(ExpressionTree expression, TreePath parent, Trees trees) {
        return JavaDepthMinimumConstantValues.constant(expression, parent, trees);
    }
    static ExpressionTree identity(BinaryTree tree, TreePath path, Trees trees) {
        return JavaDepthMinimumIdentities.identity(tree, path, trees);
    }
    static boolean neutralUpdate(CompoundAssignmentTree tree, TreePath path, Trees trees) {
        return JavaDepthMinimumIdentities.neutralUpdate(tree, path, trees);
    }
    private JavaDepthMinimumExpressions() { }
}
