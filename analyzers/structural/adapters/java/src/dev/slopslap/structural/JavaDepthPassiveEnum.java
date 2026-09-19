package dev.slopslap.structural;

import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import java.util.List;
import java.util.Map;

/** Proof for an enum with direct metadata storage and direct field getters. */
final class JavaDepthPassiveEnum {
    private JavaDepthPassiveEnum() { }

    record Proof(List<ExecutableElement> constructors, List<ExecutableElement> accessors,
                 List<ExecutableElement> staticAccessors, List<VariableElement> constants, int fields) {
        Proof {
            constructors = List.copyOf(constructors);
            accessors = List.copyOf(accessors);
            staticAccessors = List.copyOf(staticAccessors);
            constants = List.copyOf(constants);
        }
    }

    static Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        return JavaDepthPassiveEnumInspector.inspect(trees, ownerPath, owner);
    }

    static Map<String, Object> evidence(TypeElement owner, Proof proof) {
        return Map.of("id", owner.getQualifiedName() + "/passive-enum", "kind", "passive-enum-v1", "status", "proven",
                "details", Map.of("constants", proof.constants().size(), "metadata_fields", proof.fields(),
                        "direct_getters", proof.accessors().size() + proof.staticAccessors().size(),
                        "constructors", proof.constructors().size()), "provenance", List.of());
    }
}
