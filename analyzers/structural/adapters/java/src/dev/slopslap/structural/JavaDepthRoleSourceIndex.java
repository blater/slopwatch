package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

final class JavaDepthRoleSourceIndex {
    private JavaDepthRoleSourceIndex() { }

    static void collectTypes(Trees trees, CompilationUnitTree unit,
                             List<? extends Tree> declarations, TreePath parent,
                             String file, List<JavaDepthRoles.SourceType> output) {
        for (Tree declaration : declarations) {
            if (!(declaration instanceof ClassTree tree)) continue;
            TreePath path = parent == null ? new TreePath(new TreePath(unit), tree) : new TreePath(parent, tree);
            Element element = trees.getElement(path);
            if (element instanceof TypeElement type) {
                output.add(new JavaDepthRoles.SourceType(type, tree, path, unit, file));
                collectTypes(trees, unit, tree.getMembers(), path, file, output);
            }
        }
    }

    static void indexFields(JavaDepthRoles owner, JavaDepthRoles.SourceType source) {
        for (Tree member : source.tree().getMembers()) {
            if (!(member instanceof VariableTree variable)) continue;
            Element element = owner.trees.getElement(new TreePath(source.path(), variable));
            if (element instanceof VariableElement field) {
                owner.fieldTrees.put(field, variable);
                owner.fieldPaths.put(field, new TreePath(source.path(), variable));
            }
        }
    }

    static void indexConsumers(JavaDepthRoles owner, JavaDepthRoles.SourceType source) {
        Set<String> seen = new HashSet<>();
        for (Tree member : source.tree().getMembers()) {
            if (!(member instanceof MethodTree constructorTree)) continue;
            Element element = owner.trees.getElement(new TreePath(source.path(), constructorTree));
            if (!(element instanceof ExecutableElement constructor)
                    || constructor.getKind() != ElementKind.CONSTRUCTOR) continue;
            for (VariableElement parameter : constructor.getParameters()) {
                String key = JavaDepthRoles.typeKey(owner.types.erasure(parameter.asType()));
                if (seen.add(key)) owner.consumersByContract
                        .computeIfAbsent(key, ignored -> new java.util.ArrayList<>()).add(source);
            }
        }
    }
}
