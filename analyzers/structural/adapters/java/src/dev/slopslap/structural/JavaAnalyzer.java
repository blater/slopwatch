package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;

import javax.tools.Diagnostic;
import java.util.*;

final class JavaAnalyzer extends TreePathScanner<Void, Void> {
    private static final Set<String> PRIMITIVES = Set.of(
            "boolean", "byte", "char", "double", "float", "int", "long", "short", "void"
    );

    private final Facts.Program program = new Facts.Program();
    private final Trees trees;
    private final SourcePositions positions;
    private CompilationUnitTree unit;
    private String path;

    JavaAnalyzer(Trees trees) {
        this.trees = trees;
        this.positions = trees.getSourcePositions();
    }

    void scanUnit(CompilationUnitTree current, String relative) {
        unit = current;
        path = relative;
        program.files.add(relative);
        scan(current, null);
    }

    void skipUnit(String relative) {
        program.files.add(relative);
    }

    Facts.Program program() {
        return program;
    }

    void order() {
        Comparator<Facts.Location> locations = Comparator.comparing(Facts.Location::path)
                .thenComparingInt(Facts.Location::line).thenComparingInt(Facts.Location::column);
        program.functions.sort(Comparator.comparing(item -> item.location, locations));
        program.types.sort(Comparator.comparing(item -> item.location, locations));
        program.publicOperations.sort(Comparator.comparing(item -> item.location, locations));
        program.files.sort(String::compareTo);
    }

    Facts.Location location(Tree tree) {
        long start = positions.getStartPosition(unit, tree);
        long end = positions.getEndPosition(unit, tree);
        if (start == Diagnostic.NOPOS) {
            start = 0;
        }
        if (end == Diagnostic.NOPOS || end < start) {
            end = start;
        }
        LineMap lines = unit.getLineMap();
        return new Facts.Location(
                path, (int) lines.getLineNumber(start), (int) lines.getColumnNumber(start),
                (int) lines.getLineNumber(end), (int) lines.getColumnNumber(end)
        );
    }

    @Override
    public Void visitClass(ClassTree node, Void unused) {
        String name = node.getSimpleName().toString();
        List<ClassTree> nestedClasses = new ArrayList<>();
        if (!name.isEmpty()) {
            Facts.TypeFact type = new Facts.TypeFact();
            type.name = name;
            type.kind = switch (node.getKind()) {
                case INTERFACE, ANNOTATION_TYPE -> "interface";
                case ENUM -> "enum";
                case RECORD -> "record";
                default -> "class";
            };
            type.location = location(node);
            Set<String> genericNames = new HashSet<>();
            node.getTypeParameters().forEach(value -> genericNames.add(value.getName().toString()));
            Set<String> foreignTypes = new TreeSet<>();
            if (node.getExtendsClause() != null) {
                collectTypes(node.getExtendsClause(), foreignTypes);
            }
            node.getImplementsClause().forEach(value -> collectTypes(value, foreignTypes));
            NestedClasses nested = new NestedClasses(nestedClasses);
            for (Tree member : node.getMembers()) {
                if (member instanceof VariableTree field) {
                    type.fields.add(field.getName().toString());
                    collectTypes(field.getType(), foreignTypes);
                    Facts.FieldFact fieldFact = new Facts.FieldFact();
                    fieldFact.name = field.getName().toString();
                    fieldFact.publicField = field.getModifiers().getFlags().contains(javax.lang.model.element.Modifier.PUBLIC);
                    fieldFact.mutable = !field.getModifiers().getFlags().contains(javax.lang.model.element.Modifier.FINAL);
                    fieldFact.type = shape(field.getType());
                    type.fieldFacts.add(fieldFact);
                    addRepresentationExposure(name, field, fieldFact);
                    nested.scan(field.getInitializer(), null);
                } else if (member instanceof ClassTree child) {
                    nestedClasses.add(child);
                } else if (member instanceof BlockTree block) {
                    nested.scan(block, null);
                }
            }
            Set<String> ownFields = Set.copyOf(type.fields);
            int ordinal = 0;
            for (Tree member : node.getMembers()) {
                if (!(member instanceof MethodTree method)
                        || positions.getStartPosition(unit, method) == Diagnostic.NOPOS) {
                    continue;
                }
                Facts.Function function = function(method, name);
                program.functions.add(function);
                type.methods.add(function);
                if (type.kind.equals("interface") || method.getModifiers().getFlags().contains(javax.lang.model.element.Modifier.PUBLIC)) {
                    program.publicOperations.add(operation(method, name));
                }
                if (type.kind.equals("interface")) {
                    type.interfaceMethodCount++;
                }
                String methodKey = method.getName() + "#" + ordinal++;
                MethodBodyFacts fields = new MethodBodyFacts(
                        ownFields, method.getParameters(), foreignTypes, nestedClasses
                );
                if (method.getBody() != null) {
                    fields.scan(method.getBody(), null);
                }
                type.methodFields.put(methodKey, new ArrayList<>(fields.own));
                type.foreignFields.addAll(fields.foreign);
                collectMethodTypes(method, foreignTypes);
            }
            foreignTypes.remove(name);
            foreignTypes.removeAll(genericNames);
            foreignTypes.removeAll(PRIMITIVES);
            type.foreignTypes.addAll(foreignTypes);
            SortedSet<String> uniqueForeignFields = new TreeSet<>();
            type.foreignFields.stream().map(String::trim).forEach(uniqueForeignFields::add);
            type.foreignFields.clear();
            type.foreignFields.addAll(uniqueForeignFields);
            program.types.add(type);
        } else {
            NestedClasses nested = new NestedClasses(nestedClasses);
            node.getMembers().forEach(member -> nested.scan(member, null));
        }
        nestedClasses.sort(Comparator.comparingLong(item -> positions.getStartPosition(unit, item)));
        nestedClasses.forEach(item -> scan(item, null));
        return null;
    }

