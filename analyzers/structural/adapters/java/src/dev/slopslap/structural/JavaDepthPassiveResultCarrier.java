package dev.slopslap.structural;

import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.BlockTree;
import com.sun.source.tree.ClassTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.VariableTree;
import com.sun.source.tree.UnaryTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Structural proof for mutable result objects which only store and expose values. */
final class JavaDepthPassiveResultCarrier {
    private JavaDepthPassiveResultCarrier() { }

    static Map<String, Object> inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (!(ownerPath.getLeaf() instanceof ClassTree declaration) || !eligibleType(owner, declaration)) return null;
        List<VariableElement> fields = fields(trees, ownerPath, declaration);
        if (fields.isEmpty() || fields.size() > 256) return null;
        Map<Element, VariableElement> fieldSet = new HashMap<>();
        for (VariableElement field : fields) fieldSet.put(field, field);
        Counts counts = new Counts();
        Set<VariableElement> getters = new HashSet<>();
        for (VariableElement field : fields) if (!field.getModifiers().contains(Modifier.PRIVATE)) getters.add(field);
        int exposedFields = getters.size();
        for (Tree member : declaration.getMembers()) {
            if (member instanceof VariableTree variable) {
                if (!pureInitializer(trees, ownerPath, variable, fieldSet)) return null;
                continue;
            }
            if (!(member instanceof MethodTree methodTree)) return null;
            Element element = trees.getElement(new TreePath(ownerPath, methodTree));
            if (!(element instanceof ExecutableElement method) || !eligibleMethod(method)) return null;
            TreePath methodPath = new TreePath(ownerPath, methodTree);
            if (method.getKind() == ElementKind.CONSTRUCTOR) {
                if (!directAssignments(trees, methodPath, methodTree.getBody(), method, fieldSet)) return null;
                counts.constructors++;
            } else if (getter(trees, methodPath, method, fieldSet, getters)) {
            } else if (directAssignments(trees, methodPath, methodTree.getBody(), method, fieldSet)) {
                counts.mutators++;
            } else {
                return null;
            }
        }
        if (getters.size() != fields.size()) return null;
        Map<String, Object> evidence = new HashMap<>(marker(owner, fields.size(), getters.size() - exposedFields, counts.mutators, counts.constructors));
        if (exposedFields > 0) evidence.put("kind", "passive-value-object-v1");
        return evidence;
    }

    private static boolean eligibleType(TypeElement owner, ClassTree tree) {
        if (owner.getKind() != ElementKind.CLASS || owner.getModifiers().contains(Modifier.ABSTRACT)
                || owner.getNestingKind().isNested() && !owner.getModifiers().contains(Modifier.STATIC)
                || !JavaDepthPassiveValueObject.interfacesWithoutBehavior(owner)) return false;
        TypeMirror superclass = owner.getSuperclass();
        return superclass.getKind() == TypeKind.NONE
                || superclass instanceof DeclaredType declared
                && declared.asElement() instanceof TypeElement parent
                && parent.getQualifiedName().contentEquals("java.lang.Object");
    }

    private static List<VariableElement> fields(Trees trees, TreePath ownerPath, ClassTree declaration) {
        List<VariableElement> result = new ArrayList<>();
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof VariableTree variable)) continue;
            Element element = trees.getElement(new TreePath(ownerPath, variable));
            if (!(element instanceof VariableElement field)
                    || field.getModifiers().contains(Modifier.STATIC)
                    || field.getModifiers().contains(Modifier.VOLATILE)) return List.of();
            result.add(field);
        }
        return result;
    }

    private static boolean eligibleMethod(ExecutableElement method) {
        return !method.getModifiers().contains(Modifier.STATIC)
                && !method.getModifiers().contains(Modifier.SYNCHRONIZED)
                && !method.getModifiers().contains(Modifier.ABSTRACT)
                && !method.getModifiers().contains(Modifier.NATIVE)
                && !method.isVarArgs() && method.getTypeParameters().isEmpty()
                && method.getThrownTypes().isEmpty();
    }

    private static boolean pureInitializer(Trees trees, TreePath ownerPath, VariableTree variable,
                                           Map<Element, VariableElement> fields) {
        Element element = trees.getElement(new TreePath(ownerPath, variable));
        if (!(element instanceof VariableElement field) || !fields.containsKey(field)) return false;
        return variable.getInitializer() == null
                || allowedValue(trees, new TreePath(ownerPath, variable), variable.getInitializer(), field.asType(), Map.of());
    }

    private static boolean getter(Trees trees, TreePath methodPath, ExecutableElement method,
                                  Map<Element, VariableElement> fields, Set<VariableElement> getters) {
        if (method.getReturnType().getKind() == TypeKind.VOID || !method.getParameters().isEmpty()) return false;
        MethodTree tree = (MethodTree) methodPath.getLeaf();
        if (tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        VariableElement field = directField(trees, methodPath, returned.getExpression(), fields);
        if (field == null || !sameType(field.asType(), method.getReturnType())) return false;
        getters.add(field);
        return true;
    }

    private static boolean directAssignments(Trees trees, TreePath methodPath, BlockTree body,
                                             ExecutableElement method, Map<Element, VariableElement> fields) {
        if (body == null) return false;
        Map<Element, VariableElement> parameters = new HashMap<>();
        for (VariableElement parameter : method.getParameters()) parameters.put(parameter, parameter);
        List<? extends StatementTree> statements = body.getStatements();
        if (statements.isEmpty()) return false;
        for (StatementTree statement : statements) {
            if (method.getKind() == ElementKind.CONSTRUCTOR && objectSuper(trees, methodPath, statement)) continue;
            if (!(statement instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)) return false;
            TreePath statementPath = TreePath.getPath(methodPath, statement);
            if (statementPath == null) return false;
            VariableElement field = directField(trees, statementPath, assignment.getVariable(), fields);
            if (field == null || !allowedValue(trees, statementPath, assignment.getExpression(), field.asType(), parameters)) return false;
        }
        return true;
    }

    private static boolean objectSuper(Trees trees, TreePath methodPath, StatementTree statement) {
        if (!(statement instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof MethodInvocationTree invocation)
                || !invocation.getMethodSelect().toString().contentEquals("super")
                || !invocation.getArguments().isEmpty()) return false;
        Element target = trees.getElement(TreePath.getPath(methodPath, invocation));
        return target instanceof ExecutableElement constructor
                && constructor.getKind() == ElementKind.CONSTRUCTOR
                && constructor.getEnclosingElement() instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.Object");
    }

    private static VariableElement directField(Trees trees, TreePath parent, Tree expression,
                                               Map<Element, VariableElement> fields) {
        TreePath path = TreePath.getPath(parent, expression);
        Element element = path == null ? null : trees.getElement(path);
        if (!(element instanceof VariableElement field) || !fields.containsKey(field)) return null;
        if (expression instanceof IdentifierTree) return field;
        if (expression instanceof MemberSelectTree member && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this")) return field;
        return null;
    }

    private static boolean allowedValue(Trees trees, TreePath parent, Tree expression, TypeMirror expected,
                                        Map<Element, VariableElement> parameters) {
        TreePath path = TreePath.getPath(parent, expression);
        if (path == null) return false;
        Element element = trees.getElement(path);
        if (element instanceof VariableElement variable && parameters.containsKey(variable)) {
            return sameType(variable.asType(), expected);
        }
        if (expression instanceof UnaryTree unary && (unary.getKind() == Tree.Kind.UNARY_MINUS || unary.getKind() == Tree.Kind.UNARY_PLUS)
                && unary.getExpression() instanceof LiteralTree literal && literal.getValue() instanceof Number) {
            return literalTypeMatches(trees.getTypeMirror(path), expected);
        }
        if (expression instanceof LiteralTree literal) {
            return literal.getValue() == null ? expected.getKind() != TypeKind.NULL && !expected.getKind().isPrimitive()
                    : literalTypeMatches(trees.getTypeMirror(path), expected);
        }
        if (element instanceof VariableElement constant && constant.getKind() == ElementKind.ENUM_CONSTANT) {
            return constant.getEnclosingElement() instanceof TypeElement enumType
                    && expected instanceof DeclaredType declared && declared.asElement().equals(enumType);
        }
        if (element instanceof VariableElement constant && constant.getConstantValue() != null
                && constant.getModifiers().contains(Modifier.STATIC)
                && constant.getModifiers().contains(Modifier.FINAL)) {
            return literalTypeMatches(constant.asType(), expected);
        }
        return false;
    }

    private static boolean literalTypeMatches(TypeMirror actual, TypeMirror expected) {
        if (actual == null || expected == null) return false;
        if (sameType(actual, expected)) return true;
        return actual.getKind() == TypeKind.INT && expected.getKind() == TypeKind.LONG;
    }

    private static boolean sameType(TypeMirror left, TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }

    private static Map<String, Object> marker(TypeElement owner, int fields, int getters,
                                               int mutators, int constructors) {
        return Map.of("id", owner.getQualifiedName() + "/passive-result-carrier",
                "kind", "passive-result-carrier-v1", "status", "proven",
                "details", Map.of("instance_fields", fields, "direct_getters", getters,
                        "direct_mutators", mutators, "constructors", constructors),
                "provenance", List.of());
    }

    private static final class Counts {
        int constructors;
        int mutators;
    }
}
