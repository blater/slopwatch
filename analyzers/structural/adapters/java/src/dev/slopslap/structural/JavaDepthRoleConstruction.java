package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.util.TreePath;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;

/** Public-facing construction query backed by a separately built source index. */
final class JavaDepthRoleConstruction {
    private final JavaDepthRoleConstructionIndex index;

    JavaDepthRoleConstruction(JavaDepthRoles owner) {
        index = new JavaDepthRoleConstructionIndex(owner);
    }

    boolean uses(JavaDepthRoles.SourceType consumer, ExecutableElement target,
                 JavaDepthRoles.SourceType implementation, int parameterIndex) {
        return index.uses(consumer, target, implementation, parameterIndex);
    }

    boolean resolvesTo(TreePath parent, ExpressionTree expression, TypeElement implementation) {
        return index.resolvesTo(parent, expression, implementation);
    }
}
