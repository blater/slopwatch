package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.TypeKind;
import java.util.*;

final class JavaDepthBoundary {
    private final Trees trees;
    private final TreePath path;
    private final String file;
    private final TypeElement owner;
    private final CompilationUnitTree unit;
    private final Map<String, Object> supportingContract;
    private final SourcePositions positions;
    private final Map<String, Object> identity;
    private final Map<ExecutableElement, TreePath> sourceMethodPaths;
    private final List<Object> functions = new ArrayList<>();
    private final List<Object> families = new ArrayList<>();
    private final List<Object> routes = new ArrayList<>();
    private final List<Object> slots = new ArrayList<>();
    private final Set<String> concepts = new TreeSet<>();
    private final Map<String, String> gaps = new TreeMap<>();
    private final List<Object> behaviorReasons = new ArrayList<>();
    private final List<Map<String, Object>> creationRoutes = new ArrayList<>();
    private final List<String> passiveAccessors = new ArrayList<>();
    private Map<String, Object> passiveResultEvidence;
    private Map<String, Object> passiveValueObjectEvidence;
    private Map<String, Object> passiveEnumEvidence;
    private boolean validatedCreation;
    private boolean creationIncomplete;
    private final JavaDepthInstancePurityGraph.Graph instancePurity;
    JavaDepthBoundary(Trees trees, TreePath path, CompilationUnitTree unit, String file, TypeElement owner,
                      Map<String, Object> supportingContract,
                      Map<ExecutableElement, TreePath> sourceMethodPaths) {
        this.trees = trees; this.path = path; this.unit = unit; this.file = file; this.owner = owner;
        this.supportingContract = supportingContract;
        this.sourceMethodPaths = sourceMethodPaths == null ? Map.of() : sourceMethodPaths;
        this.instancePurity = JavaDepthInstancePurity.graph(trees, path, owner);
        this.positions = trees.getSourcePositions();
        String name = owner.getQualifiedName().toString();
        identity = Map.of("artifact", owner.getEnclosingElement().toString(),
                "audience", owner.getModifiers().contains(Modifier.PUBLIC) ? "external" : "package",
                "view", "type", "symbol", name);
    }
    void gap(String reason) { gap(reason, reason); }
    void gap(String code, String message) { gaps.merge(code, message, (first, next) -> first + "\n" + next); }
    private void behaviorGap(String code, String member, String message) {
        behaviorReasons.add(Map.of("code", code, "dimension", "behavior", "message", message,
                "fact_ids", List.of(member)));
    }
    void collect() {
        ClassTree declaration = (ClassTree) path.getLeaf();
        if (supportingContract == null) {
            passiveResultEvidence = JavaDepthPassiveResultCarrier.inspect(trees, path, owner);
            passiveValueObjectEvidence = inspectPassiveValueObject();
            passiveEnumEvidence = inspectPassiveEnum();
        }
        if (collectPassiveCarrier()) return;
        if (collectPassiveValueObject()) return;
        if (collectPassiveEnum()) return;
        if (collectValidatedCarrier()) return;
        collectSupportingContractGaps();
        collectUnsupportedTypeSurface(declaration);
        if (owner.getKind() != ElementKind.CLASS) gap("unsupported_type_kind");
        collectMethodInventory(declaration);
        boolean explicitConstructor = collectMembers(declaration);
        finishCollection(declaration, explicitConstructor);
        JavaDepthRouteNormalization.apply(trees, path, owner, families);
    }

    private boolean collectPassiveCarrier() {
        if (!gaps.isEmpty() || supportingContract != null) return false;
        JavaDepthCarrier.Proof proof = JavaDepthCarrier.inspect(trees, path, owner);
        if (proof == null) return false;
        if (proof.constructors().isEmpty()) {
            JavaDepthCreation.addImplicit(owner, identity, families, functions, creationRoutes);
        }
        for (ExecutableElement constructor : proof.constructors()) {
            if (!boundaryVisible(constructor)) continue;
            JavaDepthCreation.addExplicit(owner, identity, constructor, families, functions,
                    slots, concepts, creationRoutes);
        }
        for (ExecutableElement accessor : proof.accessors()) {
            String id = owner.getQualifiedName() + "#" + accessor;
            if (boundaryVisible(accessor)) collectRoute(id, accessor);
            passiveAccessors.add(id);
        }
        return true;
    }

