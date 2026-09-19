package dev.slopslap.structural;

import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.ClassTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.LiteralTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.VariableTree;
import com.sun.source.tree.NewClassTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeKind;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Proof for an enum with direct metadata storage and direct field getters. */
final class JavaDepthPassiveEnum {
    private JavaDepthPassiveEnum() { }

    record Proof(List<ExecutableElement> constructors, List<ExecutableElement> accessors,
                 List<ExecutableElement> staticAccessors, List<VariableElement> constants, int fields) {
        Proof {
            constructors = List.copyOf(constructors);
            accessors = List.copyOf(accessors);
            staticAccessors = List.copyOf(staticAccessors);
            constants = List.copyOf(constants);
        }
    }

    static Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (owner.getKind() != ElementKind.ENUM || !(ownerPath.getLeaf() instanceof ClassTree declaration)
                || !owner.getTypeParameters().isEmpty() || !owner.getInterfaces().isEmpty()
                || owner.getNestingKind().isNested() || declaration.getMembers().size() > 256) return null;
        List<VariableElement> constants = new ArrayList<>();
        List<VariableElement> fields = new ArrayList<>();
        Map<String, VariableElement> staticFields = new HashMap<>();
        for (Element element : owner.getEnclosedElements()) {
            if (element.getKind() == ElementKind.ENUM_CONSTANT && element instanceof VariableElement constant) constants.add(constant);
            if (element.getKind() == ElementKind.FIELD && element instanceof VariableElement field
                    && trees.getPath(field) != null) {
                if (field.getModifiers().contains(Modifier.STATIC)) {
                    if (!field.getModifiers().contains(Modifier.FINAL)
                            || field.getConstantValue() == null && !valuesLengthField(trees, ownerPath, field)) return null;
                    staticFields.put(field.getSimpleName().toString(), field);
                } else {
                    if (!field.getModifiers().contains(Modifier.PRIVATE) || !field.getModifiers().contains(Modifier.FINAL)
                            || field.getModifiers().contains(Modifier.VOLATILE)) return null;
                    fields.add(field);
                }
            }
        }
        if (constants.isEmpty() || !constantTreesArePlain(trees, ownerPath, declaration)) return null;
        Map<String, VariableElement> byName = new HashMap<>();
        for (VariableElement field : fields) byName.put(field.getSimpleName().toString(), field);
        List<ExecutableElement> constructors = new ArrayList<>();
        List<ExecutableElement> accessors = new ArrayList<>();
        List<ExecutableElement> staticAccessors = new ArrayList<>();
        for (Element element : owner.getEnclosedElements()) {
            if (!(element instanceof ExecutableElement executable)) continue;
            if (executable.getKind() == ElementKind.CONSTRUCTOR) {
                if (!constructor(trees, executable, fields, byName)) return null;
                constructors.add(executable);
            } else if (isAccessor(trees, executable, byName)) {
                accessors.add(executable);
            } else if (isStaticAccessor(trees, executable, staticFields)) {
                staticAccessors.add(executable);
            } else if (trees.getPath(executable) != null) {
                return null;
            }
        }
        if (accessors.size() != fields.size()) return null;
        return new Proof(constructors, accessors, staticAccessors, constants, fields.size());
    }

    private static boolean constantTreesArePlain(Trees trees, TreePath ownerPath, ClassTree declaration) {
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof VariableTree variable)) {
                if (member instanceof MethodTree || member.getKind() == Tree.Kind.EMPTY_STATEMENT) continue;
                return false;
            }
            Element element = trees.getElement(new TreePath(ownerPath, variable));
            if (!(element instanceof VariableElement variableElement)) return false;
            if (variableElement.getKind() != ElementKind.ENUM_CONSTANT) continue;
            if (variable.getInitializer() instanceof NewClassTree created && created.getClassBody() != null) return false;
            if (variable.getInitializer() != null && !allowedEnumConstant(trees, TreePath.getPath(ownerPath, variable), variable.getInitializer())) return false;
        }
        return true;
    }

    private static boolean allowedEnumConstant(Trees trees, TreePath parent, Tree expression) {
        if (expression instanceof NewClassTree created) {
            if (created.getClassBody() != null) return false;
            for (Tree argument : created.getArguments()) if (!allowedEnumValue(trees, TreePath.getPath(parent, created), argument)) return false;
            return true;
        }
        return allowedEnumValue(trees, parent, expression);
    }

    private static boolean allowedEnumValue(Trees trees, TreePath parent, Tree expression) {
        if (expression instanceof LiteralTree) return true;
        Element element = trees.getElement(TreePath.getPath(parent, expression));
        if (element instanceof VariableElement variable) {
            return variable.getKind() == ElementKind.ENUM_CONSTANT
                    || variable.getModifiers().contains(Modifier.STATIC) && variable.getModifiers().contains(Modifier.FINAL)
                    && variable.getConstantValue() != null;
        }
        return false;
    }

    private static boolean constructor(Trees trees, ExecutableElement constructor, List<VariableElement> fields,
                                       Map<String, VariableElement> byName) {
        if (constructor.isVarArgs() || !constructor.getTypeParameters().isEmpty() || !constructor.getThrownTypes().isEmpty()) return false;
        TreePath constructorPath = trees.getPath(constructor);
        if (constructorPath == null || !(constructorPath.getLeaf() instanceof MethodTree method) || method.getBody() == null) return fields.isEmpty();
        Map<Element, VariableElement> parameters = new HashMap<>();
        for (VariableElement parameter : constructor.getParameters()) parameters.put(parameter, parameter);
        Set<VariableElement> assigned = new HashSet<>();
        for (StatementTree statement : method.getBody().getStatements()) {
            if (enumSuper(trees, constructorPath, statement)) continue;
            if (!(statement instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)) return false;
            TreePath statementPath = TreePath.getPath(constructorPath, statement);
            VariableElement field = directField(trees, statementPath, assignment.getVariable(), byName);
            Element value = trees.getElement(TreePath.getPath(statementPath, assignment.getExpression()));
            if (field == null || !assigned.add(field) || !(value instanceof VariableElement parameter)
                    || !parameters.containsKey(parameter) || !sameType(field.asType(), parameter.asType())) return false;
        }
        return assigned.size() == fields.size();
    }

    private static boolean enumSuper(Trees trees, TreePath constructorPath, StatementTree statement) {
        if (!(statement instanceof ExpressionStatementTree expression)
                || !(expression.getExpression() instanceof com.sun.source.tree.MethodInvocationTree invocation)
                || !invocation.getMethodSelect().toString().contentEquals("super")) return false;
        Element target = trees.getElement(TreePath.getPath(constructorPath, invocation));
        return target instanceof ExecutableElement constructor
                && constructor.getKind() == ElementKind.CONSTRUCTOR
                && constructor.getEnclosingElement() instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.Enum");
    }

    private static boolean isAccessor(Trees trees, ExecutableElement method, Map<String, VariableElement> fields) {
        if (method.getKind() != ElementKind.METHOD || method.getModifiers().contains(Modifier.STATIC)
                || !method.getParameters().isEmpty() || method.getReturnType().getKind() == TypeKind.VOID) return false;
        VariableElement field = fields.get(method.getSimpleName().toString());
        if (field == null || !sameType(field.asType(), method.getReturnType())) return false;
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree)) return true;
        if (tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        return directField(trees, TreePath.getPath(methodPath, returned), returned.getExpression(), fields) == field;
    }

    private static boolean isStaticAccessor(Trees trees, ExecutableElement method,
                                            Map<String, VariableElement> fields) {
        if (method.getKind() != ElementKind.METHOD || !method.getModifiers().contains(Modifier.STATIC)
                || !method.getParameters().isEmpty() || method.getReturnType().getKind() == TypeKind.VOID) return false;
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree)
                || tree.getBody() == null || tree.getBody().getStatements().size() != 1
                || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                || returned.getExpression() == null) return false;
        VariableElement field = directField(trees, TreePath.getPath(methodPath, returned), returned.getExpression(), fields);
        return field != null && sameType(field.asType(), method.getReturnType());
    }

    private static boolean valuesLengthField(Trees trees, TreePath ownerPath, VariableElement field) {
        TreePath fieldPath = trees.getPath(field);
        if (fieldPath == null || !(fieldPath.getLeaf() instanceof VariableTree variable)
                || !(variable.getInitializer() instanceof MemberSelectTree length)
                || !length.getIdentifier().contentEquals("length")
                || !(length.getExpression() instanceof com.sun.source.tree.MethodInvocationTree values)
                || !values.getArguments().isEmpty()) return false;
        Element target = trees.getElement(TreePath.getPath(fieldPath, values));
        return target instanceof ExecutableElement method && method.getSimpleName().contentEquals("values")
                && method.getEnclosingElement() == field.getEnclosingElement();
    }

    private static VariableElement directField(Trees trees, TreePath parent, Tree expression,
                                               Map<String, VariableElement> fields) {
        if (parent == null) return null;
        Element element = trees.getElement(TreePath.getPath(parent, expression));
        if (!(element instanceof VariableElement field) || !fields.containsValue(field)) return null;
        if (expression instanceof IdentifierTree) return field;
        return expression instanceof MemberSelectTree member && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this") ? field : null;
    }

    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }

    static Map<String, Object> evidence(TypeElement owner, Proof proof) {
        return Map.of("id", owner.getQualifiedName() + "/passive-enum", "kind", "passive-enum-v1", "status", "proven",
                "details", Map.of("constants", proof.constants().size(), "metadata_fields", proof.fields(),
                        "direct_getters", proof.accessors().size() + proof.staticAccessors().size(),
                        "constructors", proof.constructors().size()),
                "provenance", List.of());
    }
}
