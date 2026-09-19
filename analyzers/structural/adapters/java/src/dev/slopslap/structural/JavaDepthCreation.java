package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import java.util.*;

final class JavaDepthCreation {
    private JavaDepthCreation() {}

    static Map<String, Object> fact(TypeElement owner, List<Map<String, Object>> creationRoutes,
                                    List<String> passiveAccessors, boolean validatedCreation,
                                    boolean creationIncomplete) {
        List<Object> bindings = new ArrayList<>();
        List<Object> initialFields = new ArrayList<>();
        for (Map<String, Object> route : creationRoutes) {
            bindings.addAll((List<?>) route.get("input_bindings"));
            initialFields.addAll((List<?>) route.get("initial_fields"));
        }
        return Map.ofEntries(Map.entry("id", "create:" + owner.getQualifiedName()),
                Map.entry("canonical_type", owner.getQualifiedName().toString()),
                Map.entry("family", "create:" + owner.getQualifiedName()),
                Map.entry("route", "create:" + owner.getQualifiedName()), Map.entry("input_bindings", bindings),
                Map.entry("routes", creationRoutes), Map.entry("initial_fields", initialFields),
                Map.entry("passive_accessors", passiveAccessors),
                Map.entry("possible_failures", validatedCreation ? List.of("source_rejection") : List.of()),
                Map.entry("behavior", validatedCreation ? List.of("normal", "rejection") : List.of("normal")),
                Map.entry("data_only", !validatedCreation && !creationIncomplete), Map.entry("accessible", true),
                Map.entry("knowledge", creationIncomplete ? "partial" : "measured"));
    }

    static boolean trivialConstructor(Trees trees, TreePath ownerPath, MethodTree tree, ExecutableElement method) {
        if (method.getModifiers().contains(Modifier.PRIVATE)) return true;
        BlockTree body = tree.getBody();
        if (body == null) return false;
        if (body.getStatements().isEmpty()) return true;
        if (body.getStatements().size() != 1 || !(body.getStatements().get(0) instanceof ExpressionStatementTree statement)) return false;
        if (!(statement.getExpression() instanceof MethodInvocationTree call)) return false;
        TreePath memberPath = new TreePath(ownerPath, tree);
        Element target = trees.getElement(TreePath.getPath(memberPath, call));
        if (!(target instanceof ExecutableElement constructor) || constructor.getKind() != ElementKind.CONSTRUCTOR) return false;
        TypeElement enclosing = (TypeElement) constructor.getEnclosingElement();
        return enclosing.getQualifiedName().contentEquals("java.lang.Object") && call.getArguments().isEmpty();
    }

    static void addImplicit(TypeElement owner, Map<String, Object> identity,
                            List<Object> families, List<Object> functions,
                            List<Map<String, Object>> creationRoutes) {
        String id = owner.getQualifiedName() + "#<init>()";
        addCreationRoute(owner, identity, families, functions, id, "()", List.of());
        creationRoutes.add(fact(owner, id, "()", List.of()));
    }

    static void addExplicit(TypeElement owner, Map<String, Object> identity,
                            ExecutableElement constructor, List<Object> families,
                            List<Object> functions, List<Object> slots, Set<String> concepts,
                            List<Map<String, Object>> creationRoutes) {
        String id = JavaDepthRoles.constructorID(constructor);
        List<String> required = new ArrayList<>();
        List<Map<String, Object>> bindings = new ArrayList<>();
        List<Map<String, Object>> formals = new ArrayList<>();
        for (int index = 0; index < constructor.getParameters().size(); index++) {
            String slot = id + "/arg" + index;
            String concept = JavaDepthTypes.concept(constructor.getParameters().get(index).asType());
            concepts.add(concept);
            slots.add(Map.of("id", slot, "concept", concept, "required", true));
            required.add(slot);
            bindings.add(Map.of("formal", slot, "actual", slot, "value", slot));
            formals.add(JavaDepthTypes.formal("arg" + index, slot, constructor.getParameters().get(index).asType()));
        }
        addCreationRoute(owner, identity, families, functions, id, constructor.asType().toString(), formals, required);
        creationRoutes.add(fact(owner, id, constructor.asType().toString(), bindings));
    }

    // Inventory an ordinary constructor without claiming allocation-only behavior.
    static void addInventory(TypeElement owner, Map<String, Object> identity,
                             ExecutableElement constructor, List<Object> families,
                             List<Object> slots, Set<String> concepts) {
        addExplicit(owner, identity, constructor, families, new ArrayList<>(), slots, concepts, new ArrayList<>());
    }

    private static void addCreationRoute(TypeElement owner, Map<String, Object> identity,
                                         List<Object> families, List<Object> functions,
                                         String id, String signature, List<String> required) {
        addCreationRoute(owner, identity, families, functions, id, signature, List.of(), required);
    }

    private static void addCreationRoute(TypeElement owner, Map<String, Object> identity,
                                         List<Object> families, List<Object> functions,
                                         String id, String signature, List<Map<String, Object>> formals,
                                         List<String> required) {
        String family = "create:" + owner.getQualifiedName();
        Map<String, Object> route = Map.of("id", id, "family", family, "signature", signature,
                "target_function_id", id, "boundary", identity, "required_slots", required,
                "exposed_slots", required);
        List<Object> existing = routesForFamily(families, family);
        existing.add(route);
        families.removeIf(item -> family.equals(familyID(item)));
        families.add(Map.of("id", family, "routes", existing));
        functions.add(Map.of("id", id, "entry", "entry", "formals", formals, "results", List.of(),
                "blocks", List.of(Map.of("id", "entry", "instructions", List.of(
                        Map.of("id", "n1", "opcode", "return", "operands", List.of()))))));
    }

    private static String familyID(Object item) {
        if (!(item instanceof Map<?, ?> map)) return "";
        Object id = map.get("id");
        return id instanceof String value ? value : "";
    }

    private static List<Object> routesForFamily(List<Object> families, String family) {
        for (Object item : families) {
            if (!family.equals(familyID(item)) || !(item instanceof Map<?, ?> map)) continue;
            Object routes = map.get("routes");
            if (!(routes instanceof List<?> values)) continue;
            List<Object> copy = new ArrayList<>();
            copy.addAll(values);
            return copy;
        }
        return new ArrayList<>();
    }

    private static Map<String, Object> fact(TypeElement owner, String id, String signature,
                                            List<Map<String, Object>> bindings) {
        return Map.ofEntries(Map.entry("id", id), Map.entry("canonical_type", owner.getQualifiedName().toString()),
                Map.entry("family", "create:" + owner.getQualifiedName()), Map.entry("route", id),
                Map.entry("signature", signature),
                Map.entry("input_bindings", bindings), Map.entry("initial_fields", List.of()),
                Map.entry("possible_failures", List.of()), Map.entry("behavior", List.of("normal")),
                Map.entry("data_only", true), Map.entry("accessible", true), Map.entry("knowledge", "measured"));
    }
}
