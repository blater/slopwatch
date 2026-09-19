package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;

/** Whitelisted enum-constant initializer checks. */
final class JavaDepthPassiveEnumConstants {
    private JavaDepthPassiveEnumConstants() { }
    static boolean arePlain(Trees trees, TreePath ownerPath, ClassTree declaration) {
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof VariableTree variable)) {
                if (member instanceof MethodTree || member.getKind() == Tree.Kind.EMPTY_STATEMENT) continue;
                return false;
            }
            Element element = trees.getElement(new TreePath(ownerPath, variable));
            if (!(element instanceof VariableElement variableElement)) return false;
            if (variableElement.getKind() != ElementKind.ENUM_CONSTANT) continue;
            if (variable.getInitializer() instanceof NewClassTree created && created.getClassBody() != null) return false;
            if (variable.getInitializer() != null
                    && !allowed(trees, TreePath.getPath(ownerPath, variable), variable.getInitializer())) return false;
        }
        return true;
    }
    private static boolean allowed(Trees trees, TreePath parent, Tree expression) {
        if (expression instanceof NewClassTree created) {
            if (created.getClassBody() != null) return false;
            for (Tree argument : created.getArguments()) {
                if (!value(trees, TreePath.getPath(parent, created), argument)) return false;
            }
            return true;
        }
        return value(trees, parent, expression);
    }
    private static boolean value(Trees trees, TreePath parent, Tree expression) {
        if (expression instanceof LiteralTree) return true;
        Element element = trees.getElement(TreePath.getPath(parent, expression));
        if (element instanceof VariableElement variable) {
            return variable.getKind() == ElementKind.ENUM_CONSTANT
                    || variable.getModifiers().contains(Modifier.STATIC)
                    && variable.getModifiers().contains(Modifier.FINAL)
                    && variable.getConstantValue() != null;
        }
        return false;
    }
}
