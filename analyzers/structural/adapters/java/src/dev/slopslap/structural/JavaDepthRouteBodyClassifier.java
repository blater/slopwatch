package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;

/** Route-body façade retaining the historical normalization entry point. */
final class JavaDepthRouteBodyClassifier {
    private JavaDepthRouteBodyClassifier() { }
    record Normalized(String serviceID, java.util.List<String> required, java.util.List<String> exposed) { }
    static Normalized normalize(Trees trees, TreePath ownerPath, TypeElement owner, ExecutableElement method) {
        return JavaDepthRouteProjection.normalize(trees, ownerPath, owner, method);
    }
}