    private Map<String, Object> inspectPassiveValueObject() {
        JavaDepthPassiveValueObject.Proof proof = JavaDepthPassiveValueObject.inspect(trees, path, owner);
        return proof == null ? null : JavaDepthPassiveValueObject.evidence(owner, proof);
    }

    private Map<String, Object> inspectPassiveEnum() {
        JavaDepthPassiveEnum.Proof proof = JavaDepthPassiveEnum.inspect(trees, path, owner);
        return proof == null ? null : JavaDepthPassiveEnum.evidence(owner, proof);
    }

    private boolean collectPassiveValueObject() {
        if (passiveValueObjectEvidence == null) return false;
        JavaDepthPassiveValueObject.Proof proof = JavaDepthPassiveValueObject.inspect(trees, path, owner);
        if (proof == null) return false;
        if (proof.constructors().isEmpty()) JavaDepthCreation.addImplicit(owner, identity, families, functions, creationRoutes);
        for (ExecutableElement constructor : proof.constructors()) {
            if (boundaryVisible(constructor)) {
                JavaDepthCreation.addExplicit(owner, identity, constructor, families, functions, slots, concepts, creationRoutes);
            }
        }
        for (ExecutableElement accessor : proof.accessors()) {
            String id = owner.getQualifiedName() + "#" + accessor;
            collectRoute(id, accessor);
            passiveAccessors.add(id);
        }
        return true;
    }

    private boolean collectPassiveEnum() {
        if (passiveEnumEvidence == null) return false;
        JavaDepthPassiveEnum.Proof proof = JavaDepthPassiveEnum.inspect(trees, path, owner);
        if (proof == null) return false;
        List<Object> constantRoutes = new ArrayList<>();
        String constantFamily = "value:" + owner.getQualifiedName();
        for (VariableElement constant : proof.constants()) {
            String id = owner.getQualifiedName() + "#" + constant.getSimpleName();
            Map<String, Object> route = Map.of("id", id, "family", constantFamily,
                    "signature", owner.getQualifiedName().toString(), "target_function_id", id,
                    "boundary", identity, "required_slots", List.of(), "exposed_slots", List.of());
            routes.add(route);
            constantRoutes.add(route);
        }
        if (!constantRoutes.isEmpty()) families.add(Map.of("id", constantFamily, "routes", constantRoutes));
        for (ExecutableElement accessor : proof.accessors()) {
            String id = owner.getQualifiedName() + "#" + accessor;
            collectRoute(id, accessor);
            passiveAccessors.add(id);
        }
        for (ExecutableElement accessor : proof.staticAccessors()) {
            String id = owner.getQualifiedName() + "#" + accessor;
            if (boundaryVisible(accessor)) collectRoute(id, accessor);
            passiveAccessors.add(id);
        }
        return true;
    }

    private boolean collectValidatedCarrier() {
        if (!gaps.isEmpty() || supportingContract != null) return false;
        JavaDepthValidatedCarrier.Proof proof = JavaDepthValidatedCarrier.inspect(trees, path, owner);
        if (proof == null) return false;
        validatedCreation = true;
        for (ExecutableElement constructor : proof.constructors()) {
            if (!boundaryVisible(constructor)) continue;
            JavaDepthCreation.addExplicit(owner, identity, constructor, families, functions,
                    slots, concepts, creationRoutes);
            String id = JavaDepthRoles.constructorID(constructor);
            // Replace allocation-only flow with the constructor's guarded payload flow.
            JavaDepthCarrierFlow.ConstructorFlow flow =
                    JavaDepthCarrierFlow.constructorFlow(trees, path, constructor, proof.fields(), id);
            functions.set(functions.size() - 1, flow.function());
            int index = creationRoutes.size() - 1;
            Map<String, Object> creation = new LinkedHashMap<>(creationRoutes.get(index));
            creation.put("initial_fields", flow.initialFields());
            boolean rejection = JavaDepthValidatedCarrier.hasRejection(trees, path, constructor);
            creation.put("data_only", !rejection);
            creation.put("possible_failures", rejection ? List.of("source_rejection") : List.of());
            creation.put("behavior", rejection ? List.of("normal", "rejection") : List.of("normal"));
            creationRoutes.set(index, creation);
        }
        for (ExecutableElement accessor : proof.accessors()) {
            String id = owner.getQualifiedName() + "#" + accessor;
            collectRoute(id, accessor);
            passiveAccessors.add(id);
            functions.add(JavaDepthCarrierFlow.accessor(trees, path, accessor, id));
        }
        return true;
    }

