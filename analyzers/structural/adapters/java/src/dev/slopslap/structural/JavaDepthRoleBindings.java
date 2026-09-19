package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import javax.lang.model.util.Elements;
import javax.lang.model.util.Types;
import java.util.*;

final class JavaDepthRoleBindings {
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Elements elements;
    private final Types types;
    private final List<JavaDepthRoles.SourceType> sources;
    private final Map<Element, VariableTree> fieldTrees;
    private final Map<Element, TreePath> fieldPaths;
    private final Map<ExecutableElement, Map<VariableElement, VariableElement>> storedFields = new IdentityHashMap<>();
    private final JavaDepthRoleContract contractEvidence;
    private final JavaDepthRoleConstruction construction;

    JavaDepthRoleBindings(JavaDepthRoles owner) {
        this.owner = owner;
        this.trees = owner.trees;
        this.elements = owner.elements;
        this.types = owner.types;
        this.sources = owner.sources;
        this.fieldTrees = owner.fieldTrees;
        this.fieldPaths = owner.fieldPaths;
        this.contractEvidence = new JavaDepthRoleContract(owner);
        this.construction = new JavaDepthRoleConstruction(owner);
    }

    List<Map<String, Object>> findBindings(JavaDepthRoles.SourceType implementation, TypeElement contract,
                                                   JavaDepthRoles.SourceType consumer, Map<Element, ExecutableElement> matches) {
        List<Map<String, Object>> result = new ArrayList<>();
        for (Tree member : consumer.tree().getMembers()) {
            if (!(member instanceof MethodTree constructorTree)) continue;
            TreePath constructorPath = new TreePath(consumer.path(), constructorTree);
            Element element = trees.getElement(constructorPath);
            if (!(element instanceof ExecutableElement constructor)
                    || constructor.getKind() != ElementKind.CONSTRUCTOR) continue;
            for (int index = 0; index < constructor.getParameters().size(); index++) {
                VariableElement parameter = constructor.getParameters().get(index);
                if (!sameType(parameter.asType(), contract.asType())) continue;
                VariableElement field = storedFinalField(constructorPath, parameter);
                if (field == null) continue;
                Invocation invocation = contractUse(consumer, field, contract, matches);
                if (invocation == null || !productionConstructionUses(consumer, constructor, implementation, index)) continue;
                String constructorID = constructorID(constructor);
                String memberID = methodID(invocation.implementation);
                String useID = methodID(invocation.consumerMethod) + "/call/" + methodID(invocation.contractMethod);
                result.add(Map.of(
                        "member", memberID,
                        "contract", contract.getQualifiedName().toString(),
                        "contract_member", methodID(invocation.contractMethod),
                        "consumer", constructorID,
                        "slot", constructorID + "/arg" + index,
                        "use", useID,
                        "provenance", List.of(provenance(consumer, invocation.tree(), invocation.consumerMethod))
                ));
            }
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
        new TreePathScanner<Void, Void>() {
            @Override public Void visitAssignment(AssignmentTree assignment, Void unused) {
                Element left = trees.getElement(new TreePath(getCurrentPath(), assignment.getVariable()));
                Element right = trees.getElement(new TreePath(getCurrentPath(), assignment.getExpression()));
                if (left instanceof VariableElement field && field.getKind() == ElementKind.FIELD
                        && field.getModifiers().contains(Modifier.FINAL)
                        && right instanceof VariableElement value && constructor.getParameters().contains(value)) {
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

    private boolean productionConstructionUses(JavaDepthRoles.SourceType consumer, ExecutableElement target,
                                               JavaDepthRoles.SourceType implementation, int parameterIndex) {
        return construction.uses(consumer, target, implementation, parameterIndex);
    }

    private boolean resolvesTo(TreePath parent, ExpressionTree expression, TypeElement implementation) {
        return construction.resolvesTo(parent, expression, implementation);
    }

    private boolean sameType(TypeMirror left, TypeMirror right) {
        return owner.sameType(left, right);
    }

    private static boolean isTestPath(String path) {
        return JavaDepthRoles.isTestPath(path);
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
