package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.TypeMirror;
import javax.lang.model.util.Elements;
import javax.lang.model.util.Types;
import java.util.*;

final class JavaDepthRoleCandidate {
    record Result(List<TypeElement> contracts, List<String> memberIDs,
                  Map<Element, ExecutableElement> matches) { }

    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Elements elements;
    private final Types types;

    JavaDepthRoleCandidate(JavaDepthRoles owner) {
        this.owner = owner;
        this.trees = owner.trees;
        this.elements = owner.elements;
        this.types = owner.types;
    }

    Result analyze(JavaDepthRoles.SourceType source) {
        TypeElement candidateOwner = source.element();
        if (candidateOwner.getKind() != ElementKind.CLASS || owner.publiclyAccessible(candidateOwner)
                || !candidateOwner.getModifiers().contains(Modifier.FINAL)) return null;
        TypeMirror superclass = candidateOwner.getSuperclass();
        Element superclassElement = types.asElement(superclass);
        if (superclassElement instanceof TypeElement base
                && !base.getQualifiedName().contentEquals("java.lang.Object")) return null;
        List<TypeElement> contracts = contracts(candidateOwner);
        if (contracts.isEmpty()) return null;
        List<ExecutableElement> members = publicInstanceMethods(source);
        List<String> memberIDs = new ArrayList<>();
        Map<Element, ExecutableElement> matches = new IdentityHashMap<>();
        for (ExecutableElement member : members) {
            memberIDs.add(JavaDepthRoles.methodID(member));
            for (TypeElement contract : contracts) {
                ExecutableElement match = matchingContractMethod(candidateOwner, member, contract);
                if (match != null) {
                    matches.put(member, match);
                    break;
                }
            }
        }
        return new Result(contracts, memberIDs, matches);
    }

    private List<TypeElement> contracts(TypeElement owner) {
        List<TypeElement> contracts = new ArrayList<>();
        for (TypeMirror implemented : owner.getInterfaces()) {
            Element element = types.asElement(implemented);
            if (!(element instanceof TypeElement contract) || contract.getKind() != ElementKind.INTERFACE
                    || this.owner.publiclyAccessible(contract)) return List.of();
            contracts.add(contract);
        }
        return contracts;
    }

    private List<ExecutableElement> publicInstanceMethods(JavaDepthRoles.SourceType source) {
        List<ExecutableElement> methods = new ArrayList<>();
        for (Tree member : source.tree().getMembers()) {
            if (!(member instanceof MethodTree methodTree)) continue;
            Element element = trees.getElement(new TreePath(source.path(), methodTree));
            if (element instanceof ExecutableElement method && method.getKind() == ElementKind.METHOD
                    && method.getModifiers().contains(Modifier.PUBLIC)
                    && !method.getModifiers().contains(Modifier.STATIC)) methods.add(method);
        }
        return methods;
    }

    private ExecutableElement matchingContractMethod(TypeElement owner, ExecutableElement implementation,
                                                      TypeElement contract) {
        for (Element member : elements.getAllMembers(contract)) {
            if (member instanceof ExecutableElement required && required.getKind() == ElementKind.METHOD
                    && elements.overrides(implementation, required, owner)) return required;
        }
        return null;
    }
}
