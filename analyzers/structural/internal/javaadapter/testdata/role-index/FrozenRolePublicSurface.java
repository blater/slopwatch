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

final class FrozenRolePublicSurface {
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Elements elements;
    private final Types types;
    private final FrozenRoleReachability reachability;

    FrozenRolePublicSurface(JavaDepthRoles owner) {
        this.owner=owner;
        this.trees=owner.trees;
        this.elements=owner.elements;
        this.types=owner.types;
        this.reachability=new FrozenRoleReachability(owner);
    }

    void inspect(JavaDepthRoles.SourceType source, TypeElement implementation,
                                       List<String> routes, List<TypeElement> contracts,
                                       Set<ExecutableElement> exposedMethods) {
        if (!publiclyAccessible(source.element())) return;
        // getAllMembers includes public methods inherited through a package-private
        // base. Those methods are still callable through this public type.
        for (Element item : elements.getAllMembers(source.element())) {
            if (!(item instanceof ExecutableElement method) || !method.getModifiers().contains(Modifier.PUBLIC)) continue;
            if (method.getEnclosingElement() instanceof TypeElement declaringType
                    && declaringType.getQualifiedName().contentEquals("java.lang.Object")) continue;
            if (JavaDepthRoleTypes.hasErrorType(method)) owner.exposureCutoff = true;
            boolean bodyExposes = exposedMethods.contains(method);
            if (mentions(method, implementation, contracts) || bodyExposes) addRoute(routes, methodID(method));
        }
        for (Tree member : source.tree().getMembers()) {
            Element element = trees.getElement(new TreePath(source.path(), member));
            if (member instanceof MethodTree methodTree && element instanceof ExecutableElement method
                    && method.getModifiers().contains(Modifier.PUBLIC)) {
                if (mentions(method, implementation, contracts)
                        || exposedMethods.contains(method)) {
                    addRoute(routes, methodID(method));
                }
            } else if (member instanceof VariableTree variable && element instanceof VariableElement field
                    && field.getModifiers().contains(Modifier.PUBLIC)) {
                if (JavaDepthRoleTypes.hasErrorType(field.asType())) {
                    owner.exposureCutoff = true;
                } else if (mentions(field.asType(), implementation, contracts)
                        || creates(variable.getInitializer(), source.path(), implementation, contracts)
                        || reachability.directlyReferences(variable.getInitializer(), source.path(), implementation)) {
                    addRoute(routes, fieldID(field));
                }
            }
        }
    }


    private boolean mentions(ExecutableElement method, TypeElement implementation, List<TypeElement> contracts) {
        if (mentions(method.getReturnType(), implementation, contracts)) return true;
        return method.getParameters().stream().anyMatch(parameter -> mentions(parameter.asType(), implementation, contracts));
    }

    private boolean mentions(TypeMirror type, TypeElement implementation, List<TypeElement> contracts) {
        if (type == null) return false;
        if (type.getKind() == TypeKind.ARRAY) return mentions(((ArrayType) type).getComponentType(), implementation, contracts);
        if (!(type instanceof DeclaredType declared)) return false;
        Element element = declared.asElement();
        if (element == implementation || contracts.contains(element)) return true;
        return declared.getTypeArguments().stream().anyMatch(argument -> mentions(argument, implementation, contracts));
    }

    private boolean creates(Tree tree, TreePath parent, TypeElement implementation, List<TypeElement> contracts) {
        if (tree == null) return false;
        final boolean[] found = {false};
        new TreePathScanner<Void, Void>() {
            @Override public Void visitNewClass(NewClassTree created, Void unused) {
                Element type = types.asElement(trees.getTypeMirror(getCurrentPath()));
                if (type == implementation || contracts.contains(type)) found[0] = true;
                return super.visitNewClass(created, unused);
            }
        }.scan(new TreePath(parent, tree), null);
        return found[0];
    }

    private static void addRoute(List<String> routes, String route) {
        if (!routes.contains(route)) routes.add(route);
    }

    private static String methodID(ExecutableElement method) {
        return JavaDepthRoles.methodID(method);
    }

    private static String fieldID(VariableElement field) {
        return JavaDepthRoles.fieldID(field);
    }

    private boolean publiclyAccessible(TypeElement type) {
        return owner.publiclyAccessible(type);
    }
}
