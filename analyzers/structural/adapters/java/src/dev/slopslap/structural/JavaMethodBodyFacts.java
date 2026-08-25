package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreeScanner;

import java.util.*;

final class JavaMethodBodyFacts extends TreeScanner<Void, Void> {
    private final Set<String> ownFields;
    private final Set<String> typeNames;
    private final List<ClassTree> nestedClasses;
    private final Set<String> locals = new HashSet<>();
    final SortedSet<String> own = new TreeSet<>();
    final SortedSet<String> foreign = new TreeSet<>();
    private boolean collectBodyTypes = true;
    private int classDepth;

    JavaMethodBodyFacts(
            Set<String> fields, List<? extends VariableTree> parameters, Set<String> typeNames,
            List<ClassTree> nestedClasses
    ) {
        ownFields = fields;
        this.typeNames = typeNames;
        this.nestedClasses = nestedClasses;
        parameters.forEach(value -> locals.add(value.getName().toString()));
    }

    private void type(Tree value) {
        if (collectBodyTypes && value != null) {
            new JavaTypeNames(typeNames).scan(value, null);
        }
    }

    @Override
    public Void visitVariable(VariableTree node, Void unused) {
        type(node.getType());
        if (node.getInitializer() != null) scan(node.getInitializer(), unused);
        locals.add(node.getName().toString());
        return null;
    }

    @Override
    public Void visitIdentifier(IdentifierTree node, Void unused) {
        String name = node.getName().toString();
        if (ownFields.contains(name) && !locals.contains(name)) own.add(name);
        return null;
    }

    @Override
    public Void visitClass(ClassTree node, Void unused) {
        if (classDepth == 0) nestedClasses.add(node);
        classDepth++;
        try {
            return super.visitClass(node, unused);
        } finally {
            classDepth--;
        }
    }

    @Override
    public Void visitMemberSelect(MemberSelectTree node, Void unused) {
        String base = node.getExpression().toString();
        String field = node.getIdentifier().toString();
        if ((base.equals("this") || base.endsWith(".this")) && ownFields.contains(field)) {
            own.add(field);
        } else if (!base.equals("this")) {
            foreign.add(base + "." + field);
        }
        scan(node.getExpression(), unused);
        return null;
    }

    @Override
    public Void visitNewClass(NewClassTree node, Void unused) {
        type(node.getIdentifier());
        node.getTypeArguments().forEach(this::type);
        scan(node.getEnclosingExpression(), unused);
        node.getTypeArguments().forEach(value -> scan(value, unused));
        scan(node.getIdentifier(), unused);
        node.getArguments().forEach(value -> scan(value, unused));
        scanClassBodyWithoutTypes(node, unused);
        return null;
    }

    private void scanClassBodyWithoutTypes(NewClassTree node, Void unused) {
        if (node.getClassBody() == null) return;
        boolean previous = collectBodyTypes;
        collectBodyTypes = false;
        try {
            scan(node.getClassBody(), unused);
        } finally {
            collectBodyTypes = previous;
        }
    }

    @Override
    public Void visitNewArray(NewArrayTree node, Void unused) {
        type(node.getType());
        scan(node.getType(), unused);
        node.getDimensions().forEach(value -> scan(value, unused));
        if (node.getInitializers() != null) node.getInitializers().forEach(value -> scan(value, unused));
        return null;
    }

    @Override
    public Void visitTypeCast(TypeCastTree node, Void unused) {
        type(node.getType());
        scan(node.getType(), unused);
        return scan(node.getExpression(), unused);
    }

    @Override
    public Void visitInstanceOf(InstanceOfTree node, Void unused) {
        type(node.getType());
        scan(node.getType(), unused);
        scan(node.getExpression(), unused);
        return scan(node.getPattern(), unused);
    }

    @Override
    public Void visitAnnotation(AnnotationTree node, Void unused) {
        type(node.getAnnotationType());
        scan(node.getAnnotationType(), unused);
        node.getArguments().forEach(value -> scan(value, unused));
        return null;
    }

    @Override
    public Void visitMethodInvocation(MethodInvocationTree node, Void unused) {
        node.getTypeArguments().forEach(this::type);
        if (node.getMethodSelect() instanceof MemberSelectTree selected) scan(selected.getExpression(), unused);
        node.getArguments().forEach(value -> scan(value, unused));
        return null;
    }

    @Override
    public Void visitMemberReference(MemberReferenceTree node, Void unused) {
        if (node.getTypeArguments() != null) node.getTypeArguments().forEach(this::type);
        scan(node.getQualifierExpression(), unused);
        return null;
    }
}
