package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import javax.lang.model.util.Types;
import java.util.*;

final class JavaDepthRoleBindings {
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Types types;
    private final List<JavaDepthRoles.SourceType> sources;
    private final Map<ExecutableElement, Map<VariableElement, VariableElement>> storedFields = new IdentityHashMap<>();
    private final JavaDepthRoleContract contractEvidence;
    private final JavaDepthRoleConstruction construction;
    private record Slot(JavaDepthRoles.SourceType consumer, ExecutableElement constructor,
                        VariableElement field, int index) { }
    private final Map<TypeElement, Map<String, List<Slot>>> slotsByImplementation = new IdentityHashMap<>();

    JavaDepthRoleBindings(JavaDepthRoles owner) {
        this.owner = owner;
        this.trees = owner.trees;
        this.types = owner.types;
        this.sources = owner.sources;
        this.contractEvidence = new JavaDepthRoleContract(owner);
        this.construction = new JavaDepthRoleConstruction(owner);
        indexSlots();
    }

    private void indexSlots() {
        for (JavaDepthRoles.SourceType consumer : sources) {
            for (Tree member : consumer.tree().getMembers()) {
                if (!(member instanceof MethodTree constructorTree)) continue;
                TreePath path = new TreePath(consumer.path(), constructorTree);
                Element element = trees.getElement(path);
                if (!(element instanceof ExecutableElement constructor) || constructor.getKind() != ElementKind.CONSTRUCTOR) continue;
                List<? extends VariableElement> parameters = constructor.getParameters();
                for (int index = 0; index < parameters.size(); index++) {
                    VariableElement parameter = parameters.get(index);
                    VariableElement field = storedFinalField(path, parameter);
                    if (field == null) continue;
                    String contract = JavaDepthRoles.typeKey(types.erasure(parameter.asType()));
                    Slot slot = new Slot(consumer, constructor, field, index);
                    for (TypeElement implementation : construction.implementations(consumer, constructor, index)) {
                        slotsByImplementation.computeIfAbsent(implementation, ignored -> new HashMap<>())
                                .computeIfAbsent(contract, ignored -> new ArrayList<>()).add(slot);
                    }
                }
            }
        }
    }

    List<Map<String, Object>> findBindings(JavaDepthRoles.SourceType implementation, TypeElement contract,
                                           Map<Element, ExecutableElement> matches) {
        List<Map<String, Object>> result = new ArrayList<>();
        String contractKey = JavaDepthRoles.typeKey(types.erasure(contract.asType()));
        for (Slot slot : slotsByImplementation.getOrDefault(implementation.element(), Map.of())
                .getOrDefault(contractKey, List.of())) {
            Invocation invocation = contractUse(slot.consumer(), slot.field(), contract, matches);
            if (invocation == null) continue;
            String constructorID = constructorID(slot.constructor());
            String memberID = methodID(invocation.implementation);
            String useID = methodID(invocation.consumerMethod) + "/call/" + methodID(invocation.contractMethod);
            result.add(Map.of(
                    "member", memberID,
                    "contract", contract.getQualifiedName().toString(),
                    "contract_member", methodID(invocation.contractMethod),
                    "consumer", constructorID,
                    "slot", constructorID + "/arg" + slot.index(),
                    "use", useID,
                    "provenance", List.of(provenance(slot.consumer(), invocation.tree(), invocation.consumerMethod))
            ));
        }
        return result;
    }

    private VariableElement storedFinalField(TreePath constructorPath, VariableElement parameter) {
        MethodTree tree = (MethodTree) constructorPath.getLeaf();
        Element constructorElement = trees.getElement(constructorPath);
        if (!(constructorElement instanceof ExecutableElement constructor)) return null;
        Map<VariableElement, VariableElement> known = storedFields.get(constructor);
        if (known != null) return known.get(parameter);
        known = new IdentityHashMap<>();
        storedFields.put(constructor, known);
        if (tree.getBody() == null) return null;
        Map<VariableElement, VariableElement> indexed = known;
        Set<VariableElement> parameters = Collections.newSetFromMap(new IdentityHashMap<>());
        parameters.addAll(constructor.getParameters());
        new TreePathScanner<Void, Void>() {
            @Override public Void visitAssignment(AssignmentTree assignment, Void unused) {
                Element left = trees.getElement(new TreePath(getCurrentPath(), assignment.getVariable()));
                Element right = trees.getElement(new TreePath(getCurrentPath(), assignment.getExpression()));
                if (left instanceof VariableElement field && field.getKind() == ElementKind.FIELD
                        && field.getModifiers().contains(Modifier.FINAL)
                        && right instanceof VariableElement value && parameters.contains(value)) {
                    indexed.put(value, field);
                }
                return super.visitAssignment(assignment, unused);
            }
        }.scan(constructorPath, null);
        return known.get(parameter);
    }

    record Invocation(ExecutableElement consumerMethod, ExecutableElement contractMethod,
                              ExecutableElement implementation, MethodInvocationTree tree) { }

    private Invocation contractUse(JavaDepthRoles.SourceType consumer, VariableElement field,
                                   TypeElement contract, Map<Element, ExecutableElement> matches) {
        return contractEvidence.find(consumer, field, contract, matches);
    }

    private static String methodID(ExecutableElement method) {
        return JavaDepthRoles.methodID(method);
    }

    private static String constructorID(ExecutableElement constructor) {
        return JavaDepthRoles.constructorID(constructor);
    }

    private Map<String, Object> provenance(JavaDepthRoles.SourceType source, Tree tree, ExecutableElement method) {
        return owner.provenance(source, tree, method);
    }
}
