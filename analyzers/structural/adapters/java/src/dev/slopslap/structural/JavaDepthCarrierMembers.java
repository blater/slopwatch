package dev.slopslap.structural;

import com.sun.source.tree.BlockTree;
import com.sun.source.tree.ClassTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.VariableTree;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.VariableElement;
import java.util.ArrayList;
import java.util.List;

final class JavaDepthCarrierMembers {
    private JavaDepthCarrierMembers() { }

    static boolean index(JavaDepthCarrier.Inspection inspection) {
        if (inspection.declaration.getMembers().size() > 256) return false;
        for (Tree member : inspection.declaration.getMembers()) {
            if (member instanceof VariableTree variable) {
                if (!indexField(inspection, variable)) return false;
            } else if (member instanceof MethodTree method) {
                if (!indexMethod(inspection, method)) return false;
            } else if (member instanceof BlockTree || member instanceof ClassTree) {
                return false;
            } else {
                return false;
            }
        }
        return true;
    }

    private static boolean indexField(JavaDepthCarrier.Inspection inspection, VariableTree tree) {
        VariableElement field = inspection.element(tree, VariableElement.class);
        if (field == null || field.getKind() != ElementKind.FIELD) return false;
        inspection.fields.add(field);
        inspection.treesByElement.put(field, tree);
        return true;
    }

    private static boolean indexMethod(JavaDepthCarrier.Inspection inspection, MethodTree tree) {
        ExecutableElement method = inspection.element(tree, ExecutableElement.class);
        if (method == null) return false;
        inspection.treesByElement.put(method, tree);
        if (method.getKind() == ElementKind.CONSTRUCTOR) inspection.constructors.add(method);
        return true;
    }

    static boolean visibleConstructor(JavaDepthCarrier.Inspection inspection) {
        if (inspection.constructors.isEmpty()) return !privateOwner(inspection);
        for (ExecutableElement constructor : inspection.constructors) {
            if (exposed(inspection, constructor)) return true;
        }
        return false;
    }

    static boolean exposed(JavaDepthCarrier.Inspection inspection, ExecutableElement method) {
        return inspection.owner.getModifiers().contains(Modifier.PUBLIC)
                ? method.getModifiers().contains(Modifier.PUBLIC)
                : !method.getModifiers().contains(Modifier.PRIVATE);
    }

    private static boolean privateOwner(JavaDepthCarrier.Inspection inspection) {
        return inspection.owner.getModifiers().contains(Modifier.PRIVATE);
    }

    static List<ExecutableElement> methods(JavaDepthCarrier.Inspection inspection) {
        List<ExecutableElement> methods = new ArrayList<>();
        for (var element : inspection.owner.getEnclosedElements()) {
            if (element instanceof ExecutableElement method) methods.add(method);
        }
        return methods;
    }
}
