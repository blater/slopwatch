package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import java.util.Collections;
import java.util.HashMap;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/** Builds the bounded constructor and factory-use index for role attribution. */
final class JavaDepthRoleConstructionIndex {
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Map<TypeElement, Map<TypeElement, Map<Integer, Set<TypeElement>>>> constructorTypes = new IdentityHashMap<>();
    private final Map<TypeElement, Map<ExecutableElement, Map<Integer, Set<TypeElement>>>> consumerTargets = new IdentityHashMap<>();
    private final Map<TypeElement, Map<ExecutableElement, Map<Integer, Set<TypeElement>>>> constructorTargets = new IdentityHashMap<>();

    JavaDepthRoleConstructionIndex(JavaDepthRoles owner) {
        this.owner = owner;
        trees = owner.trees;
        indexConstructionUses();
    }

    boolean uses(JavaDepthRoles.SourceType consumer, ExecutableElement target,
                 JavaDepthRoles.SourceType implementation, int parameterIndex) {
        if (JavaDepthRoles.isTestPath(consumer.file())) return false;
        TypeElement targetType = target.getEnclosingElement() instanceof TypeElement type ? type : null;
        Map<TypeElement, Map<Integer, Set<TypeElement>>> byType = constructorTypes.get(consumer.element());
        if (targetType != null && contains(byType == null ? null : byType.get(targetType), parameterIndex,
                implementation.element())) return true;
        Map<ExecutableElement, Map<Integer, Set<TypeElement>>> localTargets = consumerTargets.get(consumer.element());
        if (contains(localTargets == null ? null : localTargets.get(target), parameterIndex, implementation.element())) return true;
        Map<ExecutableElement, Map<Integer, Set<TypeElement>>> byTarget = targetType == null ? null : constructorTargets.get(targetType);
        return contains(byTarget == null ? null : byTarget.get(target), parameterIndex, implementation.element());
    }

    private static boolean contains(Map<Integer, Set<TypeElement>> values, int index, TypeElement implementation) {
        return values != null && values.getOrDefault(index, Set.of()).contains(implementation);
    }

    private void indexConstructionUses() {
        for (JavaDepthRoles.SourceType source : owner.sources) {
            if (JavaDepthRoles.isTestPath(source.file())) continue;
            indexConstructors(source);
            new TreePathScanner<Void, Void>() {
                @Override public Void visitNewClass(NewClassTree created, Void unused) {
                    Element called = trees.getElement(getCurrentPath());
                    if (called instanceof ExecutableElement target) {
                        recordGlobal((TypeElement) target.getEnclosingElement(), target,
                                created.getArguments(), getCurrentPath());
                    }
                    return super.visitNewClass(created, unused);
                }
            }.scan(source.path(), null);
        }
    }

    private void indexConstructors(JavaDepthRoles.SourceType source) {
        for (Tree member : source.tree().getMembers()) {
            if (!(member instanceof MethodTree constructorTree)) continue;
            Element element = trees.getElement(new TreePath(source.path(), constructorTree));
            if (element instanceof ExecutableElement constructor && constructor.getKind() == ElementKind.CONSTRUCTOR) {
                indexConstructorBody(source, constructorTree, source.element());
            }
        }
    }

    private void indexConstructorBody(JavaDepthRoles.SourceType source, MethodTree tree, TypeElement consumer) {
        new TreePathScanner<Void, Void>() {
            @Override public Void visitMethodInvocation(MethodInvocationTree call, Void unused) {
                Element called = trees.getElement(getCurrentPath());
                if (called instanceof ExecutableElement target && call.getMethodSelect() instanceof IdentifierTree identifier
                        && identifier.getName().contentEquals("this")) {
                    record(consumerTargets.computeIfAbsent(consumer, key -> new IdentityHashMap<>()),
                            target, call.getArguments(), getCurrentPath());
                }
                return super.visitMethodInvocation(call, unused);
            }

            @Override public Void visitNewClass(NewClassTree created, Void unused) {
                Element createdType = owner.types.asElement(trees.getTypeMirror(getCurrentPath()));
                if (createdType instanceof TypeElement targetType) {
                    recordType(consumer, targetType, created.getArguments(), getCurrentPath());
                }
                return super.visitNewClass(created, unused);
            }
        }.scan(new TreePath(source.path(), tree), null);
    }

