package dev.slopslap.structural;

import com.sun.source.tree.*;

import javax.tools.Diagnostic;
import java.util.*;

final class JavaClassFacts {
    private static final Set<String> PRIMITIVES = Set.of(
            "boolean", "byte", "char", "double", "float", "int", "long", "short", "void"
    );

    private final JavaAnalyzer analyzer;
    private final Facts.Program program;
    private final String path;

    JavaClassFacts(JavaAnalyzer analyzer, Facts.Program program, String path) {
        this.analyzer = analyzer;
        this.program = program;
        this.path = path;
    }

    void analyze(ClassTree node, List<ClassTree> nestedClasses) {
        String name = node.getSimpleName().toString();
        if (name.isEmpty()) {
            JavaNestedClasses nested = new JavaNestedClasses(nestedClasses);
            node.getMembers().forEach(member -> nested.scan(member, null));
            return;
        }
        Facts.TypeFact type = newTypeFact(node, name);
        Set<String> genericNames = new HashSet<>();
        node.getTypeParameters().forEach(value -> genericNames.add(value.getName().toString()));
        Set<String> foreignTypes = inheritedTypes(node);
        collectClassMembers(node, name, type, foreignTypes, nestedClasses);
        collectClassMethods(node, name, type, foreignTypes, nestedClasses);
        finishType(type, name, genericNames, foreignTypes);
    }

    private Facts.TypeFact newTypeFact(ClassTree node, String name) {
        Facts.TypeFact type = new Facts.TypeFact();
        type.name = name;
        type.kind = switch (node.getKind()) {
            case INTERFACE, ANNOTATION_TYPE -> "interface";
            case ENUM -> "enum";
            case RECORD -> "record";
            default -> "class";
        };
        type.location = analyzer.location(node);
        return type;
    }

    private Set<String> inheritedTypes(ClassTree node) {
        Set<String> foreignTypes = new TreeSet<>();
        if (node.getExtendsClause() != null) collectTypes(node.getExtendsClause(), foreignTypes);
        node.getImplementsClause().forEach(value -> collectTypes(value, foreignTypes));
        return foreignTypes;
    }

    private void collectClassMembers(
            ClassTree node, String name, Facts.TypeFact type, Set<String> foreignTypes,
            List<ClassTree> nestedClasses
    ) {
        JavaNestedClasses nested = new JavaNestedClasses(nestedClasses);
        for (Tree member : node.getMembers()) {
            if (member instanceof VariableTree field) {
                collectField(name, type, foreignTypes, nested, field);
            } else if (member instanceof ClassTree child) {
                nestedClasses.add(child);
            } else if (member instanceof BlockTree block) {
                nested.scan(block, null);
            }
        }
    }

    private void collectField(
            String owner, Facts.TypeFact type, Set<String> foreignTypes,
            JavaNestedClasses nested, VariableTree field
    ) {
        type.fields.add(field.getName().toString());
        collectTypes(field.getType(), foreignTypes);
        Facts.FieldFact fieldFact = new Facts.FieldFact();
        fieldFact.name = field.getName().toString();
        fieldFact.publicField = field.getModifiers().getFlags().contains(javax.lang.model.element.Modifier.PUBLIC);
        fieldFact.mutable = !field.getModifiers().getFlags().contains(javax.lang.model.element.Modifier.FINAL);
        fieldFact.type = JavaTypeShapes.shape(field.getType());
        type.fieldFacts.add(fieldFact);
        addRepresentationExposure(owner, field, fieldFact);
        nested.scan(field.getInitializer(), null);
    }

    private void collectClassMethods(
            ClassTree node, String name, Facts.TypeFact type, Set<String> foreignTypes,
            List<ClassTree> nestedClasses
    ) {
        Set<String> ownFields = Set.copyOf(type.fields);
        int ordinal = 0;
        for (Tree member : node.getMembers()) {
            if (!(member instanceof MethodTree method)
                    || analyzer.startPosition(method) == Diagnostic.NOPOS) continue;
            collectMethod(name, type, foreignTypes, nestedClasses, ownFields, method, ordinal++);
        }
    }

