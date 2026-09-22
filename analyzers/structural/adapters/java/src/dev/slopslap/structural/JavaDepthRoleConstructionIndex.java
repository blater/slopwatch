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
    private final Map<VariableElement, Set<TypeElement>> aliases = new IdentityHashMap<>();
    private final Map<TypeElement, Map<TypeElement, Map<Integer, Set<TypeElement>>>> constructorTypes = new IdentityHashMap<>();
    private final Map<TypeElement, Map<ExecutableElement, Map<Integer, Set<TypeElement>>>> consumerTargets = new IdentityHashMap<>();
    private final Map<TypeElement, Map<ExecutableElement, Map<Integer, Set<TypeElement>>>> constructorTargets = new IdentityHashMap<>();

    JavaDepthRoleConstructionIndex(JavaDepthRoles owner) {
        this.owner = owner;
        trees = owner.trees;
        indexConstructionUses();
    }

    Set<TypeElement> implementations(JavaDepthRoles.SourceType consumer, ExecutableElement target, int parameterIndex) {
        Set<TypeElement> result = newIdentitySet();
        TypeElement targetType = target.getEnclosingElement() instanceof TypeElement type ? type : null;
        var byType = constructorTypes.get(consumer.element());
        if (byType != null && targetType != null) add(result, byType.get(targetType), parameterIndex);
        var local = consumerTargets.get(consumer.element());
        if (local != null) add(result, local.get(target), parameterIndex);
        var global = constructorTargets.get(targetType);
        if (global != null) add(result, global.get(target), parameterIndex);
        return result;
    }

    private static void add(Set<TypeElement> result, Map<Integer, Set<TypeElement>> slots, int index) {
        if (slots != null) result.addAll(slots.getOrDefault(index, Set.of()));
    }

    private void indexConstructionUses() {
        for (JavaDepthRoles.SourceType source : owner.sources) {
            if (JavaDepthRoles.isTestPath(source.file())) continue;
            indexConstructors(source);
            // Top-level trees already include every nested declaration.
            if (!(source.path().getParentPath().getLeaf() instanceof CompilationUnitTree)) continue;
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
            Set<TypeElement> completed = aliases.get(variable);
            if (completed != null) return completed;
            VariableTree field = owner.fieldTrees.get(variable);
            TreePath fieldPath = owner.fieldPaths.get(variable);
            if (field != null && fieldPath != null && field.getInitializer() != null) {
                result.addAll(resolvesTo(field.getInitializer(), new TreePath(fieldPath, field.getInitializer()), seen));
            }
            // Alias expressions have a single successor; a cycle therefore has no concrete construction.
            Set<TypeElement> completedResult = Set.copyOf(result);
            aliases.put(variable, completedResult);
            return completedResult;
        }
        if (element instanceof TypeElement type) result.add(type);
        return result;
    }

}