    private void record(Map<ExecutableElement, Map<Integer, Set<TypeElement>>> targetIndex,
                        ExecutableElement target, List<? extends ExpressionTree> arguments, TreePath parent) {
        for (int argumentIndex = 0; argumentIndex < arguments.size(); argumentIndex++) {
            ExpressionTree argument = arguments.get(argumentIndex);
            for (TypeElement resolved : resolvesTo(argument, new TreePath(parent, argument))) {
                targetIndex.computeIfAbsent(target, key -> new HashMap<>())
                        .computeIfAbsent(argumentIndex, key -> newIdentitySet()).add(resolved);
            }
        }
    }

    private void recordType(TypeElement consumer, TypeElement targetType,
                            List<? extends ExpressionTree> arguments, TreePath parent) {
        Map<TypeElement, Map<Integer, Set<TypeElement>>> targets = constructorTypes.computeIfAbsent(consumer,
                key -> new IdentityHashMap<>());
        for (int argumentIndex = 0; argumentIndex < arguments.size(); argumentIndex++) {
            ExpressionTree argument = arguments.get(argumentIndex);
            for (TypeElement resolved : resolvesTo(argument, new TreePath(parent, argument))) {
                targets.computeIfAbsent(targetType, key -> new HashMap<>())
                        .computeIfAbsent(argumentIndex, key -> newIdentitySet()).add(resolved);
            }
        }
    }

    private void recordGlobal(TypeElement targetType, ExecutableElement target,
                              List<? extends ExpressionTree> arguments, TreePath parent) {
        Map<ExecutableElement, Map<Integer, Set<TypeElement>>> targets = constructorTargets.computeIfAbsent(targetType,
                key -> new IdentityHashMap<>());
        record(targets, target, arguments, parent);
    }

    private Set<TypeElement> newIdentitySet() {
        return Collections.newSetFromMap(new IdentityHashMap<>());
    }

    private Set<TypeElement> resolvesTo(ExpressionTree expression, TreePath expressionPath) {
        return resolvesTo(expression, expressionPath, Collections.newSetFromMap(new IdentityHashMap<>()));
    }

    private Set<TypeElement> resolvesTo(ExpressionTree expression, TreePath expressionPath, Set<Element> seen) {
        Set<TypeElement> result = newIdentitySet();
        if (expression == null || expressionPath == null) return result;
        if (expression instanceof ParenthesizedTree parenthesized) {
            return resolvesTo(parenthesized.getExpression(),
                    new TreePath(expressionPath, parenthesized.getExpression()), seen);
        }
        Element element = trees.getElement(expressionPath);
        if (expression instanceof NewClassTree) {
            Element type = owner.types.asElement(trees.getTypeMirror(expressionPath));
            if (type instanceof TypeElement resolved) result.add(resolved);
            return result;
        }
        if (element instanceof VariableElement variable && variable.getModifiers().contains(Modifier.STATIC)
                && variable.getModifiers().contains(Modifier.FINAL) && seen.add(variable)) {
            VariableTree field = owner.fieldTrees.get(variable);
            TreePath fieldPath = owner.fieldPaths.get(variable);
            if (field != null && fieldPath != null && field.getInitializer() != null) {
                result.addAll(resolvesTo(field.getInitializer(), new TreePath(fieldPath, field.getInitializer()), seen));
            }
            return result;
        }
        if (element instanceof TypeElement type) result.add(type);
        return result;
    }

    boolean resolvesTo(TreePath parent, ExpressionTree expression, TypeElement implementation) {
        return resolvesTo(expression, new TreePath(parent, expression)).contains(implementation);
    }
}
