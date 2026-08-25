package dev.slopslap.structural;

import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MemberSelectTree;
import com.sun.source.util.TreeScanner;

import java.util.Set;

final class JavaTypeNames extends TreeScanner<Void, Void> {
    private final Set<String> output;

    JavaTypeNames(Set<String> output) {
        this.output = output;
    }

    @Override
    public Void visitIdentifier(IdentifierTree node, Void unused) {
        output.add(node.getName().toString());
        return null;
    }

    @Override
    public Void visitMemberSelect(MemberSelectTree node, Void unused) {
        output.add(node.toString());
        return null;
    }
}
