package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.TypeElement;
import java.util.List;

/** Groups forwarding entry routes by their resolved same-owner service target. */
final class JavaDepthRouteNormalization {
    private JavaDepthRouteNormalization() { }

    static void apply(Trees trees, TreePath ownerPath, TypeElement owner, List<Object> families) {
        JavaDepthRouteFamilyNormalizer.apply(trees, ownerPath, owner, families);
    }
}