    private void collectMethodTypes(MethodTree method, Set<String> output) {
        if (method.getReturnType() != null) {
            collectTypes(method.getReturnType(), output);
        }
        method.getParameters().forEach(value -> collectTypes(value.getType(), output));
        method.getThrows().forEach(value -> collectTypes(value, output));
    }

    private void addRepresentationExposure(String owner, VariableTree field, Facts.FieldFact fieldFact) {
        if (!fieldFact.publicField || !fieldFact.mutable) return;
        Facts.RepresentationExposure exposure = new Facts.RepresentationExposure();
        exposure.stableId = path + ":" + owner + ":" + fieldFact.name;
        exposure.kind = "public-mutable-field";
        exposure.entity = owner + "." + fieldFact.name;
        exposure.location = location(field);
        exposure.evidence = "public non-final Java field exposes mutable representation";
        exposure.confidence = "exact";
        program.representation.add(exposure);
    }

    private void collectTypes(Tree tree, Set<String> output) {
        new TypeNames(output).scan(tree, null);
    }

    private Facts.Function function(MethodTree method, String owner) {
        Facts.Function result = new Facts.Function();
        result.name = method.getName().contentEquals("<init>") ? owner : method.getName().toString();
        result.receiver = owner;
        result.location = location(method);
        if (method.getBody() != null) {
            result.body.addAll(statements(method.getBody().getStatements()));
        }
        return result;
    }

    private Facts.PublicOperation operation(MethodTree method, String owner) {
        Facts.PublicOperation result = new Facts.PublicOperation();
        result.name = method.getName().toString();
        result.ownerType = owner;
        result.location = location(method);
        result.stableId = path + ":" + owner + ":" + result.name;
        for (VariableTree parameter : method.getParameters()) result.parameters.add(shape(parameter.getType()));
        Tree returnType = method.getReturnType();
        if (returnType != null && !returnType.toString().equals("void")) result.results.add(shape(returnType));
        result.emitsOutput = !result.results.isEmpty();
        return result;
    }

    private Facts.TypeShape shape(Tree tree) {
        return JavaTypeShapes.shape(tree);
    }

    List<Facts.Statement> statements(List<? extends StatementTree> input) {
        return new JavaStatements(this).translate(input);
    }

    Facts.Expression expression(ExpressionTree value) {
        return JavaExpressions.translate(this, value);
    }

    private static final class TypeNames extends TreeScanner<Void, Void> {
        private final Set<String> output;

        private TypeNames(Set<String> output) {
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

    private static final class NestedClasses extends TreeScanner<Void, Void> {
        private final List<ClassTree> output;

        private NestedClasses(List<ClassTree> output) {
            this.output = output;
        }

        @Override
        public Void visitClass(ClassTree node, Void unused) {
            output.add(node);
            return null;
        }
    }

    private static final class MethodBodyFacts extends TreeScanner<Void, Void> {
        private final Set<String> ownFields;
        private final Set<String> typeNames;
        private final List<ClassTree> nestedClasses;
        private final Set<String> locals = new HashSet<>();
        private final SortedSet<String> own = new TreeSet<>();
        private final SortedSet<String> foreign = new TreeSet<>();
        private boolean collectBodyTypes = true;
        private int classDepth;

        private MethodBodyFacts(
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
                new TypeNames(typeNames).scan(value, null);
            }
        }

        @Override
        public Void visitVariable(VariableTree node, Void unused) {
            type(node.getType());
            if (node.getInitializer() != null) {
                scan(node.getInitializer(), unused);
            }
            locals.add(node.getName().toString());
            return null;
        }

        @Override
        public Void visitIdentifier(IdentifierTree node, Void unused) {
            String name = node.getName().toString();
            if (ownFields.contains(name) && !locals.contains(name)) {
                own.add(name);
            }
            return null;
        }

        @Override
        public Void visitClass(ClassTree node, Void unused) {
            if (classDepth == 0) {
                nestedClasses.add(node);
            }
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
            if (node.getClassBody() != null) {
                boolean previous = collectBodyTypes;
                collectBodyTypes = false;
                try {
                    scan(node.getClassBody(), unused);
                } finally {
                    collectBodyTypes = previous;
                }
            }
            return null;
        }

        @Override
        public Void visitNewArray(NewArrayTree node, Void unused) {
            type(node.getType());
            scan(node.getType(), unused);
            node.getDimensions().forEach(value -> scan(value, unused));
            if (node.getInitializers() != null) {
                node.getInitializers().forEach(value -> scan(value, unused));
            }
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
            if (node.getMethodSelect() instanceof MemberSelectTree selected) {
                scan(selected.getExpression(), unused);
            }
            node.getArguments().forEach(value -> scan(value, unused));
            return null;
        }

        @Override
        public Void visitMemberReference(MemberReferenceTree node, Void unused) {
            if (node.getTypeArguments() != null) {
                node.getTypeArguments().forEach(this::type);
            }
            scan(node.getQualifierExpression(), unused);
            return null;
        }
    }
}
