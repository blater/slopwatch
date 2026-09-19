package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** Deterministic family grouping and argument-slot projection for route reports. */
final class JavaDepthRouteFamilyNormalizer {
    private JavaDepthRouteFamilyNormalizer() { }

    static void apply(Trees trees, TreePath ownerPath, TypeElement owner, List<Object> families) {
        Map<String, ExecutableElement> methods = methods(owner);
        if (methods.isEmpty() || families.isEmpty()) return;
        Map<String, List<Map<String, Object>>> grouped = new LinkedHashMap<>();
        List<Object> order = new ArrayList<>();
        Map<String, Boolean> seenGroups = new LinkedHashMap<>();
        for (Object value : families) {
            if (!(value instanceof Map<?, ?> family)) continue;
            Object rawRoutes = family.get("routes");
            if (!(rawRoutes instanceof List<?> routes)) { order.add(value); continue; }
            boolean methodFamily = false;
            for (Object rawRoute : routes) {
                if (!(rawRoute instanceof Map<?, ?> route)) continue;
                ExecutableElement method = methods.get(string(route.get("id")));
                if (method == null) continue;
                methodFamily = true;
                JavaDepthRouteBodyClassifier.Normalized normalized = JavaDepthRouteBodyClassifier.normalize(
                        trees, ownerPath, owner, method);
                Map<String, Object> copy = copy(route);
                copy.put("family", normalized.serviceID());
                copy.put("required_slots", normalized.required());
                copy.put("exposed_slots", normalized.exposed());
                if (!seenGroups.containsKey(normalized.serviceID())) {
                    seenGroups.put(normalized.serviceID(), true);
                    order.add(normalized.serviceID());
                }
                grouped.computeIfAbsent(normalized.serviceID(), ignored -> new ArrayList<>()).add(copy);
            }
            if (!methodFamily) order.add(value);
        }
        families.clear();
        for (Object item : order) {
            if (!(item instanceof String serviceID)) { families.add(item); continue; }
            List<Map<String, Object>> routes = grouped.get(serviceID);
            if (routes != null && !routes.isEmpty()) families.add(Map.of("id", serviceID, "routes", routes));
        }
    }

    private static Map<String, ExecutableElement> methods(TypeElement owner) {
        Map<String, ExecutableElement> result = new LinkedHashMap<>();
        for (Element element : owner.getEnclosedElements()) {
            if (element instanceof ExecutableElement method && method.getKind() == ElementKind.METHOD) {
                result.put(JavaDepthRoles.methodID(method), method);
            }
        }
        return result;
    }

    private static Map<String, Object> copy(Map<?, ?> source) {
        Map<String, Object> result = new LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : source.entrySet()) {
            if (entry.getKey() instanceof String key) result.put(key, entry.getValue());
        }
        return result;
    }

    private static String string(Object value) { return value instanceof String result ? result : ""; }
}
