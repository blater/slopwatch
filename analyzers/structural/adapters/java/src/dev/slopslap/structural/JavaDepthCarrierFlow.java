package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.VariableElement;
import java.util.List;
import java.util.Map;

/** Builds normalized flow bodies for the validated immutable-carrier proof. */
final class JavaDepthCarrierFlow {
    private JavaDepthCarrierFlow() { }

    record ConstructorFlow(Map<String, Object> function, List<Map<String, Object>> initialFields) { }

    static ConstructorFlow constructorFlow(Trees trees, TreePath ownerPath, ExecutableElement constructor,
                                           List<VariableElement> fields, String id) {
        TreePath methodPath = methodPath(trees, ownerPath, constructor);
        return new JavaDepthCarrierLowering(new JavaDepthFunction(trees, methodPath))
                .constructor(id, constructor, fields);
    }

    static Map<String, Object> accessor(Trees trees, TreePath ownerPath, ExecutableElement getter, String id) {
        TreePath methodPath = methodPath(trees, ownerPath, getter);
        return new JavaDepthCarrierLowering(new JavaDepthFunction(trees, methodPath))
                .accessor(id, getter, getterField(trees, methodPath));
    }

    private static VariableElement getterField(Trees trees, TreePath methodPath) {
        if (!(methodPath.getLeaf() instanceof MethodTree method) || method.getBody() == null
                || method.getBody().getStatements().size() != 1
                || !(method.getBody().getStatements().get(0) instanceof ReturnTree returned)) return null;
        ExpressionTree expression = returned.getExpression();
        Element element = trees.getElement(TreePath.getPath(methodPath, expression));
        return element instanceof VariableElement field ? field : null;
    }

    private static TreePath methodPath(Trees trees, TreePath ownerPath, ExecutableElement target) {
        if (!(ownerPath.getLeaf() instanceof ClassTree declaration)) return ownerPath;
        for (var member : declaration.getMembers()) {
            Element element = trees.getElement(new TreePath(ownerPath, member));
            if (element == target) return new TreePath(ownerPath, member);
        }
        return ownerPath;
    }
}
