package dev.slopslap.structural;

import com.sun.source.tree.ClassTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.element.VariableElement;
import javax.lang.model.type.TypeMirror;
import java.util.ArrayList;
import java.util.List;

/** Proof for a scalar immutable carrier whose constructors expose validation guards. */
final class JavaDepthValidatedCarrier {
    private JavaDepthValidatedCarrier() { }

    record Proof(List<VariableElement> fields, List<ExecutableElement> constructors,
                 List<ExecutableElement> accessors) {
        Proof {
            fields = List.copyOf(fields);
            constructors = List.copyOf(constructors);
            accessors = List.copyOf(accessors);
        }
    }

    static Proof inspect(Trees trees, TreePath ownerPath, TypeElement owner) {
        if (!JavaDepthCarrier.eligibleType(owner) || !(ownerPath.getLeaf() instanceof ClassTree declaration)) return null;
        JavaDepthCarrier.Inspection inspection = new JavaDepthCarrier.Inspection(trees, ownerPath, owner, declaration);
        if (!JavaDepthCarrierMembers.index(inspection) || !scalarFields(inspection)
                || !JavaDepthCarrierFields.check(inspection)
                || !JavaDepthValidatedCarrierConstructors.check(inspection)
                || !inspection.hasVisibleGuard
                || !JavaDepthCarrierAccessors.check(inspection)
                || !observableFields(inspection)) return null;
        List<VariableElement> fields = new ArrayList<>();
        for (VariableElement field : inspection.fields) {
            if (!field.getModifiers().contains(Modifier.STATIC)) fields.add(field);
        }
        return new Proof(fields, inspection.constructors, inspection.accessors);
    }

    static boolean hasRejection(Trees trees, TreePath ownerPath, ExecutableElement constructor) {
        if (!(ownerPath.getLeaf() instanceof ClassTree declaration)) return false;
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof MethodTree method)) continue;
            if (trees.getElement(new TreePath(ownerPath, method)) == constructor) {
                return JavaDepthValidatedCarrierConstructors.hasGuard(method);
            }
        }
        return false;
    }

    private static boolean scalarFields(JavaDepthCarrier.Inspection inspection) {
        boolean instance = false;
        for (VariableElement field : inspection.fields) {
            if (field.getModifiers().contains(Modifier.STATIC)) continue;
            instance = true;
            TypeMirror type = field.asType();
            if (!scalar(type)) return false;
        }
        return instance;
    }

    private static boolean scalar(TypeMirror type) {
        if (type == null) return false;
        return type.getKind() == javax.lang.model.type.TypeKind.INT
                || type.getKind() == javax.lang.model.type.TypeKind.LONG
                || type.getKind() == javax.lang.model.type.TypeKind.BOOLEAN;
    }

    private static boolean observableFields(JavaDepthCarrier.Inspection inspection) {
        for (VariableElement field : inspection.fields) {
            if (field.getModifiers().contains(Modifier.STATIC)) continue;
            boolean seen = false;
            for (ExecutableElement accessor : inspection.accessors) {
                MethodTree tree = (MethodTree) inspection.treesByElement.get(accessor);
                if (tree == null || tree.getBody() == null || tree.getBody().getStatements().size() != 1
                        || !(tree.getBody().getStatements().get(0) instanceof ReturnTree returned)
                        || returned.getExpression() == null) continue;
                Element target = treesFor(inspection, accessor, returned.getExpression());
                if (target == field) { seen = true; break; }
            }
            if (!seen) return false;
        }
        return true;
    }

    private static Element treesFor(JavaDepthCarrier.Inspection inspection, ExecutableElement accessor, Tree tree) {
        return inspection.trees.getElement(TreePath.getPath(inspection.methodPath(accessor), tree));
    }
}
