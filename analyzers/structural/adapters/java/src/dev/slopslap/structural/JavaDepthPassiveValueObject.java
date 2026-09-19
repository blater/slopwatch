package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import java.util.List;
import java.util.Map;

/** Proof for a plain record whose generated/value accessors expose only components. */
final class JavaDepthPassiveValueObject {
    private JavaDepthPassiveValueObject() { }

    record Proof(List<ExecutableElement> constructors, List<ExecutableElement> accessors, int components) {
        Proof {
            constructors = List.copyOf(constructors);
            accessors = List.copyOf(accessors);
        }
    }

    static Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        return JavaDepthPassiveValueObjectInspector.inspect(trees, ownerPath, owner);
    }

    static Map<String, Object> evidence(TypeElement owner, Proof proof) {
        return Map.of("id", owner.getQualifiedName() + "/passive-value-object",
                "kind", "passive-value-object-v1", "status", "proven",
                "details", Map.of("components", proof.components(), "direct_getters", proof.accessors().size(),
                        "constructors", proof.constructors().size()), "provenance", List.of());
    }
}
