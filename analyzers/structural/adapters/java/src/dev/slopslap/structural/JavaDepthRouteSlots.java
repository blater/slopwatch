package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;

/** Parameter dependency and literal-shape checks for normalized routes. */
final class JavaDepthRouteSlots {
    private JavaDepthRouteSlots() { }
    static Integer parameterIndex(Trees trees, TreePath path, ExecutableElement method, ExpressionTree expression) {
        if (!(expression instanceof IdentifierTree identifier)) return null;
        Element element = trees.getElement(TreePath.getPath(path, identifier));
        for (int index = 0; index < method.getParameters().size(); index++) if (method.getParameters().get(index).equals(element)) return index;
        return null;
    }
    static boolean markOnce(boolean[] used, int index) {
        if (index < 0 || index >= used.length || used[index]) return false;
        used[index] = true; return true;
    }
    static boolean typedLiteral(Trees trees, TreePath path, ExpressionTree expression,
                                javax.lang.model.type.TypeMirror expected) {
        if (!(expression instanceof LiteralTree)) return false;
        javax.lang.model.type.TypeMirror actual = trees.getTypeMirror(TreePath.getPath(path, expression));
        return actual != null && expected != null && actual.toString().contentEquals(expected.toString());
    }
}
