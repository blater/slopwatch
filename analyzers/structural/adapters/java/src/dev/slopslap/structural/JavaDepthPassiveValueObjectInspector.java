package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Record shape and source-member checks for passive value objects. */
final class JavaDepthPassiveValueObjectInspector {
    private JavaDepthPassiveValueObjectInspector() { }

    static JavaDepthPassiveValueObject.Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (owner.getKind() != ElementKind.RECORD || !(ownerPath.getLeaf() instanceof ClassTree declaration)
                || !interfacesWithoutBehavior(owner) || declaration.getExtendsClause() != null
                || declaration.getMembers().size() > 256) return null;
        List<RecordComponentElement> components = components(owner);
        Map<String, RecordComponentElement> byName = new HashMap<>();
        for (RecordComponentElement component : components) byName.put(component.getSimpleName().toString(), component);
        if (!sourceMembersArePlain(trees, ownerPath, declaration, byName)) return null;
        List<ExecutableElement> constructors = new ArrayList<>();
        List<ExecutableElement> accessors = new ArrayList<>();
        for (Element element : owner.getEnclosedElements()) {
            if (!(element instanceof ExecutableElement executable)) continue;
            if (executable.getKind() == ElementKind.CONSTRUCTOR) {
                if (!JavaDepthPassiveValueObjectConstructors.canonicalConstructor(
                        trees, ownerPath, executable, components, byName)) return null;
                constructors.add(executable);
            } else if (JavaDepthPassiveValueObjectConstructors.isAccessor(
                    trees, ownerPath, executable, byName)) {
                accessors.add(executable);
            } else if (trees.getPath(executable) != null) return null;
        }
        if (accessors.size() != components.size()) return null;
        return new JavaDepthPassiveValueObject.Proof(constructors, accessors, components.size());
    }

    static boolean interfacesWithoutBehavior(TypeElement owner) {
        ArrayDeque<javax.lang.model.type.TypeMirror> pending = new ArrayDeque<>(owner.getInterfaces());
        Set<Element> seen = new HashSet<>();
        while (!pending.isEmpty()) {
            javax.lang.model.type.TypeMirror type = pending.removeFirst();
            if (type.getKind() != TypeKind.DECLARED || !(type instanceof DeclaredType declared)
                    || !(declared.asElement() instanceof TypeElement contract)
                    || contract.getKind() != ElementKind.INTERFACE) return false;
            if (!seen.add(contract)) continue;
            if (seen.size() > 32) return false;
            for (Element member : contract.getEnclosedElements()) {
                if (member.getKind() == ElementKind.METHOD && !member.getModifiers().contains(Modifier.STATIC)
                        && !member.getModifiers().contains(Modifier.ABSTRACT)) return false;
            }
            pending.addAll(contract.getInterfaces());
        }
        return true;
    }

    private static List<RecordComponentElement> components(TypeElement owner) {
        List<RecordComponentElement> result = new ArrayList<>();
        for (Element element : owner.getEnclosedElements()) {
            if (element.getKind() == ElementKind.RECORD_COMPONENT && element instanceof RecordComponentElement component) {
                result.add(component);
            }
        }
        return result;
    }

    private static boolean sourceMembersArePlain(Trees trees, TreePath ownerPath, ClassTree declaration,
                                                 Map<String, RecordComponentElement> components) {
        for (Tree member : declaration.getMembers()) {
            if (member instanceof MethodTree method) {
                Element element = trees.getElement(new TreePath(ownerPath, method));
                if (!(element instanceof ExecutableElement executable)) return false;
                if (executable.getKind() != ElementKind.CONSTRUCTOR
                        && !JavaDepthPassiveValueObjectConstructors.isAccessor(trees, ownerPath, executable, components)) {
                    return false;
                }
                continue;
            }
            if (member instanceof VariableTree variable) {
                Element element = trees.getElement(new TreePath(ownerPath, variable));
                if (!(element instanceof VariableElement field)
                        || !components.containsKey(field.getSimpleName().toString())) return false;
                continue;
            }
            return false;
        }
        return true;
    }
}
