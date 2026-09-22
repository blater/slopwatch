package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.util.Elements;
import java.util.*;

final class JavaDepthRoleContract {
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Elements elements;
    private final Map<JavaDepthRoles.SourceType, Map<VariableElement, List<InvocationSite>>> invocations = new IdentityHashMap<>();
    private final Map<TypeElement, JavaDepthRoleMethodIndex> contractMethods = new IdentityHashMap<>();

    private final Map<TypeElement, Map<ExecutableElement, ExecutableElement>> resolutions = new IdentityHashMap<>();
    private final Map<Map<Element, ExecutableElement>, Map<ExecutableElement, ExecutableElement>> inverses = new IdentityHashMap<>();

    private record UseKey(JavaDepthRoles.SourceType consumer, VariableElement field, TypeElement contract) { }
    private record OrderedSite(InvocationSite site, int ordinal) { }
    private final Map<UseKey, Map<ExecutableElement, OrderedSite>> uses = new HashMap<>();
    private final Map<Map<Element, ExecutableElement>, Map<UseKey, JavaDepthRoleBindings.Invocation>> evidence = new IdentityHashMap<>();

    JavaDepthRoleContract(JavaDepthRoles owner) {
        this.owner=owner;
        this.trees=owner.trees;
        this.elements=owner.elements;
        indexInvocations();
    }

    private record InvocationSite(VariableElement receiver, ExecutableElement consumerMethod,
                                   ExecutableElement called, MethodInvocationTree tree) { }

    JavaDepthRoleBindings.Invocation find(JavaDepthRoles.SourceType consumer, VariableElement field, TypeElement contract,
                                   Map<Element, ExecutableElement> matches) {
        Map<ExecutableElement, ExecutableElement> inverse = inverses.computeIfAbsent(matches, values -> {
            Map<ExecutableElement, ExecutableElement> result = new IdentityHashMap<>();
            for (Map.Entry<Element, ExecutableElement> entry : values.entrySet()) {
                if (entry.getKey() instanceof ExecutableElement implementation) result.putIfAbsent(entry.getValue(), implementation);
            }
            return result;
        });
        UseKey key = new UseKey(consumer, field, contract);
        Map<UseKey, JavaDepthRoleBindings.Invocation> known = evidence.computeIfAbsent(matches, ignored -> new HashMap<>());
        if (known.containsKey(key)) return known.get(key);
        Map<ExecutableElement, OrderedSite> requiredUses = uses.computeIfAbsent(key, ignored -> {
            Map<ExecutableElement, OrderedSite> result = new IdentityHashMap<>();
            int ordinal = 0;
            for (InvocationSite site : invocations.getOrDefault(consumer, Map.of()).getOrDefault(field, List.of())) {
                ExecutableElement required = requiredMethod(contract, site.called());
                if (required != null) result.putIfAbsent(required, new OrderedSite(site, ordinal));
                ordinal++;
            }
            return result;
        });
        OrderedSite first = null;
        ExecutableElement selectedRequired = null;
        // Probe the smaller indexed side, retaining the first invocation in source order.
        Set<ExecutableElement> candidates = inverse.size() < requiredUses.size() ? inverse.keySet() : requiredUses.keySet();
        for (ExecutableElement required : candidates) {
            if (!inverse.containsKey(required)) continue;
            OrderedSite site = requiredUses.get(required);
            if (site != null && (first == null || site.ordinal() < first.ordinal())) {
                first = site;
                selectedRequired = required;
            }
        }
        JavaDepthRoleBindings.Invocation result = first == null ? null : new JavaDepthRoleBindings.Invocation(
                first.site().consumerMethod(), selectedRequired, inverse.get(selectedRequired), first.site().tree());
        known.put(key, result);
        return result;
    }

    private ExecutableElement requiredMethod(TypeElement contract, ExecutableElement called) {
        Map<ExecutableElement, ExecutableElement> known = resolutions.computeIfAbsent(contract, ignored -> new IdentityHashMap<>());
        if (known.containsKey(called)) return known.get(called);
        ExecutableElement result = null;
        for (ExecutableElement candidate : contractMethods.computeIfAbsent(contract, this::methods)
                .candidates(called)) {
            if (candidate == called || elements.overrides(called, candidate, contract)) { result = candidate; break; }
        }
        known.put(called, result);
        return result;
    }

    private JavaDepthRoleMethodIndex methods(TypeElement contract) {
        List<ExecutableElement> result = new ArrayList<>();
        for (Element item : elements.getAllMembers(contract)) {
            if (item instanceof ExecutableElement method && method.getKind() == ElementKind.METHOD) result.add(method);
        }
        return new JavaDepthRoleMethodIndex(owner.types, contract, result);
    }

    private void indexInvocations() {
        for (JavaDepthRoles.SourceType source : owner.sources) {
            for (Tree member : source.tree().getMembers()) {
                if (!(member instanceof MethodTree methodTree) || methodTree.getBody() == null) continue;
                TreePath methodPath = new TreePath(source.path(), methodTree);
                Element methodElement = trees.getElement(methodPath);
                if (!(methodElement instanceof ExecutableElement consumerMethod)) continue;
                new TreePathScanner<Void, Void>() {
                    @Override public Void visitMethodInvocation(MethodInvocationTree call, Void unused) {
                        ExpressionTree select = call.getMethodSelect();
                        if (select instanceof MemberSelectTree selected) {
                            Element receiver = trees.getElement(new TreePath(getCurrentPath(), selected.getExpression()));
                            Element called = trees.getElement(getCurrentPath());
                            if (receiver instanceof VariableElement field && called instanceof ExecutableElement executable) {
                                invocations.computeIfAbsent(source, ignored -> new IdentityHashMap<>()).computeIfAbsent(field, ignored -> new ArrayList<>()).add(new InvocationSite(field, consumerMethod, executable, call));
                            }
                        }
                        return super.visitMethodInvocation(call, unused);
                    }
                }.scan(methodPath, null);
            }
        }
    }

}
