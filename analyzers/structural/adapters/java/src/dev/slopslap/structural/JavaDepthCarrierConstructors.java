package dev.slopslap.structural;

import com.sun.source.tree.MethodTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.ExpressionStatementTree;
import com.sun.source.tree.AssignmentTree;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.VariableElement;
import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.List;
import java.util.Set;

final class JavaDepthCarrierConstructors {
    private JavaDepthCarrierConstructors() { }

    static boolean check(JavaDepthCarrier.Inspection inspection) {
        if (inspection.constructors.isEmpty()) return JavaDepthCarrierFields.allInitialized(inspection);
        for (ExecutableElement constructor : inspection.constructors) {
            if (!validSignature(constructor) || !body(inspection, constructor)) return false;
        }
        return true;
    }

    private static boolean validSignature(ExecutableElement constructor) {
        return constructor.getTypeParameters().isEmpty() && !constructor.isVarArgs()
                && constructor.getThrownTypes().isEmpty();
    }

    private static boolean body(JavaDepthCarrier.Inspection inspection, ExecutableElement constructor) {
        MethodTree tree = (MethodTree) inspection.treesByElement.get(constructor);
        Set<VariableElement> initialized = Collections.newSetFromMap(new IdentityHashMap<>());
        for (VariableElement field : inspection.fields) {
            if (!field.getModifiers().contains(Modifier.STATIC)
                    && JavaDepthCarrierFields.hasInitializer(inspection, field)) initialized.add(field);
        }
        List<? extends StatementTree> statements = tree.getBody() == null
                ? List.of() : tree.getBody().getStatements();
        if (statements.size() > inspection.fields.size() + 1) return false;
        int start = JavaDepthCarrierConstructorValues.superIndex(inspection, constructor, statements);
        if (start < 0) return false;
        for (int index = start; index < statements.size(); index++) {
            if (!(statements.get(index) instanceof ExpressionStatementTree expression)
                    || !(expression.getExpression() instanceof AssignmentTree assignment)
                    || !JavaDepthCarrierConstructorValues.initialize(inspection, constructor, assignment, initialized)) {
                return false;
            }
        }
        return allInitialized(inspection, initialized);
    }

    private static boolean allInitialized(JavaDepthCarrier.Inspection inspection,
                                          Set<VariableElement> initialized) {
        for (VariableElement field : inspection.fields) {
            if (!field.getModifiers().contains(Modifier.STATIC) && !initialized.contains(field)) return false;
        }
        return true;
    }
}
