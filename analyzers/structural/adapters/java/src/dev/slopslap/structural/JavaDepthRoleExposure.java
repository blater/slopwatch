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

final class JavaDepthRoleExposure {
    private static final int MAX_REFERENCE_METHODS = JavaDepthRoles.MAX_REFERENCE_METHODS;
    private final JavaDepthRoles owner;
    private final Trees trees;
    private final Elements elements;
    private final Types types;
    private final List<JavaDepthRoles.SourceType> sources;
    private final JavaDepthRoleReachability reachability;

    JavaDepthRoleExposure(JavaDepthRoles owner) {
        this.owner = owner;
        this.trees = owner.trees;
        this.elements = owner.elements;
        this.types = owner.types;
        this.sources = owner.sources;
        this.reachability = new JavaDepthRoleReachability(owner);
        this.publicSurface = new JavaDepthRolePublicSurface(owner);
    }

    private final JavaDepthRolePublicSurface publicSurface;

    void inspectPublicExposure(JavaDepthRoles.SourceType source, TypeElement implementation,
                                       List<String> routes, List<TypeElement> contracts,
                                       Set<ExecutableElement> exposedMethods) {
        publicSurface.inspect(source, implementation, routes, contracts, exposedMethods);
    }

    Set<ExecutableElement> candidateExposureMethods(TypeElement implementation,
                                                             List<TypeElement> contracts) {
        return reachability.candidateExposureMethods(implementation, contracts);
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

    private JavaDepthRoles.SourceType sourceForType(Element element) {
        if (!(element instanceof TypeElement type)) return null;
        for (JavaDepthRoles.SourceType source : sources) {
            if (source.element().equals(type)) return source;
        }
        return null;
    }

    private MethodTree methodTree(JavaDepthRoles.SourceType source, ExecutableElement expected) {
        for (Tree member : source.tree().getMembers()) {
            if (member instanceof MethodTree method
                    && trees.getElement(new TreePath(source.path(), method)) == expected) return method;
        }
        return null;
    }

    private static void addRoute(List<String> routes, String route) {
        if (!routes.contains(route)) routes.add(route);
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

    private static String methodID(ExecutableElement method) {
        return JavaDepthRoles.methodID(method);
    }

    private static String fieldID(VariableElement field) {
        return JavaDepthRoles.fieldID(field);
    }

    private boolean publiclyAccessible(TypeElement type) {
        return owner.publiclyAccessible(type);
    }

    private boolean samePackage(TypeElement left, TypeElement right) {
        return owner.samePackage(left, right);
    }


}