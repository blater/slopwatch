package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.TypeElement;
import java.util.ArrayList;
import java.util.List;

/** Computes forwarding route parameter slots after body classification. */
final class JavaDepthRouteProjection {
    private JavaDepthRouteProjection() { }
    static JavaDepthRouteBodyClassifier.Normalized normalize(Trees trees, TreePath ownerPath,
                                                             TypeElement owner, ExecutableElement method) {
        ExecutableElement target = JavaDepthRouteTarget.forwardedTarget(trees, ownerPath, owner, method);
        if (target == null) return self(method);
        String serviceID = JavaDepthRoles.methodID(target);
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree) || tree.getBody() == null) return self(method);
        ReturnTree returned = JavaDepthRouteTarget.returnedCall(trees, methodPath, method, tree.getBody());
        if (returned == null || !(returned.getExpression() instanceof MethodInvocationTree call)
                || call.getArguments().size() != target.getParameters().size()) return self(method);
        boolean[] used = new boolean[method.getParameters().size()];
        List<String> required = new ArrayList<>(), exposed = new ArrayList<>();
        for (int index = 0; index < target.getParameters().size(); index++) {
            ExpressionTree argument = call.getArguments().get(index);
            Integer dependency = JavaDepthRouteSlots.parameterIndex(trees, methodPath, method, argument);
            if (dependency == null) {
                if (!JavaDepthRouteSlots.typedLiteral(trees, methodPath, argument, target.getParameters().get(index).asType())) return self(method);
                continue;
            }
            if (!sameType(method.getParameters().get(dependency).asType(), target.getParameters().get(index).asType())
                    || !JavaDepthRouteSlots.markOnce(used, dependency)) return self(method);
            String slot = serviceID + "/arg" + index;
            exposed.add(slot); required.add(slot);
        }
        for (boolean value : used) if (!value) return self(method);
        return new JavaDepthRouteBodyClassifier.Normalized(serviceID, required, exposed);
    }
    private static JavaDepthRouteBodyClassifier.Normalized self(ExecutableElement method) {
        String id = JavaDepthRoles.methodID(method); List<String> slots = new ArrayList<>();
        for (int index = 0; index < method.getParameters().size(); index++) slots.add(id + "/arg" + index);
        return new JavaDepthRouteBodyClassifier.Normalized(id, slots, new ArrayList<>(slots));
    }
    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }
}
