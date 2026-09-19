package dev.slopslap.structural;

import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.ClassTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.RecordComponentElement;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeKind;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Proof for a plain record whose generated/value accessors expose only components. */
final class JavaDepthPassiveValueObject {
    private JavaDepthPassiveValueObject() { }

    record Proof(List<ExecutableElement> constructors, List<ExecutableElement> accessors, int components) {
        Proof {
            constructors = List.copyOf(constructors);
            accessors = List.copyOf(accessors);
        }
    }

    static Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (owner.getKind() != ElementKind.RECORD || !(ownerPath.getLeaf() instanceof ClassTree declaration)
                || !interfacesWithoutBehavior(owner) || declaration.getExtendsClause() != null
                || declaration.getMembers().size() > 256) return null;
        List<RecordComponentElement> components = components(owner);
        Map<String, RecordComponentElement> byName = new HashMap<>();
        for (RecordComponentElement component : components) byName.put(component.getSimpleName().toString(), component);
        if (!sourceMembersArePlain(trees, ownerPath, declaration, byName)) return null;
        List<ExecutableElement> constructors = new ArrayList<>();
        List<ExecutableElement> accessors = new ArrayList<>();
        for (Element element : owner.getEnclosedElements()) {
            if (element instanceof ExecutableElement executable) {
                if (executable.getKind() == ElementKind.CONSTRUCTOR) {
                    if (!canonicalConstructor(trees, ownerPath, executable, components, byName)) return null;
                    constructors.add(executable);
                } else if (isComponentAccessor(trees, ownerPath, executable, byName)) {
                    accessors.add(executable);
                } else if (trees.getPath(executable) != null) {
                    return null;
                }
            }
        }
        if (accessors.size() != components.size()) return null;
        if (constructors.isEmpty()) return new Proof(List.of(), accessors, components.size());
        return new Proof(constructors, accessors, components.size());
    }

    // Abstract contracts contribute no inherited instance behavior. Bound the
    // traversal so unknown or unusually large interface graphs remain unproven.
    static boolean interfacesWithoutBehavior(TypeElement owner) {
        var pending = new java.util.ArrayDeque<javax.lang.model.type.TypeMirror>(owner.getInterfaces());
        Set<Element> seen = new HashSet<>();
        while (!pending.isEmpty()) {
            var type = pending.removeFirst();
            if (type.getKind() != TypeKind.DECLARED || !(type instanceof javax.lang.model.type.DeclaredType declared)
                    || !(declared.asElement() instanceof TypeElement contract)
                    || contract.getKind() != ElementKind.INTERFACE) return false;
            if (!seen.add(contract)) continue;
            if (seen.size() > 32) return false;
            for (Element member : contract.getEnclosedElements()) {
                if (member.getKind() == ElementKind.METHOD && !member.getModifiers().contains(Modifier.STATIC) && !member.getModifiers().contains(Modifier.ABSTRACT)) return false;
            }
            pending.addAll(contract.getInterfaces());
        }
        return true;
    }

    private static List<RecordComponentElement> components(TypeElement owner) {
        List<RecordComponentElement> result = new ArrayList<>();
        for (Element element : owner.getEnclosedElements()) {
            if (element.getKind() == ElementKind.RECORD_COMPONENT && element instanceof RecordComponentElement component) {
                result.add(component);
            }
        }
        return result;
    }

    private static boolean sourceMembersArePlain(Trees trees, TreePath ownerPath, ClassTree declaration,
                                                 Map<String, RecordComponentElement> components) {
        for (Tree member : declaration.getMembers()) {
            if (member instanceof MethodTree method) {
                Element element = trees.getElement(new TreePath(ownerPath, method));
                if (!(element instanceof ExecutableElement executable)) return false;
                if (executable.getKind() != ElementKind.CONSTRUCTOR
                        && !isComponentAccessor(trees, ownerPath, executable, components)) return false;
                continue;
            }
            if (member instanceof VariableTree variable) {
                Element element = trees.getElement(new TreePath(ownerPath, variable));
                if (!(element instanceof VariableElement field)
                        || !components.containsKey(field.getSimpleName().toString())) return false;
                continue;
            }
            return false;
        }
        return true;
    }

    private static boolean canonicalConstructor(Trees trees, TreePath ownerPath, ExecutableElement constructor,
                                                List<RecordComponentElement> components,
                                                Map<String, RecordComponentElement> byName) {
        if (constructor.isVarArgs() || !constructor.getTypeParameters().isEmpty()
                || !constructor.getThrownTypes().isEmpty() || constructor.getParameters().size() != components.size()) return false;
        for (int index = 0; index < components.size(); index++) {
            if (!sameType(constructor.getParameters().get(index).asType(), components.get(index).asType())) return false;
        }
        TreePath constructorPath = trees.getPath(constructor);
        if (constructorPath == null || !(constructorPath.getLeaf() instanceof MethodTree method) || method.getBody() == null) return true;
        List<? extends StatementTree> statements = method.getBody().getStatements();
        if (statements.isEmpty()) return true;
        Map<Element, VariableElement> parameters = new HashMap<>();
        for (VariableElement parameter : constructor.getParameters()) parameters.put(parameter, parameter);
        Set<String> assigned = new HashSet<>();
        Set<Element> copied = new HashSet<>();
        boolean assignmentSeen = false;
        for (StatementTree statement : statements) {
            if (recordSuper(trees, constructorPath, statement)) continue;
            if (!(statement instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)) return false;
            TreePath statementPath = TreePath.getPath(constructorPath, statement);
            String field = directField(trees, statementPath, assignment.getVariable(), byName);
            Element target = trees.getElement(TreePath.getPath(statementPath, assignment.getVariable()));
            Tree valueExpression = assignment.getExpression();
            boolean defensiveCopy = valueExpression instanceof MethodInvocationTree;
            if (defensiveCopy) {
                MethodInvocationTree call = (MethodInvocationTree) valueExpression;
                Element resolved = trees.getElement(TreePath.getPath(statementPath, call));
                if (!(resolved instanceof ExecutableElement methodTarget)
                        || !methodTarget.getModifiers().contains(Modifier.STATIC)
                        || !methodTarget.getSimpleName().contentEquals("copyOf")
                        || !(methodTarget.getEnclosingElement() instanceof TypeElement type)
                        || !type.getQualifiedName().contentEquals("java.util.List")
                        || call.getArguments().size() != 1) return false;
                valueExpression = call.getArguments().get(0);
            }
            Element value = trees.getElement(TreePath.getPath(statementPath, valueExpression));
            if (field == null || !(valueExpression instanceof IdentifierTree)
                    || !(value instanceof VariableElement parameter) || !parameters.containsKey(parameter)
                    || !parameter.getSimpleName().contentEquals(field)
                    || !sameType(parameter.asType(), byName.get(field).asType())) return false;
            if (target != null && target.getKind() == ElementKind.PARAMETER) {
                // Compact constructors normalize the component parameter; javac
                // supplies the eventual field assignment. Only self-copy is safe.
                if (!defensiveCopy || !target.equals(parameter) || !copied.add(target)) return false;
            } else {
                if (target == null || target.getKind() != ElementKind.FIELD
                        || !target.getEnclosingElement().equals(constructor.getEnclosingElement())
                        || !assigned.add(field)) return false;
                assignmentSeen = true;
            }
        }
        return !assignmentSeen || assigned.size() == components.size();
    }

    private static boolean recordSuper(Trees trees, TreePath constructorPath, StatementTree statement) {
        if (!(statement instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof com.sun.source.tree.MethodInvocationTree invocation)
                || !invocation.getMethodSelect().toString().contentEquals("super")
                || !invocation.getArguments().isEmpty()) return false;
        Element target = trees.getElement(TreePath.getPath(constructorPath, invocation));
        return target instanceof ExecutableElement constructor
                && constructor.getKind() == ElementKind.CONSTRUCTOR
                && constructor.getEnclosingElement() instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.Record");
    }

    private static boolean isComponentAccessor(Trees trees, TreePath ownerPath, ExecutableElement method,
                                                Map<String, RecordComponentElement> components) {
        if (method.getKind() != ElementKind.METHOD || !method.getParameters().isEmpty()
                || method.getReturnType().getKind() == TypeKind.VOID) return false;
        RecordComponentElement component = components.get(method.getSimpleName().toString());
        if (component == null || !sameType(component.asType(), method.getReturnType())) return false;
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree)) return true;
        if (tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        String field = directField(trees, TreePath.getPath(methodPath, returned), returned.getExpression(), components);
        return field != null && component.getSimpleName().contentEquals(field);
    }

    private static String directField(Trees trees, TreePath parent, Tree expression,
                                               Map<String, RecordComponentElement> components) {
        if (parent == null) return null;
        Element element = trees.getElement(TreePath.getPath(parent, expression));
        if (!(element instanceof VariableElement field) || !components.containsKey(field.getSimpleName().toString())) return null;
        if (expression instanceof IdentifierTree) return field.getSimpleName().toString();
        return expression instanceof MemberSelectTree member && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this") ? field.getSimpleName().toString() : null;
    }

    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }

    static Map<String, Object> evidence(TypeElement owner, Proof proof) {
        return Map.of("id", owner.getQualifiedName() + "/passive-value-object",
                "kind", "passive-value-object-v1", "status", "proven",
                "details", Map.of("components", proof.components(), "direct_getters", proof.accessors().size(),
                        "constructors", proof.constructors().size()), "provenance", List.of());
    }
}
