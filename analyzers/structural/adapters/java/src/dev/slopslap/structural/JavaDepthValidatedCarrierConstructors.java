package dev.slopslap.structural;

import com.sun.source.tree.MethodTree;
import com.sun.source.tree.StatementTree;
import javax.lang.model.element.ExecutableElement;

final class JavaDepthValidatedCarrierConstructors {
    private JavaDepthValidatedCarrierConstructors() { }

    static boolean check(JavaDepthCarrier.Inspection inspection) {
        for (ExecutableElement constructor : inspection.constructors) {
            if (!JavaDepthValidatedCarrierConstructorBody.check(inspection, constructor)) return false;
        }
        return true;
    }

    static boolean hasGuard(MethodTree tree) {
        if (tree == null || tree.getBody() == null) return false;
        for (StatementTree statement : tree.getBody().getStatements()) {
            if (JavaDepthValidatedCarrierConstructorGuards.isGuard(statement)) return true;
        }
        return false;
    }
}
