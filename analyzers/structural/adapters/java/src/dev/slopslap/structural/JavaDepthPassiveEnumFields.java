package dev.slopslap.structural;

import com.sun.source.tree.MemberSelectTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;

/** Field shape checks for passive enums. */
final class JavaDepthPassiveEnumFields {
    private JavaDepthPassiveEnumFields() { }
    static boolean staticField(Trees trees, VariableElement field) {
        return field.getModifiers().contains(Modifier.FINAL)
                && (field.getConstantValue() != null || valuesLengthField(trees, field));
    }
    static boolean metadataField(VariableElement field) {
        return field.getModifiers().contains(Modifier.PRIVATE) && field.getModifiers().contains(Modifier.FINAL)
                && !field.getModifiers().contains(Modifier.VOLATILE);
    }
    private static boolean valuesLengthField(Trees trees, VariableElement field) {
        TreePath fieldPath = trees.getPath(field);
        if (fieldPath == null || !(fieldPath.getLeaf() instanceof VariableTree variable)
                || !(variable.getInitializer() instanceof MemberSelectTree length)
                || !length.getIdentifier().contentEquals("length")
                || !(length.getExpression() instanceof MethodInvocationTree values)
                || !values.getArguments().isEmpty()) return false;
        Element target = trees.getElement(TreePath.getPath(fieldPath, values));
        return target instanceof ExecutableElement method && method.getSimpleName().contentEquals("values")
                && method.getEnclosingElement() == field.getEnclosingElement();
    }
}
