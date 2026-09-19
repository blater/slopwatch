package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.TypeElement;
import java.util.Map;

/** Structural proof for mutable result objects which only store and expose values. */
final class JavaDepthPassiveResultCarrier {
    private JavaDepthPassiveResultCarrier() { }

    static Map<String, Object> inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        return JavaDepthPassiveResultCarrierInspector.inspect(trees, ownerPath, owner);
    }
}
