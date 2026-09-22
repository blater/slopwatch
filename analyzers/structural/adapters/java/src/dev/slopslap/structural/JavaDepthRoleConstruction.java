package dev.slopslap.structural;

import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;

/** Public-facing construction query backed by a separately built source index. */
final class JavaDepthRoleConstruction {
    private final JavaDepthRoleConstructionIndex index;

    JavaDepthRoleConstruction(JavaDepthRoles owner) {
        index = new JavaDepthRoleConstructionIndex(owner);
    }

    java.util.Set<TypeElement> implementations(JavaDepthRoles.SourceType consumer, ExecutableElement target, int parameterIndex) {
        return index.implementations(consumer, target, parameterIndex);
    }

}
