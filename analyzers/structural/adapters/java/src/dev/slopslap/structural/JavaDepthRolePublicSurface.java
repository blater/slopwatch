package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import javax.lang.model.element.*;
import javax.lang.model.type.*;
import java.util.*;

/** Reverse references are built once, never by searching a package per candidate. */
final class JavaDepthRolePublicSurface {
    private final JavaDepthRoles owner;
    private static final class Surface {
        final Map<TypeElement, Set<String>> mentions = new IdentityHashMap<>();
        final Map<TypeElement, Set<String>> direct = new IdentityHashMap<>();
        final Map<ExecutableElement, String> methods = new IdentityHashMap<>();
        boolean incomplete;
    }
    private final Map<String, Surface> packages = new HashMap<>();
    long indexWork, queryWork;

    JavaDepthRolePublicSurface(JavaDepthRoles owner) {
        this.owner = owner;
        for (JavaDepthRoles.SourceType source : owner.sources) index(source);
    }

    List<String> routes(TypeElement implementation, List<TypeElement> contracts,
                        Set<ExecutableElement> exposedMethods) {
        Surface surface = packages.get(JavaDepthRoles.packageName(implementation));
        if (surface == null) return new ArrayList<>();
        if (surface.incomplete) owner.exposureCutoff = true;
        Set<String> result = new HashSet<>();
        add(result, surface.mentions.get(implementation));
        add(result, surface.direct.get(implementation));
        for (TypeElement contract : contracts) add(result, surface.mentions.get(contract));
        for (ExecutableElement method : exposedMethods) {
            queryWork++;
            String route = surface.methods.get(method);
            if (route != null) result.add(route);
        }
        return new ArrayList<>(result);
    }

    private void add(Set<String> result, Set<String> values) {
        queryWork++;
        if (values != null) { queryWork += values.size(); result.addAll(values); }
    }

    private void index(JavaDepthRoles.SourceType source) {
        indexWork++;
        if (!owner.publiclyAccessible(source.element())) return;
        Surface surface = packages.computeIfAbsent(JavaDepthRoles.packageName(source.element()), key -> new Surface());
        // Inherited methods remain callable through a public subclass.
        for (Element item : owner.elements.getAllMembers(source.element())) {
            indexWork++;
            if (!(item instanceof ExecutableElement method) || !method.getModifiers().contains(Modifier.PUBLIC)) continue;
            if (method.getEnclosingElement() instanceof TypeElement type
                    && type.getQualifiedName().contentEquals("java.lang.Object")) continue;
            surface.incomplete |= JavaDepthRoleTypes.hasErrorType(method);
            indexMethod(surface, method);
        }
        for (Tree member : source.tree().getMembers()) {
            indexWork++;
            Element element = owner.trees.getElement(new TreePath(source.path(), member));
            if (member instanceof MethodTree && element instanceof ExecutableElement method
                    && method.getModifiers().contains(Modifier.PUBLIC)) {
                indexMethod(surface, method);
            } else if (member instanceof VariableTree variable && element instanceof VariableElement field
                    && field.getModifiers().contains(Modifier.PUBLIC)) {
                if (JavaDepthRoleTypes.hasErrorType(field.asType())) { surface.incomplete = true; continue; }
                String route = JavaDepthRoles.fieldID(field);
                mention(surface.mentions, field.asType(), route);
                indexInitializer(surface, variable.getInitializer(), source.path(), route);
            }
        }
    }

    private void indexMethod(Surface surface, ExecutableElement method) {
        if (surface.methods.containsKey(method)) return;
        String route = JavaDepthRoles.methodID(method);
        surface.methods.put(method, route);
        mention(surface.mentions, method.getReturnType(), route);
        for (VariableElement parameter : method.getParameters()) mention(surface.mentions, parameter.asType(), route);
    }

    private void mention(Map<TypeElement, Set<String>> index, TypeMirror type, String route) {
        indexWork++;
        if (type instanceof ArrayType array) { mention(index, array.getComponentType(), route); return; }
        if (!(type instanceof DeclaredType declared)) return;
        if (declared.asElement() instanceof TypeElement element) record(index, element, route);
        for (TypeMirror argument : declared.getTypeArguments()) mention(index, argument, route);
    }

    private static void record(Map<TypeElement, Set<String>> index, TypeElement type, String route) {
        index.computeIfAbsent(type, key -> new HashSet<>()).add(route);
    }

    private void indexInitializer(Surface surface, Tree tree, TreePath parent, String route) {
        if (tree == null) return;
        new TreePathScanner<Void, Void>() {
            @Override public Void scan(Tree node, Void unused) {
                if (node != null) indexWork++;
                return super.scan(node, unused);
            }
            @Override public Void visitIdentifier(IdentifierTree node, Void unused) {
                reference(); return super.visitIdentifier(node, unused);
            }
            @Override public Void visitMemberSelect(MemberSelectTree node, Void unused) {
                reference(); return super.visitMemberSelect(node, unused);
            }
            @Override public Void visitNewClass(NewClassTree node, Void unused) {
                Element type = owner.types.asElement(owner.trees.getTypeMirror(getCurrentPath()));
                if (type instanceof TypeElement created) record(surface.mentions, created, route);
                reference(); return super.visitNewClass(node, unused);
            }
            private void reference() {
                Element element = owner.trees.getElement(getCurrentPath());
                if (element instanceof TypeElement type) record(surface.direct, type, route);
                else if (element instanceof VariableElement variable) mention(surface.direct, variable.asType(), route);
            }
        }.scan(new TreePath(parent, tree), null);
    }
}