    private void finishCollection(ClassTree declaration, boolean explicitConstructor) {
        if (!explicitConstructor && supportingContract == null && constructible() && hasVisibleInstanceMethod(declaration)) {
            JavaDepthCreation.addImplicit(owner, identity, families, functions, creationRoutes);
        }
        if (routes.isEmpty() && families.isEmpty() && supportingContract == null) gap("unresolved_or_absent_public_surface");
    }

    private boolean collectMembers(ClassTree declaration) {
        boolean explicitConstructor = false;
        for (Tree member : declaration.getMembers()) {
            if (member instanceof MethodTree method) {
                explicitConstructor |= collectMethodMember(method);
                continue;
            }
            if (member instanceof VariableTree variable) {
                collectField(variable);
                continue;
            }
            if (member instanceof BlockTree block) {
                behaviorGap("initializer_effects_unknown", owner.getQualifiedName() + "/initializer/" + positions.getStartPosition(unit, block),
                        "Initializer is inventoried; its effects are outside precise flow analysis.");
                continue;
            }
            if (member instanceof ClassTree || member.getKind() == Tree.Kind.EMPTY_STATEMENT) continue;
            gap("unsupported_class_member");
        }
        return explicitConstructor;
    }

    // Caller-visible signatures are independent of admission to the flow evaluator.
    private void collectMethodInventory(ClassTree declaration) {
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof MethodTree tree)) continue;
            Element element = trees.getElement(new TreePath(path, tree));
            if (element instanceof ExecutableElement method && method.getKind() == ElementKind.METHOD
                    && boundaryVisible(method)) {
                collectRoute(JavaDepthRoles.methodID(method), method);
            }
        }
    }

    private boolean collectMethodMember(MethodTree method) {
        TreePath memberPath = new TreePath(path, method);
        Element element = trees.getElement(memberPath);
        boolean constructor = element instanceof ExecutableElement executable
                && executable.getKind() == ElementKind.CONSTRUCTOR;
        collectMethod(method);
        return constructor;
    }

    private void collectSupportingContractGaps() {
        if (supportingContract == null) return;
        if (!"measured".equals(supportingContract.get("inventory"))
                || !"measured".equals(supportingContract.get("exposure"))) {
            gap("incomplete_supporting_role");
        }
    }

    private void collectUnsupportedTypeSurface(ClassTree declaration) {
        if (supportingContract != null) return;
        if (declaration.getExtendsClause() != null || !declaration.getImplementsClause().isEmpty()) gap("unsupported_inherited_surface");
        if (!owner.getTypeParameters().isEmpty()) gap("unsupported_generic_surface");
    }
    private void collectMethod(MethodTree tree) {
        TreePath memberPath = new TreePath(path, tree);
        Element element = trees.getElement(memberPath);
        if (!(element instanceof ExecutableElement method)) { gap("unresolved_method"); return; }
        if (method.getKind() == ElementKind.CONSTRUCTOR) { collectConstructor(tree, method); return; }
        String id = owner.getQualifiedName() + "#" + method;
        if (!method.getModifiers().contains(Modifier.STATIC) && !admitInstance(method, memberPath)) return;
        if (method.getModifiers().contains(Modifier.SYNCHRONIZED)) {
            if (boundaryVisible(method)) behaviorGap("synchronization_effects_unknown", id,
                    "Method remains in the public inventory; precise synchronization effects are unknown.");
            return;
        }
        functions.add(new JavaDepthFunction(trees, memberPath, sourceMethodPaths.keySet()).lower(id, method));
    }

    private void collectConstructor(MethodTree tree, ExecutableElement method) {
        if (supportingContract != null) return;
        if (!JavaDepthCreation.trivialConstructor(trees, path, tree, method)) {
            if (boundaryVisible(method)) {
                creationIncomplete = true;
                JavaDepthCreation.addInventory(owner, identity, method, families, slots, concepts);
                behaviorGap("constructor_effects_unknown", JavaDepthRoles.constructorID(method),
                        "Constructor signature is inventoried; its body is outside precise creation analysis.");
            }
            return;
        }
        if (boundaryVisible(method)) {
            JavaDepthCreation.addExplicit(owner, identity, method, families, functions, slots,
                    concepts, creationRoutes);
        }
    }

    private boolean admitInstance(ExecutableElement method, TreePath memberPath) {
        if (supportingContract != null) return false;
        if (!JavaDepthCalls.dispatchable(method, owner) || !instancePurity.isStateless(method)) {
            if (boundaryVisible(method)) behaviorGap("instance_effects_unknown", JavaDepthRoles.methodID(method),
                    "Method is inventoried; receiver state or dispatch is outside precise stateless analysis.");
            return false;
        }
        return true;
    }

    private boolean constructible() {
        return (owner.getModifiers().contains(Modifier.PUBLIC) || !owner.getModifiers().contains(Modifier.PRIVATE))
                && !owner.getModifiers().contains(Modifier.ABSTRACT);
    }

    private boolean hasVisibleInstanceMethod(ClassTree declaration) {
        for (Tree member : declaration.getMembers()) {
            if (!(member instanceof MethodTree method)) continue;
            Element element = trees.getElement(new TreePath(path, method));
            if (element instanceof ExecutableElement executable
                    && executable.getKind() == ElementKind.METHOD
                    && boundaryVisible(executable)
                    && !executable.getModifiers().contains(Modifier.STATIC)) return true;
        }
        return false;
    }

    private boolean boundaryVisible(ExecutableElement method) {
        return owner.getModifiers().contains(Modifier.PUBLIC)
                ? method.getModifiers().contains(Modifier.PUBLIC)
                : !method.getModifiers().contains(Modifier.PRIVATE);
    }

    private boolean isCompileTimeConstant(VariableTree tree) {
        TreePath fieldPath = new TreePath(path, tree);
        Element element = trees.getElement(fieldPath);
        return element instanceof VariableElement field
                && field.getModifiers().contains(Modifier.STATIC)
                && field.getModifiers().contains(Modifier.FINAL)
                && field.getConstantValue() != null;
    }

    private void collectField(VariableTree tree) {
        if (supportingContract != null) return;
        Element element = trees.getElement(new TreePath(path, tree));
        if (!(element instanceof VariableElement field)) { gap("unresolved_field"); return; }
        String id = owner.getQualifiedName() + "#field:" + field.getSimpleName();
        boolean visible = owner.getModifiers().contains(Modifier.PUBLIC)
                ? field.getModifiers().contains(Modifier.PUBLIC) : !field.getModifiers().contains(Modifier.PRIVATE);
        if (visible) collectExposedField(tree, field, id);
        if (tree.getInitializer() != null && !isCompileTimeConstant(tree)) {
            behaviorGap("initializer_effects_unknown", id,
                    "Field is inventoried; initializer effects are outside precise flow analysis.");
        }
    }
    private void collectExposedField(VariableTree tree, VariableElement field, String id) {
        String concept = JavaDepthTypes.concept(field.asType());
        concepts.add(concept);
        List<String> exposed = new ArrayList<>();
        if (!field.getModifiers().contains(Modifier.FINAL)) {
            String slot = id + "/value";
            slots.add(Map.of("id", slot, "concept", concept, "required", false));
            exposed.add(slot);
        }
        Map<String, Object> route = Map.of("id", id, "family", id, "signature", field.asType().toString(),
                "target_function_id", id, "boundary", identity, "required_slots", List.of(), "exposed_slots", exposed);
        routes.add(route);
        families.add(Map.of("id", id, "routes", List.of(route)));
        if (!isCompileTimeConstant(tree)) {
            behaviorGap("field_access_effects_unknown", id,
                    "Exposed field is inventoried; mutation and alias effects are not precisely modeled.");
        }
    }

    private void collectRoute(String id, ExecutableElement method) {
        if (method.isVarArgs() || !method.getTypeParameters().isEmpty()) gap("unsupported_signature_shape");
        if (!method.getThrownTypes().isEmpty()) gap("unsupported_declared_failure_contract");
        List<String> required = new ArrayList<>();
        for (int index = 0; index < method.getParameters().size(); index++) {
            String concept = JavaDepthTypes.concept(method.getParameters().get(index).asType());
            concepts.add(concept);
            String slot = id + "/arg" + index;
            required.add(slot);
            slots.add(Map.of("id", slot, "concept", concept, "required", true));
        }
        if (method.getReturnType().getKind() != TypeKind.VOID) {
            concepts.add(JavaDepthTypes.concept(method.getReturnType()));
        }
        if (concepts.contains("unknown")) gap("unsupported_surface_type");
        Map<String, Object> route = Map.of("id", id, "family", id, "signature", method.asType().toString(), "target_function_id", id,
                "boundary", identity, "required_slots", required, "exposed_slots", required);
        routes.add(route);
        families.add(Map.of("id", id, "routes", List.of(route)));
    }
    Map<String, Object> assessment() {
        Map<String, Object> knowledge = new LinkedHashMap<>();
        for (String dimension : List.of("inventory", "burden", "behavior", "alias_effects")) {
            boolean known = gaps.isEmpty() && (behaviorReasons.isEmpty()
                    || dimension.equals("inventory") || dimension.equals("burden"));
            knowledge.put(dimension, Map.of("state", known ? "measured" : "partial", "essential", true));
        }
        List<Object> reasons = new ArrayList<>();
        for (Map.Entry<String, String> gap : gaps.entrySet()) {
            reasons.add(Map.of("code", gap.getKey(), "dimension", "inventory", "message", gap.getValue()));
        }
        reasons.addAll(behaviorReasons);
        List<Object> conceptFacts = new ArrayList<>();
        for (String concept : concepts) conceptFacts.add(Map.of("id", concept, "kind", concept));
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("identity", identity); result.put("state", gaps.isEmpty() && behaviorReasons.isEmpty() ? "measured" : "partial"); result.put("knowledge", knowledge);
        result.put("burden", Map.of("O", families.size(), "T", concepts.size()));
        result.put("concepts", conceptFacts);
        result.put("slots", slots);
        result.put("route_families", families);
        List<Object> incompleteAlternatives = new ArrayList<>();
        for (int index = 0; index < families.size(); index++) incompleteAlternatives.add(List.of());
        result.put("family_alternatives", incompleteAlternatives);
        result.put("reasons", reasons);
        result.put("files", List.of(file));
        result.put("source_locations", List.of(sourceLocation((Tree) path.getLeaf())));
        if (supportingContract != null) result.put("supporting_contract", supportingContract);
        if (!creationRoutes.isEmpty()) result.put("creation", JavaDepthCreation.fact(owner, creationRoutes,
                passiveAccessors, validatedCreation, creationIncomplete));
        List<Map<String, Object>> evidence = supportingEvidence();
        if (!evidence.isEmpty()) result.put("evidence", evidence);
        if (!gaps.containsKey("incomplete_source_inventory")) {
            Map<String, Object> minimum = JavaDepthMinimum.assess(trees, path, owner, result, identity, file);
            if (minimum != null) result.put("bounded_assessment", minimum);
        }
        return result;
    }

    private List<Map<String, Object>> supportingEvidence() {
        List<Map<String, Object>> evidence = new ArrayList<>();
        if (validatedCreation) evidence.add(validatedCarrierEvidence());
        if (passiveResultEvidence != null) evidence.add(passiveResultEvidence);
        if (passiveValueObjectEvidence != null) evidence.add(passiveValueObjectEvidence);
        if (passiveEnumEvidence != null) evidence.add(passiveEnumEvidence);
        return evidence;
    }

    private Map<String, Object> validatedCarrierEvidence() {
        String rule = "immutable-scalar-creation";
        return Map.of("id", rule + ":" + owner.getQualifiedName(), "kind", rule,
                "status", "recognized",
                "description", "Immutable scalar carrier: constructor rejection is assessed as behavior; direct getters add no transformation credit.",
                "provenance", List.of(Map.of("path", file, "span", sourceLocation(path.getLeaf()),
                        "rule_id", rule, "fact_ids", passiveAccessors)));
    }

    Map<String, Object> flow() {
        return Map.of("artifact", identity.get("artifact"), "language", "java", "functions", functions, "public_routes", routes);
    }

    private Map<String, Object> sourceLocation(Tree tree) {
        return JavaDepthLocations.sourceLocation(unit, positions, file, tree);
    }
}
