package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Declaration, field and method orchestration for passive result carriers. */
final class JavaDepthPassiveResultCarrierInspector {
    private JavaDepthPassiveResultCarrierInspector() { }

    static Map<String, Object> inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (!(ownerPath.getLeaf() instanceof ClassTree declaration) || !eligibleType(owner)) return null;
        List<VariableElement> fields = fields(trees, ownerPath, declaration);
        if (fields.isEmpty() || fields.size() > 256) return null;
        Map<Element, VariableElement> fieldSet = new HashMap<>();
        for (VariableElement field : fields) fieldSet.put(field, field);
        JavaDepthPassiveResultCarrierMembers.Counts counts = new JavaDepthPassiveResultCarrierMembers.Counts();
        Set<VariableElement> getters = new HashSet<>();
        for (VariableElement field : fields) if (!field.getModifiers().contains(Modifier.PRIVATE)) getters.add(field);
        int exposedFields = getters.size();
        for (Tree member : declaration.getMembers()) {
            if (member instanceof VariableTree variable) {
                if (!JavaDepthPassiveResultCarrierMembers.pureInitializer(trees, ownerPath, variable, fieldSet)) return null;
                continue;
            }
            if (!(member instanceof MethodTree methodTree)) return null;
            Element element = trees.getElement(new TreePath(ownerPath, methodTree));
            if (!(element instanceof ExecutableElement method) || !eligibleMethod(method)) return null;
            TreePath methodPath = new TreePath(ownerPath, methodTree);
            if (method.getKind() == ElementKind.CONSTRUCTOR) {
                if (!JavaDepthPassiveResultCarrierMembers.directAssignments(
                        trees, methodPath, methodTree.getBody(), method, fieldSet)) return null;
                counts.constructors++;
            } else if (JavaDepthPassiveResultCarrierMembers.getter(trees, methodPath, method, fieldSet, getters)) {
                // accessor proof is recorded in getters
            } else if (JavaDepthPassiveResultCarrierMembers.directAssignments(
                    trees, methodPath, methodTree.getBody(), method, fieldSet)) {
                counts.mutators++;
            } else return null;
        }
        if (getters.size() != fields.size()) return null;
        Map<String, Object> evidence = new HashMap<>(marker(owner, fields.size(), getters.size() - exposedFields,
                counts.mutators, counts.constructors));
        if (exposedFields > 0) evidence.put("kind", "passive-value-object-v1");
        return evidence;
    }

    private static boolean eligibleType(TypeElement owner) {
        if (owner.getKind() != ElementKind.CLASS || owner.getModifiers().contains(Modifier.ABSTRACT)
                || owner.getNestingKind().isNested() && !owner.getModifiers().contains(Modifier.STATIC)
                || !JavaDepthPassiveValueObjectInspector.interfacesWithoutBehavior(owner)) return false;
        TypeMirror superclass = owner.getSuperclass();
        return superclass.getKind() == TypeKind.NONE || superclass instanceof DeclaredType declared
                && declared.asElement() instanceof TypeElement parent
                && parent.getQualifiedName().contentEquals("java.lang.Object");
    }

    private static List<VariableElement> fields(Trees trees, TreePath ownerPath, ClassTree declaration) {
        List<VariableElement> result = new ArrayList<>();
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof VariableTree variable)) continue;
            Element element = trees.getElement(new TreePath(ownerPath, variable));
            if (!(element instanceof VariableElement field)
                    || field.getModifiers().contains(Modifier.STATIC)
                    || field.getModifiers().contains(Modifier.VOLATILE)) return List.of();
            result.add(field);
        }
        return result;
    }

    private static boolean eligibleMethod(ExecutableElement method) {
        return !method.getModifiers().contains(Modifier.STATIC)
                && !method.getModifiers().contains(Modifier.SYNCHRONIZED)
                && !method.getModifiers().contains(Modifier.ABSTRACT)
                && !method.getModifiers().contains(Modifier.NATIVE)
                && !method.isVarArgs() && method.getTypeParameters().isEmpty()
                && method.getThrownTypes().isEmpty();
    }

    private static Map<String, Object> marker(TypeElement owner, int fields, int getters,
                                               int mutators, int constructors) {
        return Map.of("id", owner.getQualifiedName() + "/passive-result-carrier",
                "kind", "passive-result-carrier-v1", "status", "proven",
                "details", Map.of("instance_fields", fields, "direct_getters", getters,
                        "direct_mutators", mutators, "constructors", constructors), "provenance", List.of());
    }
}
