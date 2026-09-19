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
    private final Map<JavaDepthRoles.SourceType, List<InvocationSite>> invocations = new IdentityHashMap<>();
    private final Map<TypeElement, List<ExecutableElement>> contractMethods = new IdentityHashMap<>();

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
        for (InvocationSite site : invocations.getOrDefault(consumer, List.of())) {
            if (site.receiver() != field) continue;
            ExecutableElement required = requiredMethod(contract, site.called());
            if (required == null) continue;
            for (Map.Entry<Element, ExecutableElement> entry : matches.entrySet()) {
                if (entry.getValue().equals(required) && entry.getKey() instanceof ExecutableElement implementation) {
                    return new JavaDepthRoleBindings.Invocation(site.consumerMethod(), required, implementation, site.tree());
                }
            }
        }
        return null;
    }

    private ExecutableElement requiredMethod(TypeElement contract, ExecutableElement called) {
        for (ExecutableElement candidate : contractMethods.computeIfAbsent(contract, this::methods)) {
            if (candidate == called || elements.overrides(called, candidate, contract)) return candidate;
        }
        return null;
    }

    private List<ExecutableElement> methods(TypeElement contract) {
        List<ExecutableElement> result = new ArrayList<>();
        for (Element item : elements.getAllMembers(contract)) {
            if (item instanceof ExecutableElement method && method.getKind() == ElementKind.METHOD) result.add(method);
        }
        return List.copyOf(result);
    }

    private void indexInvocations() {
        for (JavaDepthRoles.SourceType source : owner.sources) {
            List<InvocationSite> sites = new ArrayList<>();
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
                                sites.add(new InvocationSite(field, consumerMethod, executable, call));
                            }
                        }
                        return super.visitMethodInvocation(call, unused);
                    }
                }.scan(methodPath, null);
            }
            invocations.put(source, List.copyOf(sites));
        }
    }

}
