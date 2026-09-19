package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.TypeElement;
import java.util.*;

/**
 * A deliberately bounded source-only fallback for ordinary Java classes.
 *
 * This is an observed-pattern summary.  It does not replace the resolved flow
 * assessor: callers use it only when the precise source inventory is usable
 * but the precise scalar proof cannot be admitted.
 */
final class JavaDepthMinimum {
    static final String RULE = "bounded-static-v1";

    static Map<String, Object> assess(Trees trees, TreePath ownerPath, TypeElement owner,
                                      Map<String, Object> precise, Map<String, Object> identity,
                                      String file) {
        if (trees == null || ownerPath == null || owner == null
                || !(ownerPath.getLeaf() instanceof ClassTree declaration)) return null;
        JavaDepthMinimumSurface surface = new JavaDepthMinimumSurface(trees, ownerPath, owner, declaration);
        if (!surface.hasImplementedBody() && (owner.getKind().isInterface()
                || routeFamilies(precise).isEmpty())) return notApplicable(precise, identity, file, owner);
        JavaDepthMinimumBehavior behavior = new JavaDepthMinimumBehavior(trees, ownerPath, owner, surface.methods());
        JavaDepthMinimumBehavior.Result result = behavior.analyze();
        return assessment(precise, identity, file, owner, result);
    }

    private static Map<String, Object> assessment(Map<String, Object> precise,
                                                   Map<String, Object> identity, String file,
                                                   TypeElement owner,
                                                   JavaDepthMinimumBehavior.Result behavior) {
        List<Map<String, Object>> obligations = behavior.obligations(owner, file);
        List<Object> alternatives = new ArrayList<>();
        List<String> alternativeIDs = new ArrayList<>();
        for (Map<String, Object> family : routeFamilies(precise)) {
            Object familyID = family.get("id");
            if (familyID instanceof String id) alternativeIDs.add(id);
            List<Object> familyAlternatives = new ArrayList<>();
            for (Map<String, Object> route : routes(family)) {
                Object routeID = route.get("id");
                List<List<String>> alternativesForRoute = routeID instanceof String id
                        ? behavior.obligationAlternatives(owner, id) : List.of(List.of());
                familyAlternatives.addAll(alternativesForRoute);
            }
            alternatives.add(familyAlternatives);
        }
        Map<String, Object> knowledge = knowledge(behavior.exhausted());
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("identity", precise.getOrDefault("identity", identity));
        result.put("state", behavior.exhausted() ? "partial" : "measured");
        result.put("knowledge", knowledge);
        result.put("burden", precise.getOrDefault("burden", Map.of("O", 0, "T", 0)));
        result.put("concepts", precise.getOrDefault("concepts", List.of()));
        result.put("slots", precise.getOrDefault("slots", List.of()));
        result.put("route_families", precise.getOrDefault("route_families", List.of()));
        result.put("family_alternatives", alternatives);
        result.put("family_alternative_ids", alternativeIDs);
        result.put("obligations", obligations);
        List<Map<String, Object>> evidence = new ArrayList<>();
        evidence.add(evidence(identity, file, owner, behavior));
        if (!behavior.sourceDelegations().isEmpty()) {
            evidence.add(Map.of("id", owner.getQualifiedName() + "/source-delegation",
                    "kind", "source-delegation", "status", "resolved",
                    "provenance", List.of()));
        }
        result.put("evidence", evidence);
        result.put("dependencies", behavior.sourceDelegations());
        result.put("reasons", behavior.reasons());
        result.put("files", List.of(file));
        result.put("source_locations", List.of(Map.of("path", file)));
        return result;
    }

    @SuppressWarnings("unchecked")
    private static List<Map<String, Object>> routeFamilies(Map<String, Object> precise) {
        Object value = precise.get("route_families");
        if (!(value instanceof List<?> families)) return List.of();
        List<Map<String, Object>> result = new ArrayList<>();
        for (Object family : families) if (family instanceof Map<?, ?> map) result.add(stringMap(map));
        return result;
    }

    private static List<Map<String, Object>> routes(Map<String, Object> family) {
        Object value = family.get("routes");
        if (!(value instanceof List<?> routes)) return List.of();
        List<Map<String, Object>> result = new ArrayList<>();
        for (Object route : routes) if (route instanceof Map<?, ?> map) result.add(stringMap(map));
        return result;
    }

    private static Map<String, Object> stringMap(Map<?, ?> source) {
        Map<String, Object> result = new LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : source.entrySet()) {
            if (entry.getKey() instanceof String key) result.put(key, entry.getValue());
        }
        return result;
    }

    private static Map<String, Object> knowledge(boolean exhausted) {
        String state = exhausted ? "partial" : "measured";
        Map<String, Object> dimension = Map.of("state", state, "essential", true);
        return Map.of("inventory", dimension, "burden", dimension,
                "behavior", dimension, "alias_effects", dimension);
    }

    private static Map<String, Object> evidence(Map<String, Object> identity, String file,
                                                TypeElement owner, JavaDepthMinimumBehavior.Result behavior) {
        return Map.of("id", RULE + ":" + owner.getQualifiedName(), "kind", RULE,
                "status", behavior.exhausted() ? "partial" : "measured",
                "provenance", List.of(Map.of("artifact", identity.getOrDefault("artifact", "java"),
                        "path", file, "rule_id", RULE, "fact_ids", List.of())));
    }

    private static Map<String, Object> notApplicable(Map<String, Object> precise,
                                                     Map<String, Object> identity, String file,
                                                     TypeElement owner) {
        Map<String, Object> dimension = Map.of("state", "not_applicable", "essential", false);
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("identity", precise.getOrDefault("identity", identity));
        result.put("state", "not_applicable");
        result.put("knowledge", Map.of("inventory", dimension, "burden", dimension,
                "behavior", dimension, "alias_effects", dimension));
        result.put("burden", precise.getOrDefault("burden", Map.of("O", 0, "T", 0)));
        result.put("concepts", precise.getOrDefault("concepts", List.of()));
        result.put("slots", precise.getOrDefault("slots", List.of()));
        result.put("route_families", precise.getOrDefault("route_families", List.of()));
        result.put("family_alternatives", List.of());
        result.put("family_alternative_ids", precise.getOrDefault("family_alternative_ids", List.of()));
        result.put("obligations", List.of());
        result.put("evidence", List.of(Map.of("id", RULE + ":" + owner.getQualifiedName(),
                "kind", RULE, "status", "not_applicable",
                "provenance", List.of(Map.of("path", file, "rule_id", RULE)))));
        result.put("reasons", List.of());
        result.put("files", List.of(file));
        result.put("source_locations", List.of(Map.of("path", file)));
        return result;
    }

    private JavaDepthMinimum() { }
}
