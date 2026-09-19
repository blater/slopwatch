package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import java.util.*;

/** Resolves the declared callable surface used by the bounded source policy. */
final class JavaDepthMinimumSurface {
    record Callable(ExecutableElement element, MethodTree tree, TreePath path) { }
    record Family(String id, List<Callable> callables) { }

    private final Trees trees;
    private final TreePath ownerPath;
    private final TypeElement owner;
    private final List<Callable> methods = new ArrayList<>();
    private final List<Family> families = new ArrayList<>();
    private final Set<String> concepts = new TreeSet<>();
    private final List<Map<String, Object>> slots = new ArrayList<>();

    JavaDepthMinimumSurface(Trees trees, TreePath ownerPath, TypeElement owner, ClassTree declaration) {
        this.trees = trees;
        this.ownerPath = ownerPath;
        this.owner = owner;
        collect(declaration);
    }

    List<Callable> methods() { return List.copyOf(methods); }
    List<Family> families() { return List.copyOf(families); }
    Set<String> concepts() { return Set.copyOf(concepts); }
    List<Map<String, Object>> slots() { return List.copyOf(slots); }

    boolean hasImplementedBody() {
        for (Callable callable : methods) {
            if (visible(callable.element()) && callable.tree().getBody() != null) return true;
        }
        return false;
    }

    List<String> familyIDs() {
        List<String> result = new ArrayList<>();
        for (Family family : families) result.add(family.id());
        return result;
    }

    List<Map<String, Object>> conceptFacts() {
        List<Map<String, Object>> result = new ArrayList<>();
        for (String concept : concepts) result.add(Map.of("id", concept, "kind", concept));
        return result;
    }

    List<Map<String, Object>> familiesAsMaps(Map<String, Object> identity) {
        List<Map<String, Object>> result = new ArrayList<>();
        for (Family family : families) {
            List<Map<String, Object>> routes = new ArrayList<>();
            for (Callable callable : family.callables()) routes.add(route(family.id(), callable, identity));
            result.add(Map.of("id", family.id(), "routes", routes));
        }
        return result;
    }

    private void collect(ClassTree declaration) {
        List<Callable> constructors = new ArrayList<>();
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof MethodTree tree)) continue;
            TreePath path = new TreePath(ownerPath, tree);
            Element element = trees.getElement(path);
            if (!(element instanceof ExecutableElement executable)) continue;
            if (!visible(executable)) continue;
            Callable callable = new Callable(executable, tree, path);
            methods.add(callable);
            String id = executable.getKind() == ElementKind.CONSTRUCTOR
                    ? JavaDepthRoles.constructorID(executable) : JavaDepthRoles.methodID(executable);
            addSlots(executable, id);
            if (executable.getKind() == ElementKind.CONSTRUCTOR) constructors.add(callable);
        }
        if (!constructors.isEmpty()) families.add(new Family("create:" + owner.getQualifiedName(), constructors));
        for (Callable callable : methods) {
            if (callable.element().getKind() != ElementKind.METHOD || !visible(callable.element())) continue;
            families.add(new Family(JavaDepthRoles.methodID(callable.element()), List.of(callable)));
        }
    }

    private void addSlots(ExecutableElement executable, String id) {
        for (int index = 0; index < executable.getParameters().size(); index++) {
            String concept = concept(executable.getParameters().get(index).asType());
            if (concept.equals("void")) continue;
            concepts.add(concept);
            slots.add(Map.of("id", id + "/arg" + index, "concept", concept, "required", true));
        }
        String returned = concept(executable.getReturnType());
        if (!returned.equals("void")) concepts.add(returned);
    }

    private Map<String, Object> route(String family, Callable callable, Map<String, Object> identity) {
        ExecutableElement executable = callable.element();
        String id = executable.getKind() == ElementKind.CONSTRUCTOR
                ? JavaDepthRoles.constructorID(executable) : JavaDepthRoles.methodID(executable);
        List<String> required = new ArrayList<>();
        for (int index = 0; index < executable.getParameters().size(); index++) {
            if (!concept(executable.getParameters().get(index).asType()).equals("void")) {
                required.add(id + "/arg" + index);
            }
        }
        return Map.of("id", id, "family", family, "signature", executable.asType().toString(),
                "target_function_id", id, "boundary", identity, "required_slots", required,
                "exposed_slots", required);
    }

    private boolean visible(ExecutableElement executable) {
        return owner.getModifiers().contains(Modifier.PUBLIC)
                ? executable.getModifiers().contains(Modifier.PUBLIC)
                : !executable.getModifiers().contains(Modifier.PRIVATE);
    }

    static String concept(TypeMirror type) {
        if (type == null) return "unknown";
        return switch (type.getKind()) {
            case VOID -> "void";
            case BOOLEAN -> "boolean";
            case BYTE, SHORT, INT, LONG, CHAR, FLOAT, DOUBLE -> "number";
            case ARRAY -> "array";
            case DECLARED -> declaredConcept((DeclaredType) type, new HashSet<>());
            default -> "reference:" + type;
        };
    }

    private static String declaredConcept(DeclaredType type, Set<TypeElement> seen) {
        if (!(type.asElement() instanceof TypeElement element)) return "unknown";
        String name = element.getQualifiedName().toString();
        if (name.equals("java.lang.String")) return "text";
        if (name.equals("java.util.Collection") || name.equals("java.lang.Iterable")
                || name.equals("java.util.List") || name.equals("java.util.Set")
                || name.equals("java.util.Queue") || name.equals("java.util.Deque")) return "collection";
        if (!seen.add(element)) return "reference:" + name;
        for (TypeMirror parent : element.getInterfaces()) {
            if (parent instanceof DeclaredType declared && declaredConcept(declared, seen).equals("collection")) return "collection";
        }
        TypeMirror superclass = element.getSuperclass();
        if (superclass instanceof DeclaredType declared && declaredConcept(declared, seen).equals("collection")) return "collection";
        return "reference:" + name;
    }
}
