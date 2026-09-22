package dev.slopslap.structural;

import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.type.*;
import javax.lang.model.util.Types;
import java.util.*;

/** Shared concrete signatures; substitute only queried generic families. Overrides remains authoritative. */
final class JavaDepthRoleMethodIndex {
    long signatureWork;
    private final Types types;
    private final TypeElement scope;
    private final Map<String, List<ExecutableElement>> coarse = new HashMap<>();
    private final Map<String, List<ExecutableElement>> exact = new HashMap<>();
    private final Map<String, List<ExecutableElement>> generic = new HashMap<>();
    private final Set<String> uncertain = new HashSet<>();
    private final Map<ExecutableElement, Integer> order = new IdentityHashMap<>();
    private final Map<TypeElement, Map<String, ResolvedFamily>> substitutions = new IdentityHashMap<>();
    private record ResolvedFamily(Map<String, List<ExecutableElement>> exact, boolean uncertain) { }

    JavaDepthRoleMethodIndex(Types types, TypeElement scope, List<ExecutableElement> methods) {
        this.types = types;
        this.scope = scope;
        for (ExecutableElement method : methods) {
            order.put(method, order.size());
            String family = family(method);
            coarse.computeIfAbsent(family, ignored -> new ArrayList<>()).add(method);
            ExecutableType member = member(scope, method);
            String signature = signature(method, member);
            if (signature == null) uncertain.add(family);
            else if (member.getParameterTypes().stream().anyMatch(JavaDepthRoleMethodIndex::scopeDependent)) {
                generic.computeIfAbsent(family, ignored -> new ArrayList<>()).add(method);
            } else exact.computeIfAbsent(signature, ignored -> new ArrayList<>()).add(method);
        }
    }

    List<ExecutableElement> candidates(ExecutableElement method) {
        return candidates(method, scope);
    }

    List<ExecutableElement> candidates(ExecutableElement method, TypeElement queryScope) {
        String family = family(method);
        String signature = signature(method, member(queryScope, method));
        if (signature == null || uncertain.contains(family)) return coarse.getOrDefault(family, List.of());
        List<ExecutableElement> fixed = exact.getOrDefault(signature, List.of());
        if (!generic.containsKey(family)) return fixed;
        ResolvedFamily resolved = substitutions.computeIfAbsent(queryScope, ignored -> new HashMap<>())
                .computeIfAbsent(family, ignored -> resolve(queryScope, family));
        if (resolved.uncertain()) return coarse.getOrDefault(family, List.of());
        List<ExecutableElement> varying = resolved.exact().getOrDefault(signature, List.of());
        if (fixed.isEmpty()) return varying;
        if (varying.isEmpty()) return fixed;
        List<ExecutableElement> result = new ArrayList<>(fixed.size() + varying.size());
        result.addAll(fixed);
        result.addAll(varying);
        result.sort(Comparator.comparingInt(order::get));
        return result;
    }

    private ResolvedFamily resolve(TypeElement queryScope, String family) {
        Map<String, List<ExecutableElement>> result = new HashMap<>();
        boolean unknown = false;
        for (ExecutableElement method : generic.get(family)) {
            String signature = signature(method, member(queryScope, method));
            if (signature == null) unknown = true;
            else result.computeIfAbsent(signature, ignored -> new ArrayList<>()).add(method);
        }
        return new ResolvedFamily(result, unknown);
    }

    private static boolean scopeDependent(TypeMirror parameter) {
        if (parameter.getKind() == TypeKind.ARRAY) return scopeDependent(((ArrayType) parameter).getComponentType());
        // Declared parameters erase to their declaration regardless of generic arguments.
        return parameter.getKind() == TypeKind.TYPEVAR;
    }

    private ExecutableType member(TypeElement owner, ExecutableElement method) {
        signatureWork++;
        try {
            return (ExecutableType) types.asMemberOf((DeclaredType) owner.asType(), method);
        } catch (IllegalArgumentException exception) {
            return null;
        }
    }

    private String signature(ExecutableElement method, ExecutableType member) {
        if (member == null) return null;
        StringBuilder key = new StringBuilder(family(method));
        for (TypeMirror parameter : member.getParameterTypes()) {
            if (JavaDepthRoleTypes.hasErrorType(parameter)) return null;
            key.append(';').append(types.erasure(parameter));
        }
        return key.toString();
    }

    private static String family(ExecutableElement method) {
        return method.getSimpleName() + "/" + method.getParameters().size();
    }
}
