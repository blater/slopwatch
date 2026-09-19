package dev.slopslap.structural;

import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.RecordComponentElement;
import java.util.List;
import java.util.Map;

/** Small façade for passive-record constructor and accessor proofs. */
final class JavaDepthPassiveValueObjectConstructors {
    private JavaDepthPassiveValueObjectConstructors() { }

    static boolean canonicalConstructor(Trees trees, TreePath ownerPath, ExecutableElement constructor,
                                        List<RecordComponentElement> components,
                                        Map<String, RecordComponentElement> byName) {
        return JavaDepthPassiveValueObjectConstructorBody.canonical(trees, constructor, components, byName);
    }

    static boolean isAccessor(Trees trees, TreePath ownerPath, ExecutableElement method,
                              Map<String, RecordComponentElement> components) {
        return JavaDepthPassiveValueObjectAccessors.isAccessor(trees, method, components);
    }
}
