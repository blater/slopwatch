package dev.slopslap.structural;

import com.sun.source.tree.AssignmentTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.VariableElement;
import java.util.Set;

final class JavaDepthValidatedCarrierConstructorAssignments {
    private JavaDepthValidatedCarrierConstructorAssignments() { }

    static boolean initialize(JavaDepthCarrier.Inspection inspection, ExecutableElement constructor,
                              AssignmentTree assignment, Set<VariableElement> initialized) {
        return directTarget(assignment.getVariable())
                && JavaDepthCarrierConstructorValues.initialize(inspection, constructor, assignment, initialized);
    }

    private static boolean directTarget(ExpressionTree target) {
        if (target instanceof IdentifierTree) return true;
        return target instanceof MemberSelectTree member
                && member.getExpression() instanceof IdentifierTree receiver
                && receiver.getName().contentEquals("this");
    }

}
