package dev.slopslap.structural;

import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.StatementTree;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.VariableElement;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Set;

final class JavaDepthValidatedCarrierConstructorBody {
    private JavaDepthValidatedCarrierConstructorBody() { }

    static boolean check(JavaDepthCarrier.Inspection inspection, ExecutableElement constructor) {
        if (!signature(constructor)) return false;
        MethodTree tree = (MethodTree) inspection.treesByElement.get(constructor);
        if (tree == null || tree.getBody() == null) return false;
        Set<VariableElement> initialized = initialFields(inspection);
        List<? extends StatementTree> statements = tree.getBody().getStatements();
        int start = JavaDepthCarrierConstructorValues.superIndex(inspection, constructor, statements);
        if (start < 0 || statements.size() > inspection.fields.size() + 257) return false;
        boolean assignments = false;
        boolean guarded = false;
        for (int index = start; index < statements.size(); index++) {
            StatementTree statement = statements.get(index);
            if (!assignments && JavaDepthValidatedCarrierConstructorGuards.isGuard(statement)) {
                guarded = true;
            } else {
                assignments = true;
                if (!(statement instanceof ExpressionStatementTree expression)
                        || !(expression.getExpression() instanceof AssignmentTree assignment)
                        || !JavaDepthValidatedCarrierConstructorAssignments.initialize(inspection, constructor, assignment, initialized)) {
                    return false;
                }
            }
        }
        inspection.hasGuard |= guarded;
        inspection.hasVisibleGuard |= guarded
                && JavaDepthCarrierMembers.exposed(inspection, constructor);
        return allInitialized(inspection, initialized);
    }

    private static boolean signature(ExecutableElement constructor) {
        if (!constructor.getTypeParameters().isEmpty() || constructor.isVarArgs()
                || !constructor.getThrownTypes().isEmpty()) return false;
        for (VariableElement parameter : constructor.getParameters()) {
            if (!scalar(parameter.asType())) return false;
        }
        return true;
    }

    private static boolean scalar(javax.lang.model.type.TypeMirror type) {
        if (type == null) return false;
        return type.getKind() == javax.lang.model.type.TypeKind.INT
                || type.getKind() == javax.lang.model.type.TypeKind.LONG
                || type.getKind() == javax.lang.model.type.TypeKind.BOOLEAN;
    }

    private static Set<VariableElement> initialFields(JavaDepthCarrier.Inspection inspection) {
        Set<VariableElement> initialized = Collections.newSetFromMap(new IdentityHashMap<>());
        for (VariableElement field : inspection.fields) {
            if (!field.getModifiers().contains(Modifier.STATIC)
                    && JavaDepthCarrierFields.hasInitializer(inspection, field)) initialized.add(field);
        }
        return initialized;
    }

    private static boolean allInitialized(JavaDepthCarrier.Inspection inspection,
                                          Set<VariableElement> initialized) {
        for (VariableElement field : inspection.fields) {
            if (!field.getModifiers().contains(Modifier.STATIC) && !initialized.contains(field)) return false;
        }
        return true;
    }
}
