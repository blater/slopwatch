package dev.slopslap.structural;

import com.sun.source.tree.BlockTree;
import com.sun.source.tree.IfTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.ThrowTree;

final class JavaDepthValidatedCarrierConstructorGuards {
    private JavaDepthValidatedCarrierConstructorGuards() { }

    static boolean isGuard(StatementTree statement) {
        if (!(statement instanceof IfTree branch) || branch.getElseStatement() != null) return false;
        StatementTree body = branch.getThenStatement();
        if (body instanceof BlockTree block) {
            return block.getStatements().size() == 1 && block.getStatements().get(0) instanceof ThrowTree;
        }
        return body instanceof ThrowTree;
    }
}
