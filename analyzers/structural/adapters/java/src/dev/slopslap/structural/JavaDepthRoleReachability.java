package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import javax.lang.model.util.Types;
import java.util.*;

final class JavaDepthRoleReachability {
    private static final int MAX_REFERENCE_METHODS = JavaDepthRoles.MAX_REFERENCE_METHODS;
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Types types;
    private final List<JavaDepthRoles.SourceType> sources;
    private final Map<String, Map<ExecutableElement, Set<ExecutableElement>>> callers = new HashMap<>();
    private final Map<String, Map<TypeElement, Set<ExecutableElement>>> directReferences = new HashMap<>();
    private final Set<String> graphCutoffs = new HashSet<>();

    JavaDepthRoleReachability(JavaDepthRoles owner) {
        this.owner = owner;
        this.trees = owner.trees;
        this.types = owner.types;
        this.sources = owner.sources;
        indexCallGraph();
    }

    Set<ExecutableElement> candidateExposureMethods(TypeElement implementation,
                                                     List<TypeElement> contracts) {
        String pkg = JavaDepthRoles.packageName(implementation);
        if (graphCutoffs.contains(pkg)) owner.exposureCutoff = true;
        Set<ExecutableElement> seeds = directReferences.getOrDefault(pkg, Map.of())
                .getOrDefault(implementation, Collections.newSetFromMap(new IdentityHashMap<>()));
        Map<ExecutableElement, Set<ExecutableElement>> packageCallers = callers.getOrDefault(pkg, Map.of());
        Set<ExecutableElement> reachable = Collections.newSetFromMap(new IdentityHashMap<>());
        ArrayDeque<ExecutableElement> pending = new ArrayDeque<>(seeds);
        while (!pending.isEmpty()) {
            ExecutableElement method = pending.removeFirst();
            if (!reachable.add(method)) continue;
            if (reachable.size() > MAX_REFERENCE_METHODS) {
                owner.exposureCutoff = true;
                break;
            }
            Set<ExecutableElement> callersForMethod = packageCallers.get(method);
            if (callersForMethod != null) pending.addAll(callersForMethod);
        }
        return reachable;
    }

    private void indexCallGraph() {
        for (JavaDepthRoles.SourceType source : sources) {
            String pkg = JavaDepthRoles.packageName(source.element());
            Map<ExecutableElement, Set<ExecutableElement>> packageCallers = callers.computeIfAbsent(pkg,
                    ignored -> new IdentityHashMap<>());
            Map<TypeElement, Set<ExecutableElement>> packageSeeds = directReferences.computeIfAbsent(pkg,
                    ignored -> new IdentityHashMap<>());
            for (Tree member : source.tree().getMembers()) {
                if (!(member instanceof MethodTree methodTree)) continue;
                Element element = trees.getElement(new TreePath(source.path(), methodTree));
                if (!(element instanceof ExecutableElement method)) continue;
                new TreePathScanner<Void, Void>() {
                    @Override public Void visitIdentifier(IdentifierTree identifier, Void unused) {
                        directReference(trees.getElement(getCurrentPath()), method, packageSeeds);
                        return super.visitIdentifier(identifier, unused);
                    }
                    @Override public Void visitMemberSelect(MemberSelectTree selected, Void unused) {
                        directReference(trees.getElement(getCurrentPath()), method, packageSeeds);
                        return super.visitMemberSelect(selected, unused);
                    }
                    @Override public Void visitNewClass(NewClassTree created, Void unused) {
                        directReference(trees.getElement(getCurrentPath()), method, packageSeeds);
                        return super.visitNewClass(created, unused);
                    }
                    @Override public Void visitMethodInvocation(MethodInvocationTree call, Void unused) {
                        Element called = trees.getElement(getCurrentPath());
                        TypeMirror callType = trees.getTypeMirror(getCurrentPath());
                        if (called == null || callType != null && callType.getKind() == TypeKind.ERROR) graphCutoffs.add(pkg);
                        if (called instanceof ExecutableElement callee
                                && owner.sourceForType(callee.getEnclosingElement()) != null) {
                            packageCallers.computeIfAbsent(callee,
                                    ignored -> Collections.newSetFromMap(new IdentityHashMap<>())).add(method);
                        }
                        return super.visitMethodInvocation(call, unused);
                    }
                }.scan(new TreePath(source.path(), methodTree), null);
            }
        }
    }

    private void directReference(Element element, ExecutableElement method,
                                 Map<TypeElement, Set<ExecutableElement>> seeds) {
        if (element instanceof TypeElement type) {
            if (owner.sourcesByType.containsKey(type)) {
                seeds.computeIfAbsent(type, ignored -> Collections.newSetFromMap(new IdentityHashMap<>())).add(method);
            }
        } else if (element instanceof VariableElement variable) {
            Set<TypeElement> types = newIdentitySet();
            mentionedTypes(variable.asType(), types);
            for (TypeElement type : types) {
                if (owner.sourcesByType.containsKey(type)) {
                    seeds.computeIfAbsent(type, ignored -> Collections.newSetFromMap(new IdentityHashMap<>())).add(method);
                }
            }
        }
    }

    private Set<TypeElement> newIdentitySet() {
        return Collections.newSetFromMap(new IdentityHashMap<>());
    }

    private void mentionedTypes(TypeMirror type, Set<TypeElement> result) {
        if (type == null) return;
        if (type.getKind() == TypeKind.ARRAY) {
            mentionedTypes(((ArrayType) type).getComponentType(), result);
            return;
        }
        if (!(type instanceof DeclaredType declared)) return;
        if (declared.asElement() instanceof TypeElement element) result.add(element);
        for (TypeMirror argument : declared.getTypeArguments()) mentionedTypes(argument, result);
    }

    boolean directlyReferences(Tree tree, TreePath parent, TypeElement implementation) {
        if (tree == null) return false;
        final boolean[] found = {false};
        new TreePathScanner<Void, Void>() {
            @Override public Void visitIdentifier(IdentifierTree identifier, Void unused) {
                check(getCurrentPath());
                return super.visitIdentifier(identifier, unused);
            }
            @Override public Void visitMemberSelect(MemberSelectTree selected, Void unused) {
                check(getCurrentPath());
                return super.visitMemberSelect(selected, unused);
            }
            @Override public Void visitNewClass(NewClassTree created, Void unused) {
                check(getCurrentPath());
                return super.visitNewClass(created, unused);
            }
            private void check(TreePath path) {
                Element element = trees.getElement(path);
                if (element == implementation
                        || element instanceof TypeElement type && type == implementation
                        || element instanceof VariableElement variable
                        && mentions(variable.asType(), implementation)) found[0] = true;
            }
        }.scan(new TreePath(parent, tree), null);
        return found[0];
    }

    private boolean mentions(TypeMirror type, TypeElement implementation) {
        if (type == null) return false;
        if (type.getKind() == TypeKind.ARRAY) return mentions(((ArrayType) type).getComponentType(), implementation);
        if (!(type instanceof DeclaredType declared)) return false;
        if (declared.asElement() == implementation) return true;
        return declared.getTypeArguments().stream().anyMatch(argument -> mentions(argument, implementation));
    }
}