    private void collectMethod(
            String owner, Facts.TypeFact type, Set<String> foreignTypes,
            List<ClassTree> nestedClasses, Set<String> ownFields, MethodTree method, int ordinal
    ) {
        Facts.Function function = function(method, owner);
        program.functions.add(function);
        type.methods.add(function);
        if (type.kind.equals("interface") || method.getModifiers().getFlags().contains(javax.lang.model.element.Modifier.PUBLIC))
            program.publicOperations.add(operation(method, owner));
        if (type.kind.equals("interface")) type.interfaceMethodCount++;
        JavaMethodBodyFacts fields = new JavaMethodBodyFacts(
                ownFields, method.getParameters(), foreignTypes, nestedClasses
        );
        if (method.getBody() != null) fields.scan(method.getBody(), null);
        type.methodFields.put(method.getName() + "#" + ordinal, new ArrayList<>(fields.own));
        type.foreignFields.addAll(fields.foreign);
        collectMethodTypes(method, foreignTypes);
    }

    private Facts.Function function(MethodTree method, String owner) {
        Facts.Function result = new Facts.Function();
        result.name = method.getName().contentEquals("<init>") ? owner : method.getName().toString();
        result.receiver = owner;
        result.location = analyzer.location(method);
        if (method.getBody() != null) result.body.addAll(analyzer.statements(method.getBody().getStatements()));
        return result;
    }

    private Facts.PublicOperation operation(MethodTree method, String owner) {
        Facts.PublicOperation result = new Facts.PublicOperation();
        result.name = method.getName().toString();
        result.ownerType = owner;
        result.location = analyzer.location(method);
        result.stableId = path + ":" + owner + ":" + result.name;
        for (VariableTree parameter : method.getParameters())
            result.parameters.add(JavaTypeShapes.shape(parameter.getType()));
        Tree returnType = method.getReturnType();
        if (returnType != null && !returnType.toString().equals("void"))
            result.results.add(JavaTypeShapes.shape(returnType));
        result.emitsOutput = !result.results.isEmpty();
        return result;
    }

    private void collectMethodTypes(MethodTree method, Set<String> output) {
        if (method.getReturnType() != null) collectTypes(method.getReturnType(), output);
        method.getParameters().forEach(value -> collectTypes(value.getType(), output));
        method.getThrows().forEach(value -> collectTypes(value, output));
    }

    private void addRepresentationExposure(String owner, VariableTree field, Facts.FieldFact fieldFact) {
        if (!fieldFact.publicField || !fieldFact.mutable) return;
        Facts.RepresentationExposure exposure = new Facts.RepresentationExposure();
        exposure.stableId = path + ":" + owner + ":" + fieldFact.name;
        exposure.kind = "public-mutable-field";
        exposure.entity = owner + "." + fieldFact.name;
        exposure.location = analyzer.location(field);
        exposure.evidence = "public non-final Java field exposes mutable representation";
        exposure.confidence = "exact";
        program.representation.add(exposure);
    }

    private void collectTypes(Tree tree, Set<String> output) {
        new JavaTypeNames(output).scan(tree, null);
    }

    private void finishType(Facts.TypeFact type, String name, Set<String> genericNames, Set<String> foreignTypes) {
        foreignTypes.remove(name);
        foreignTypes.removeAll(genericNames);
        foreignTypes.removeAll(PRIMITIVES);
        type.foreignTypes.addAll(foreignTypes);
        SortedSet<String> uniqueForeignFields = new TreeSet<>();
        type.foreignFields.stream().map(String::trim).forEach(uniqueForeignFields::add);
        type.foreignFields.clear();
        type.foreignFields.addAll(uniqueForeignFields);
        program.types.add(type);
    }
}
