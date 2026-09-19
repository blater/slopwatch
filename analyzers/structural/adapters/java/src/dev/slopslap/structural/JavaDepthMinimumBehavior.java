package dev.slopslap.structural;

import com.sun.source.tree.*;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreePathScanner;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.*;
import javax.lang.model.type.DeclaredType;
import javax.lang.model.type.TypeKind;
import javax.lang.model.type.TypeMirror;
import java.util.*;

/** Bounded connected-source recognizer for the minimum Java policy. */
final class JavaDepthMinimumBehavior {
    private static final int MAX_WORK = 8192;
    private final Trees trees;
    private final JavaDepthComputationIdentity.Session computations;
    private final TreePath ownerPath;
    private final TypeElement owner;
    private final Map<ExecutableElement, JavaDepthMinimumSurface.Callable> methods;
    private final Map<ExecutableElement, JavaDepthMinimumSurface.Callable> allMethods = new IdentityHashMap<>();
    private final Map<ExecutableElement, Summary> cache = new IdentityHashMap<>();
    private final Set<ExecutableElement> active = Collections.newSetFromMap(new IdentityHashMap<>());
    private final Set<String> limitations = new TreeSet<>();
    private final Map<TypeElement, JavaDepthMinimumMonitors> monitorCache = new IdentityHashMap<>();
    private final Set<String> sourceDelegations = new TreeSet<>();
    private int work;
    private boolean exhausted;

    JavaDepthMinimumBehavior(Trees trees, TreePath ownerPath, TypeElement owner,
                             List<JavaDepthMinimumSurface.Callable> callables) {
        this.trees = trees;
        this.computations = new JavaDepthComputationIdentity.Session(trees);
        this.ownerPath = ownerPath;
        this.owner = owner;
        this.methods = new IdentityHashMap<>();
        monitorCache.put(owner, new JavaDepthMinimumMonitors(trees, ownerPath, owner));
        for (JavaDepthMinimumSurface.Callable callable : callables) {
            methods.put(callable.element(), callable);
            allMethods.put(callable.element(), callable);
        }
        if (ownerPath.getLeaf() instanceof ClassTree declaration) {
            for (Tree member : declaration.getMembers()) {
                if (!(member instanceof MethodTree tree)) continue;
                TreePath path = new TreePath(ownerPath, tree);
                Element element = trees.getElement(path);
                if (element instanceof ExecutableElement executable
                        && executable.getEnclosingElement().equals(owner)) {
                    allMethods.putIfAbsent(executable, new JavaDepthMinimumSurface.Callable(executable, tree, path));
                }
            }
        }
    }

    Result analyze() {
        List<Summary> entries = new ArrayList<>();
        List<JavaDepthMinimumSurface.Callable> ordered = new ArrayList<>(methods.values());
        ordered.sort(Comparator.comparing(callable -> callableID(callable.element())));
        for (JavaDepthMinimumSurface.Callable callable : ordered) entries.add(summary(callable.element()));
        List<Map<String, Object>> reasons = new ArrayList<>();
        for (String limitation : limitations) reasons.add(Map.of("code", "bounded_source_limit",
                "dimension", "behavior", "message", limitation));
        if (exhausted && work > MAX_WORK) reasons.add(Map.of("code", "bounded_source_work_limit", "dimension", "behavior",
                "message", "bounded-static-v1 stopped after its deterministic source budget"));
        return new Result(reasons, List.copyOf(limitations), exhausted, List.copyOf(entries), List.copyOf(sourceDelegations));
    }

    record Result(List<Map<String, Object>> reasons, List<String> limitations,
                  boolean exhausted, List<Summary> summaries, List<String> sourceDelegations) {
        List<Map<String, Object>> obligations(TypeElement owner, String file) {
            Map<String, Map<String, Object>> result = new TreeMap<>();
            for (Summary summary : summaries) {
                for (String category : summary.categories) {
                    for (String root : summary.roots(owner, category)) {
                        Map<String, Object> obligation = obligation(owner, category, root, summary.id);
                        String id = (String) obligation.get("id");
                        Map<String, Object> existing = result.get(id);
                        if (existing != null) {
                            Set<String> evidence = new TreeSet<>();
                            for (Object item : (List<?>) existing.get("evidence")) evidence.add((String) item);
                            evidence.add(summary.id);
                            obligation = new LinkedHashMap<>(obligation);
                            obligation.put("evidence", new ArrayList<>(evidence));
                        }
                        result.put(id, obligation);
                    }
                }
            }
            return new ArrayList<>(result.values());
        }

        List<List<String>> obligationAlternatives(TypeElement owner, String routeID) {
            for (Summary summary : summaries) {
                if (summary.id.equals(routeID)) {
                    List<List<String>> result = new ArrayList<>();
                    for (Set<String> alternative : summary.alternatives) {
                        List<String> ids = new ArrayList<>();
                        for (String credit : alternative) {
                            int separator = credit.indexOf('\u0000');
                            if (separator <= 0 || separator == credit.length() - 1) continue;
                            String category = credit.substring(0, separator);
                            String root = credit.substring(separator + 1);
                            ids.add(root + "/bounded/" + category);
                        }
                        Collections.sort(ids);
                        result.add(ids);
                    }
                    return result;
                }
            }
            return List.of(List.of());
        }

        private static Map<String, Object> obligation(TypeElement owner, String category,
                                                      String root, String evidence) {
            return Map.of("id", root + "/bounded/" + category,
                    "category", category, "governed", root, "rule", JavaDepthMinimum.RULE,
                    "evidence", List.of(evidence), "provenance", List.of());
        }
    }

    static final class Summary {
        final String id;
        final Set<String> categories = new TreeSet<>();
        final Set<String> returnedCategories = new TreeSet<>();
        final Set<String> effectCategories = new TreeSet<>();
        final Set<String> transformationRoots = new TreeSet<>();
        final Set<String> effectTransformationRoots = new TreeSet<>();
        final Map<String, Set<String>> fieldRoots = new TreeMap<>();
        final Map<String, Set<String>> creditRoots = new TreeMap<>();
        final List<Set<String>> alternatives = new ArrayList<>();
        boolean onlyExceptional;
        Summary(String id) { this.id = id; alternatives.add(new TreeSet<>()); }

        void credit(String category, String root) {
            categories.add(category);
            if (root != null && !root.isEmpty()) {
                creditRoots.computeIfAbsent(category, ignored -> new TreeSet<>()).add(root);
            }
        }

        Set<String> roots(TypeElement owner, String category) {
            if (category.equals("C") && !fieldRoots.getOrDefault(category, Set.of()).isEmpty()) {
                return fieldRoots.get(category);
            }
            if (!creditRoots.getOrDefault(category, Set.of()).isEmpty()) return creditRoots.get(category);
            if (category.equals("X")) {
                if (!transformationRoots.isEmpty()) return transformationRoots;
                String prefix = owner.getQualifiedName() + "#";
                String output = id.startsWith(prefix) ? id.substring(prefix.length()) : id;
                return Set.of(owner.getQualifiedName() + "/output/" + output);
            }
            String suffix = switch (category) {
                case "V" -> "validation";
                case "Y" -> "coordination";
                default -> "output";
            };
            return Set.of(owner.getQualifiedName() + "/" + suffix);
        }

        void addFieldRoot(String category, VariableElement field) {
            fieldRoots.computeIfAbsent(category, ignored -> new TreeSet<>())
                    .add(JavaDepthRoles.fieldID(field));
        }

        void addFieldRoots(Map<String, Set<String>> roots) {
            for (Map.Entry<String, Set<String>> entry : roots.entrySet()) {
                fieldRoots.computeIfAbsent(entry.getKey(), ignored -> new TreeSet<>())
                        .addAll(entry.getValue());
            }
        }

        void addCreditRoots(Map<String, Set<String>> roots) {
            for (Map.Entry<String, Set<String>> entry : roots.entrySet()) {
                creditRoots.computeIfAbsent(entry.getKey(), ignored -> new TreeSet<>()).addAll(entry.getValue());
            }
        }
    }

    private Summary summary(ExecutableElement method) {
        if (active.contains(method) || active.size() >= 32) {
            limitations.add("recursive or over-depth source delegation is not expanded");
            return new Summary(callableID(method));
        }
        Summary known = cache.get(method);
        if (known != null) return known;
        Summary result = new Summary(callableID(method));
        cache.put(method, result);
        active.add(method);
        JavaDepthMinimumSurface.Callable callable = allMethods.get(method);
        if (callable == null) {
            TreePath sourcePath = trees.getPath(method);
            if (sourcePath != null && sourcePath.getLeaf() instanceof MethodTree sourceMethod) {
                callable = new JavaDepthMinimumSurface.Callable(method, sourceMethod, sourcePath);
                allMethods.put(method, callable);
            }
        }
        if (callable == null || callable.tree().getBody() == null) {
            active.remove(method);
            return result;
        }
        Scanner scanner = new Scanner(result, method);
        try {
            scanner.scan(new TreePath(callable.path(), callable.tree().getBody()), null);
        } catch (WorkLimit ignored) {
            // The outer assessment remains useful for already visited routes.
        }
        if (method.getModifiers().contains(Modifier.SYNCHRONIZED) && scanner.stateEffects > 0) {
            scanner.creditCoordination(scanner.monitors.methodMonitor(method));
        }
        result.alternatives.clear();
        result.onlyExceptional = !scanner.paths.isEmpty() && scanner.paths.stream().allMatch(path -> path.exceptional);
        for (PathAlternative path : scanner.paths) if (!path.exceptional) result.alternatives.add(new TreeSet<>(path.credits));
        if (result.alternatives.isEmpty()) result.alternatives.add(new TreeSet<>());
        if (scanner.stateEffects > 0 && result.categories.contains("X")) result.effectCategories.add("X");
        if (!result.fieldRoots.isEmpty()) result.effectCategories.add("C");
        if (result.categories.contains("V")) result.effectCategories.add("V");
        if (result.categories.contains("Y")) result.effectCategories.add("Y");
        active.remove(method);
        return result;
    }

    private final class Scanner extends TreePathScanner<Void, Void> {
        private final Summary result;
        private final TypeElement owner;
        private final JavaDepthMinimumMonitors monitors;
        private final Set<Element> connected = Collections.newSetFromMap(new IdentityHashMap<>());
        private final Map<Element, ExprFacts> locals = new IdentityHashMap<>();
        private final Set<VariableElement> controllingFields = Collections.newSetFromMap(new IdentityHashMap<>());
        private final Map<VariableElement, Boolean> booleanState = new IdentityHashMap<>();
        private List<PathAlternative> paths = new ArrayList<>();
        private final Deque<Tree> breakTargets = new ArrayDeque<>();
        private final boolean constructor;
        private int stateEffects;

        Scanner(Summary result, ExecutableElement method) {
            this.result = result;
            this.owner = (TypeElement) method.getEnclosingElement();
            this.monitors = monitorCache.computeIfAbsent(owner,
                    type -> new JavaDepthMinimumMonitors(trees, trees.getPath(type), type));
            constructor = method.getKind() == ElementKind.CONSTRUCTOR;
            paths.add(new PathAlternative());
            connected.addAll(method.getParameters());
            for (Element enclosed : owner.getEnclosedElements()) {
                if (enclosed instanceof VariableElement field && field.getConstantValue() == null && !constructor) connected.add(field);
            }
        }

        @Override public Void scan(Tree tree, Void unused) {
            if (tree == null) return null;
            // Statement control flow is opt-in. New AST constructs must acquire
            // an explicit transfer before their descendants can earn credit.
            if (tree instanceof StatementTree && !supportedStatement(tree.getKind())) {
                unsupportedControl(tree);
                return null;
            }
            return super.scan(tree, unused);
        }

        private boolean supportedStatement(Tree.Kind kind) {
            return switch (kind) {
                case BLOCK, VARIABLE, EXPRESSION_STATEMENT, IF, RETURN, THROW,
                        WHILE_LOOP, FOR_LOOP, ENHANCED_FOR_LOOP, SYNCHRONIZED,
                        SWITCH, TRY, BREAK, EMPTY_STATEMENT -> true;
                default -> false;
            };
        }

        private void unsupportedControl(Tree tree) {
            tick();
            limitations.add(result.id + ": unsupported control transfer " + tree.getKind()
                    + "; nested and subsequent effects are not assumed to execute");
            List<PathAlternative> opaqueExits = copyPaths(paths);
            for (PathAlternative path : opaqueExits) if (path.live) path.live = false;
            mergePaths(paths, opaqueExits);
            locals.clear();
            booleanState.clear();
        }

        @Override public Void visitSwitchExpression(SwitchExpressionTree tree, Void unused) {
            unsupportedControl(tree);
            return null;
        }

        @Override public Void visitReturn(ReturnTree tree, Void unused) {
            tick();
            if (tree.getExpression() != null) {
                    ExprFacts facts = expression(tree.getExpression(), getCurrentPath());
                    if (facts.connected) {
                        if (facts.transform) {
                        result.returnedCategories.add("X");
                        transformation(tree.getExpression(), "return", false);
                    }
                    result.categories.addAll(facts.categories);
                }
            }
            Void value = super.visitReturn(tree, unused);
            terminatePaths(false);
            return value;
        }

        @Override public Void visitThrow(ThrowTree tree, Void unused) {
            tick();
            TypeElement thrown = thrownType(tree.getExpression(), getCurrentPath());
            for (PathAlternative path : paths) if (path.live) path.pendingExceptionType = thrown;
            if (!recognizedError(tree.getExpression(), getCurrentPath())) {
                exhausted = true;
                limitations.add(result.id + ": unsupported thrown value keeps bounded validation unknown");
            }
            if (thrown == null) limitations.add(result.id + ": unresolved thrown type keeps exceptional flow unknown");
            Void value = super.visitThrow(tree, unused);
            terminatePaths(true);
            return value;
        }

        @Override public Void visitTry(TryTree tree, Void unused) {
            tick();
            if (!tree.getResources().isEmpty()) {
                unsupportedControl(tree);
                return null;
            }
            List<PathAlternative> alreadyCompleted = new ArrayList<>();
            List<PathAlternative> entry = new ArrayList<>();
            for (PathAlternative path : paths) {
                if (path.live) entry.add(path.copy()); else alreadyCompleted.add(path.copy());
            }
            if (entry.isEmpty()) return null;
            paths = entry;
            scan(tree.getBlock(), unused);

            List<PathAlternative> normal = new ArrayList<>();
            List<PathAlternative> pending = new ArrayList<>();
            for (PathAlternative path : paths) {
                if (path.exceptional) pending.add(path);
                else normal.add(path);
            }
            List<PathAlternative> caught = new ArrayList<>();
            for (CatchTree catcher : tree.getCatches()) {
                List<PathAlternative> matching = new ArrayList<>();
                List<PathAlternative> remaining = new ArrayList<>();
                for (PathAlternative path : pending) {
                    if (matchesCatch(path, catcher)) matching.add(path);
                    else remaining.add(path);
                }
                pending = remaining;
                if (matching.isEmpty()) continue;
                for (PathAlternative path : matching) {
                    path.live = true;
                    path.exceptional = false;
                    path.pendingExceptionType = null;
                }
                paths = matching;
                scan(catcher.getBlock(), unused);
                caught.addAll(paths);
            }

            List<PathAlternative> incoming = new ArrayList<>(normal.size() + caught.size() + pending.size());
            incoming.addAll(normal);
            incoming.addAll(caught);
            incoming.addAll(pending);
            if (tree.getFinallyBlock() == null) {
                incoming.addAll(alreadyCompleted);
                paths = deduplicatePaths(incoming);
                return null;
            }

            // Preserve completion in a local frame, so nested finally blocks
            // cannot overwrite an outer pending return, throw or break.
            List<PathAlternative> completed = new ArrayList<>(alreadyCompleted);
            for (PathAlternative pendingCompletion : incoming) {
                PathAlternative activePath = pendingCompletion.copy();
                activePath.live = true;
                activePath.exceptional = false;
                activePath.pendingExceptionType = null;
                activePath.jumpTarget = null;
                paths = new ArrayList<>(List.of(activePath));
                scan(tree.getFinallyBlock(), unused);
                for (PathAlternative path : paths) {
                    if (path.live) {
                        path.live = pendingCompletion.live;
                        path.exceptional = pendingCompletion.exceptional;
                        path.pendingExceptionType = pendingCompletion.pendingExceptionType;
                        path.jumpTarget = pendingCompletion.jumpTarget;
                    }
                    completed.add(path);
                }
            }
            paths = deduplicatePaths(completed);
            return null;
        }

        private TypeElement thrownType(ExpressionTree expression, TreePath parent) {
            TreePath path = TreePath.getPath(parent, expression);
            if (path == null) return null;
            TypeMirror mirror = trees.getTypeMirror(path);
            if (mirror instanceof DeclaredType declared && declared.asElement() instanceof TypeElement type) return type;
            return null;
        }

        private boolean matchesCatch(PathAlternative path, CatchTree catcher) {
            if (path.pendingExceptionType == null) {
                limitations.add(result.id + ": catch matching is unknown for unresolved thrown type");
                return false;
            }
            for (TypeElement caught : catchTypes(catcher)) {
                if (isSubtype(path.pendingExceptionType, caught, Collections.newSetFromMap(new IdentityHashMap<>()))) return true;
            }
            return false;
        }

        private List<TypeElement> catchTypes(CatchTree catcher) {
            List<TypeElement> result = new ArrayList<>();
            Tree typeTree = catcher.getParameter().getType();
            if (typeTree instanceof UnionTypeTree union) {
                for (Tree alternative : union.getTypeAlternatives()) addCatchType(result, alternative);
            } else addCatchType(result, typeTree);
            return result;
        }

        private void addCatchType(List<TypeElement> types, Tree typeTree) {
            TreePath path = TreePath.getPath(getCurrentPath(), typeTree);
            if (path == null) return;
            TypeMirror mirror = trees.getTypeMirror(path);
            if (mirror instanceof DeclaredType declared && declared.asElement() instanceof TypeElement type) types.add(type);
        }

        private boolean isSubtype(TypeElement actual, TypeElement expected, Set<Element> seen) {
            if (!seen.add(actual)) return false;
            if (actual.equals(expected)) return true;
            TypeMirror superclass = actual.getSuperclass();
            if (superclass instanceof DeclaredType declared && declared.asElement() instanceof TypeElement parent
                    && isSubtype(parent, expected, seen)) return true;
            for (TypeMirror implemented : actual.getInterfaces()) {
                if (implemented instanceof DeclaredType declared && declared.asElement() instanceof TypeElement parent
                        && isSubtype(parent, expected, seen)) return true;
            }
            return false;
        }

        @Override public Void visitVariable(VariableTree tree, Void unused) {
            tick();
            if (tree.getInitializer() != null) {
                ExprFacts facts = expression(tree.getInitializer(), getCurrentPath());
                Element element = trees.getElement(getCurrentPath());
                if (element != null) {
                    if (facts.connected) connected.add(element); else connected.remove(element);
                    locals.put(element, facts);
                }
                scan(tree.getInitializer(), unused);
            }
            return null;
        }

        @Override public Void visitAssignment(AssignmentTree tree, Void unused) {
            tick();
            ExprFacts right = expression(tree.getExpression(), getCurrentPath());
            Element target = element(tree.getVariable(), getCurrentPath());
            if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement()) && ownField(tree.getVariable())) {
                stateEffects++;
                if (right.connected && right.transform) {
                    transformation(tree.getExpression(), JavaDepthRoles.fieldID(field), true);
                }
                if (right.connected) result.categories.addAll(right.categories);
                if (!constructor && scalarPrivate(field) && ((right.transform && right.reads.contains(field))
                        || booleanTransition(field, tree.getExpression()))) {
                    credit("C", JavaDepthRoles.fieldID(field));
                    result.addFieldRoot("C", field);
                }
            } else if (target != null && !target.getKind().isField()) {
                if (right.connected) connected.add(target); else connected.remove(target);
                locals.put(target, right);
            }
            scan(tree.getExpression(), unused);
            return null;
        }

        @Override public Void visitCompoundAssignment(CompoundAssignmentTree tree, Void unused) {
            tick();
            if (JavaDepthMinimumExpressions.neutralUpdate(tree, getCurrentPath(), trees)) return null;
            ExprFacts left = expression(tree.getVariable(), getCurrentPath());
            ExprFacts right = expression(tree.getExpression(), getCurrentPath());
            Element target = element(tree.getVariable(), getCurrentPath());
            if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement()) && ownField(tree.getVariable())) {
                stateEffects++;
                if (!constructor && scalarPrivate(field) && left.reads.contains(field)) {
                    credit("C", JavaDepthRoles.fieldID(field));
                    result.addFieldRoot("C", field);
                }
                if (left.connected || right.connected) {
                    transformation(tree.getExpression(), JavaDepthRoles.fieldID(field), true);
                }
                if (right.connected) result.categories.addAll(right.categories);
            } else if (target != null && !target.getKind().isField()) {
                left.merge(right);
                left.transform = left.connected;
                locals.put(target, left);
                if (left.connected) connected.add(target); else connected.remove(target);
            }
            scan(tree.getExpression(), unused);
            return null;
        }

        @Override public Void visitUnary(UnaryTree tree, Void unused) {
            tick();
            if (tree.getKind() == Tree.Kind.PREFIX_INCREMENT || tree.getKind() == Tree.Kind.PREFIX_DECREMENT
                    || tree.getKind() == Tree.Kind.POSTFIX_INCREMENT || tree.getKind() == Tree.Kind.POSTFIX_DECREMENT) {
                Element target = element(tree.getExpression(), getCurrentPath());
                if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement()) && ownField(tree.getExpression())) {
                    stateEffects++;
                    if (!constructor && scalarPrivate(field)) {
                        credit("C", JavaDepthRoles.fieldID(field));
                        result.addFieldRoot("C", field);
                    }
                    if (!constructor) {
                        transformation(tree.getExpression(), JavaDepthRoles.fieldID(field), true);
                    }
                } else if (target != null && !target.getKind().isField()) {
                    ExprFacts value = expression(tree.getExpression(), getCurrentPath());
                    value.transform = value.connected;
                    locals.put(target, value);
                }
            }
            return super.visitUnary(tree, unused);
        }

        @Override public Void visitIf(IfTree tree, Void unused) {
            tick();
            Object constant = JavaDepthMinimumExpressions.constant(tree.getCondition(), getCurrentPath(), trees);
            if (constant instanceof Boolean selected) {
                scan(selected ? tree.getThenStatement() : tree.getElseStatement(), unused);
                return null;
            }
            ExprFacts condition = expression(tree.getCondition(), getCurrentPath());
            Set<VariableElement> previous = new HashSet<>(controllingFields);
            controllingFields.addAll(condition.reads);
            Map<Element, ExprFacts> before = new IdentityHashMap<>(locals);
            Set<Element> beforeConnected = new HashSet<>(connected);
            Map<VariableElement, Boolean> priorState = new IdentityHashMap<>(booleanState);
            scan(tree.getCondition(), unused);
            if ((condition.connected || guardConnected(tree.getCondition()))
                    && (containsThrow(tree.getThenStatement())
                    || containsStatusReturn(tree))) {
                // Both accepted and rejected values pass through this validation.
                credit("V", owner.getQualifiedName() + "/validation");
            }
            List<PathAlternative> beforePaths = copyPaths(paths);
            assumeBoolean(tree.getCondition(), true);
            paths = copyPaths(beforePaths);
            scan(tree.getThenStatement(), unused);
            List<PathAlternative> thenPaths = paths;
            Map<Element, ExprFacts> thenLocals = new IdentityHashMap<>(locals);
            locals.clear(); locals.putAll(before);
            connected.clear(); connected.addAll(beforeConnected);
            booleanState.clear(); booleanState.putAll(priorState);
            paths = copyPaths(beforePaths);
            assumeBoolean(tree.getCondition(), false);
            scan(tree.getElseStatement(), unused);
            mergePaths(thenPaths, paths);
            // Retain only facts supported on both alternatives after a branch.
            locals.entrySet().removeIf(entry -> !sameExpressionFacts(entry.getValue(), thenLocals.get(entry.getKey())));
            connected.removeIf(element -> !element.getKind().isField() && !locals.containsKey(element)
                    && !beforeConnected.contains(element));
            controllingFields.clear(); controllingFields.addAll(previous);
            booleanState.clear(); booleanState.putAll(priorState);
            return null;
        }

        private boolean guardConnected(ExpressionTree condition) {
            TreePath path = TreePath.getPath(getCurrentPath(), condition);
            if (path == null) return false;
            final boolean[] found = {false};
            new TreePathScanner<Void, Void>() {
                @Override public Void visitIdentifier(IdentifierTree tree, Void unused) {
                    tick();
                    Element element = trees.getElement(getCurrentPath());
                    if (element != null && connected.contains(element)) found[0] = true;
                    return super.visitIdentifier(tree, unused);
                }

                @Override public Void visitMemberSelect(MemberSelectTree tree, Void unused) {
                    tick();
                    Element element = trees.getElement(getCurrentPath());
                    if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                            && field.getConstantValue() == null && ownField(tree)) found[0] = true;
                    return super.visitMemberSelect(tree, unused);
                }
            }.scan(path, null);
            return found[0];
        }

        /**
         * A connected guard that returns a different status/value on one
         * route is an observed validation boundary, even when it does not
         * throw.  Compare resolved/constant return expressions rather than
         * relying on names such as status or error.
         */
        private boolean containsStatusReturn(IfTree tree) {
            List<ExpressionTree> thenReturns = returnExpressions(tree.getThenStatement());
            List<ExpressionTree> elseReturns = returnExpressions(tree.getElseStatement());
            if (contrastingReturns(thenReturns, elseReturns)) return true;
            if (thenReturns.isEmpty() == elseReturns.isEmpty()) return false;
            List<ExpressionTree> continuation = followingReturns();
            return contrastingReturns(thenReturns.isEmpty() ? elseReturns : thenReturns, continuation);
        }

        private boolean contrastingReturns(List<ExpressionTree> first, List<ExpressionTree> second) {
            if (first.isEmpty() || second.isEmpty()) return false;
            TreePath parent = getCurrentPath();
            for (ExpressionTree left : first) {
                String leftKey = returnKey(left, parent);
                if (leftKey == null) continue;
                for (ExpressionTree right : second) {
                    String rightKey = returnKey(right, parent);
                    if (rightKey != null && !leftKey.equals(rightKey)) return true;
                }
            }
            return false;
        }

        private List<ExpressionTree> returnExpressions(Tree tree) {
            if (tree == null) return List.of();
            List<ExpressionTree> result = new ArrayList<>();
            new TreeScanner<Void, Void>() {
                @Override public Void visitReturn(ReturnTree node, Void unused) {
                    if (node.getExpression() != null) result.add(node.getExpression());
                    return null;
                }
                @Override public Void visitClass(ClassTree node, Void unused) { return null; }
                @Override public Void visitLambdaExpression(LambdaExpressionTree node, Void unused) { return null; }
            }.scan(tree, null);
            return result;
        }

        private List<ExpressionTree> followingReturns() {
            TreePath current = getCurrentPath();
            TreePath parent = current == null ? null : current.getParentPath();
            if (parent == null || !(parent.getLeaf() instanceof BlockTree block)) return List.of();
            List<? extends StatementTree> statements = block.getStatements();
            int index = statements.indexOf(current.getLeaf());
            if (index < 0) return List.of();
            List<ExpressionTree> result = new ArrayList<>();
            for (int next = index + 1; next < statements.size(); next++) {
                result.addAll(returnExpressions(statements.get(next)));
            }
            return result;
        }

        private String returnKey(ExpressionTree expression, TreePath parent) {
            Object constant = JavaDepthMinimumExpressions.constant(expression, parent, trees);
            if (constant != null) return "constant:" + constant.getClass().getName() + ":" + constant;
            TreePath path = TreePath.getPath(parent, expression);
            if (path == null) return expression.getKind() + ":" + expression;
            JavaDepthComputationIdentity.Result normalized = computations.assess(path, owner);
            if (normalized.identity() != null) return normalized.identity();
            Element element = trees.getElement(path);
            if (element instanceof VariableElement variable) {
                return "value:" + variable.getEnclosingElement() + "#" + variable.getSimpleName()
                        + ":" + variable.asType();
            }
            return expression.getKind() + ":" + expression;
        }

        private boolean booleanTransition(VariableElement field, ExpressionTree value) {
            Object assigned = JavaDepthMinimumExpressions.constant(value, getCurrentPath(), trees);
            return assigned instanceof Boolean bool && booleanState.containsKey(field) && booleanState.get(field) != bool;
        }

        private void assumeBoolean(ExpressionTree condition, boolean value) {
            if (condition instanceof ParenthesizedTree wrapped) { assumeBoolean(wrapped.getExpression(), value); return; }
            if (condition instanceof UnaryTree unary && unary.getKind() == Tree.Kind.LOGICAL_COMPLEMENT) {
                assumeBoolean(unary.getExpression(), !value); return;
            }
            Element target = element(condition, getCurrentPath());
            if (target instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                    && field.asType().getKind() == TypeKind.BOOLEAN && ownField(condition)) booleanState.put(field, value);
        }

        private boolean ownField(ExpressionTree tree) {
            return tree instanceof IdentifierTree || tree instanceof MemberSelectTree member && isThis(member.getExpression());
        }

        private void transformation(ExpressionTree expression, String outcome, boolean effect) {
            TreePath path = TreePath.getPath(getCurrentPath(), expression);
            String computation = path == null ? null : computations.assess(path, owner).identity();
            if (computation == null) computation = "unresolved:" + result.id + ":" + expression;
            // A field's observable state outcome is one responsibility, regardless
            // of how many intermediate writes implement it.
            String root = effect ? owner.getQualifiedName() + "/state-transformation/" + outcome
                    : owner.getQualifiedName() + "/computation/" + outcome + "/" + computation;
            result.transformationRoots.add(root);
            credit("X", root);
            if (effect) result.effectTransformationRoots.add(root);
        }

        void creditCoordination(String monitor) {
            if (monitor == null) return;
            for (PathAlternative path : paths) {
                for (String credit : new ArrayList<>(path.credits)) {
                    int separator = credit.indexOf('\u0000');
                    if (separator <= 0 || !credit.startsWith("C\u0000")) continue;
                    String field = credit.substring(separator + 1);
                    if (!monitors.protects(field, monitor)) continue;
                    String root = field + "/monitor/" + monitor;
                    result.credit("Y", root);
                    path.credits.add("Y\u0000" + root);
                    break;
                }
            }
        }

        private void credit(String category, String root) {
            result.credit(category, root);
            if (root == null || root.isEmpty()) return;
            String value = category + "\u0000" + root;
            for (PathAlternative path : paths) if (path.live) path.credits.add(value);
        }

        private void terminatePaths(boolean exceptional) {
            for (PathAlternative path : paths) if (path.live) {
                path.live = false;
                path.exceptional = exceptional;
            }
        }

        private List<PathAlternative> copyPaths(List<PathAlternative> source) {
            List<PathAlternative> result = new ArrayList<>(source.size());
            for (PathAlternative path : source) result.add(path.copy());
            return result;
        }

        private void mergePaths(List<PathAlternative> thenPaths, List<PathAlternative> elsePaths) {
            Map<String, PathAlternative> unique = new TreeMap<>();
            for (PathAlternative path : thenPaths) unique.put(path.key(), path);
            for (PathAlternative path : elsePaths) unique.putIfAbsent(path.key(), path);
            paths = new ArrayList<>(unique.values());
            if (paths.size() > 32) {
                exhausted = true;
                limitations.add(result.id + ": branch alternatives exceeded 32");
                paths = new ArrayList<>(paths.subList(0, 32));
            }
        }

        private List<PathAlternative> deduplicatePaths(List<PathAlternative> candidates) {
            Map<String, PathAlternative> unique = new TreeMap<>();
            for (PathAlternative path : candidates) unique.putIfAbsent(path.key(), path);
            List<PathAlternative> result = new ArrayList<>(unique.values());
            if (result.size() > 32) {
                exhausted = true;
                limitations.add(resultId() + ": branch alternatives exceeded 32");
                return new ArrayList<>(result.subList(0, 32));
            }
            return result;
        }

        private String resultId() { return result.id; }

        private void mergeHelperAlternatives(List<Set<String>> helperPaths) {
            if (helperPaths.isEmpty()) return;
            Map<String, PathAlternative> merged = new TreeMap<>();
            for (PathAlternative caller : paths) {
                if (!caller.live) {
                    merged.putIfAbsent(caller.key(), caller.copy());
                    continue;
                }
                for (Set<String> helper : helperPaths) {
                    PathAlternative combined = caller.copy();
                    combined.credits.addAll(helper);
                    merged.putIfAbsent(combined.key(), combined);
                }
            }
            paths = new ArrayList<>(merged.values());
            if (paths.size() > 32) {
                exhausted = true;
                limitations.add(result.id + ": delegated alternatives exceeded 32");
                paths = new ArrayList<>(paths.subList(0, 32));
            }
        }

        private boolean sameExpressionFacts(ExprFacts first, ExprFacts second) {
            return second != null && first.connected == second.connected && first.transform == second.transform
                    && first.reads.equals(second.reads) && first.categories.equals(second.categories);
        }

        @Override public Void visitClass(ClassTree tree, Void unused) { return null; }
        @Override public Void visitLambdaExpression(LambdaExpressionTree tree, Void unused) { return null; }

        @Override public Void visitBreak(BreakTree tree, Void unused) {
            tick();
            if (tree.getLabel() != null || breakTargets.isEmpty()) {
                unsupportedControl(tree);
                return null;
            }
            Tree target = breakTargets.peek();
            for (PathAlternative path : paths) if (path.live) {
                path.live = false;
                path.jumpTarget = target;
            }
            return null;
        }

        @Override public Void visitSwitch(SwitchTree tree, Void unused) {
            tick();
            scan(tree.getExpression(), unused);
            List<PathAlternative> entry = copyPaths(paths);
            List<PathAlternative> fallthrough = new ArrayList<>();
            List<PathAlternative> exits = new ArrayList<>();
            boolean hasDefault = false;
            Object selector = JavaDepthMinimumExpressions.constant(tree.getExpression(), getCurrentPath(), trees);
            if (selector instanceof Character character) selector = (int) character;
            CaseTree selected = null, defaultCase = null;
            if (selector != null) {
                for (CaseTree branch : tree.getCases()) {
                    if (branch.getExpressions().isEmpty()) defaultCase = branch;
                    for (ExpressionTree label : branch.getExpressions()) {
                        Object value = JavaDepthMinimumExpressions.constant(label, getCurrentPath(), trees);
                        if (value instanceof Character character) value = (int) character;
                        if (Objects.equals(selector, value)) selected = branch;
                    }
                }
                if (selected == null) selected = defaultCase;
            }
            breakTargets.push(tree);
            try {
                for (CaseTree branch : tree.getCases()) {
                    if (branch.getExpression() == null) hasDefault = true;
                    paths = new ArrayList<>();
                    if (selector == null || branch == selected) paths.addAll(copyPaths(entry));
                    paths.addAll(copyPaths(fallthrough));
                    if (branch.getBody() != null) scan(branch.getBody(), unused);
                    else scan(branch.getStatements(), unused);
                    List<PathAlternative> next = new ArrayList<>();
                    for (PathAlternative path : paths) {
                        if (path.jumpTarget == tree) {
                            path.jumpTarget = null;
                            path.live = true;
                            exits.add(path);
                        } else if (!path.live || path.jumpTarget != null) {
                            exits.add(path);
                        } else {
                            next.add(path);
                        }
                    }
                    if (branch.getBody() != null) exits.addAll(next);
                    else fallthrough = next;
                }
            } finally {
                breakTargets.pop();
            }
            exits.addAll(fallthrough);
            if (!hasDefault && (selector == null || selected == null)) exits.addAll(copyPaths(entry));
            paths = deduplicatePaths(exits);
            return null;
        }

        @Override public Void visitBinary(BinaryTree tree, Void unused) {
            if (tree.getKind() != Tree.Kind.CONDITIONAL_AND && tree.getKind() != Tree.Kind.CONDITIONAL_OR) {
                return super.visitBinary(tree, unused);
            }
            tick();
            scan(tree.getLeftOperand(), unused);
            Object left = JavaDepthMinimumExpressions.constant(tree.getLeftOperand(), getCurrentPath(), trees);
            if (left instanceof Boolean value) {
                if (value == (tree.getKind() == Tree.Kind.CONDITIONAL_AND)) scan(tree.getRightOperand(), unused);
                return null;
            }
            List<PathAlternative> skipped = copyPaths(paths);
            Map<Element, ExprFacts> before = new IdentityHashMap<>(locals);
            Set<Element> beforeConnected = new HashSet<>(connected);
            scan(tree.getRightOperand(), unused);
            mergePaths(skipped, paths);
            locals.entrySet().removeIf(entry -> !sameExpressionFacts(entry.getValue(), before.get(entry.getKey())));
            connected.clear(); connected.addAll(beforeConnected);
            return null;
        }

        @Override public Void visitConditionalExpression(ConditionalExpressionTree tree, Void unused) {
            tick();
            scan(tree.getCondition(), unused);
            Object value = JavaDepthMinimumExpressions.constant(tree.getCondition(), getCurrentPath(), trees);
            if (value instanceof Boolean selected) {
                scan(selected ? tree.getTrueExpression() : tree.getFalseExpression(), unused);
                return null;
            }
            List<PathAlternative> beforePaths = copyPaths(paths);
            Map<Element, ExprFacts> before = new IdentityHashMap<>(locals);
            Set<Element> beforeConnected = new HashSet<>(connected);
            scan(tree.getTrueExpression(), unused);
            List<PathAlternative> truePaths = paths;
            Map<Element, ExprFacts> trueLocals = new IdentityHashMap<>(locals);
            paths = copyPaths(beforePaths);
            locals.clear(); locals.putAll(before);
            connected.clear(); connected.addAll(beforeConnected);
            scan(tree.getFalseExpression(), unused);
            mergePaths(truePaths, paths);
            locals.entrySet().removeIf(entry -> !sameExpressionFacts(entry.getValue(), trueLocals.get(entry.getKey())));
            connected.clear(); connected.addAll(beforeConnected);
            return null;
        }

        @Override public Void visitWhileLoop(WhileLoopTree tree, Void unused) {
            scanLoop(tree.getCondition(), tree.getStatement(), List.of(), tree, unused);
            return null;
        }

        @Override public Void visitForLoop(ForLoopTree tree, Void unused) {
            scan(tree.getInitializer(), unused);
            scanLoop(tree.getCondition(), tree.getStatement(), tree.getUpdate(), tree, unused);
            return null;
        }

        @Override public Void visitEnhancedForLoop(EnhancedForLoopTree tree, Void unused) {
            scan(tree.getExpression(), unused);
            scan(tree.getVariable(), unused);
            // The supplied collection may be empty.
            scanLoop(null, tree.getStatement(), List.of(), tree, unused);
            return null;
        }

        private void scanLoop(ExpressionTree condition, StatementTree body,
                              List<? extends StatementTree> updates, Tree loop, Void unused) {
            tick();
            Object constant = condition == null ? null : JavaDepthMinimumExpressions.constant(condition, getCurrentPath(), trees);
            scan(condition, unused);
            if (Boolean.FALSE.equals(constant)) return;
            List<PathAlternative> skipped = copyPaths(paths);
            Map<Element, ExprFacts> before = new IdentityHashMap<>(locals);
            Set<Element> beforeConnected = new HashSet<>(connected);
            breakTargets.push(loop);
            try {
                scan(body, unused);
            } finally {
                breakTargets.pop();
            }
            scan(updates, unused);
            for (PathAlternative path : paths) if (path.jumpTarget == loop) {
                path.jumpTarget = null;
                path.live = true;
            }
            if (!Boolean.TRUE.equals(constant)) mergePaths(skipped, paths);
            locals.entrySet().removeIf(entry -> !sameExpressionFacts(entry.getValue(), before.get(entry.getKey())));
            connected.clear(); connected.addAll(beforeConnected);
            limitations.add(result.id + ": loop uses bounded skipped/body alternatives; repeated iterations are not proved");
        }

        @Override public Void visitSynchronized(SynchronizedTree tree, Void unused) {
            tick();
            int priorStateEffects = stateEffects;
            String monitor = monitors.monitor(tree.getExpression(), getCurrentPath());
            super.visitSynchronized(tree, unused);
            if (stateEffects > priorStateEffects) creditCoordination(monitor);
            return null;
        }

        @Override public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) {
            tick();
            scan(tree.getMethodSelect(), unused);
            scan(tree.getArguments(), unused);
            if (paths.stream().noneMatch(path -> path.live)) return null;
            Element target = trees.getElement(TreePath.getPath(getCurrentPath(), tree));
            if (target instanceof ExecutableElement called && sourceDelegate(tree, called)) {
                sourceDelegations.add(callableID(called));
                Summary helper = JavaDepthMinimumBehavior.this.summary(called);
                String receiverRoot = delegateReceiverRoot(tree, called);
                boolean connectedInputs = !tree.getArguments().isEmpty();
                for (ExpressionTree argument : tree.getArguments()) {
                    connectedInputs &= expression(argument, getCurrentPath()).connected;
                }
                List<Set<String>> effects = new ArrayList<>();
                for (Set<String> alternative : helper.alternatives) {
                    Set<String> selected = new TreeSet<>();
                    for (String item : alternative) {
                        int separator = item.indexOf('\u0000');
                        String category = item.substring(0, separator), root = item.substring(separator + 1);
                        if (!helper.effectCategories.contains(category)) continue;
                        if (category.equals("X") && !helper.effectTransformationRoots.contains(root)) continue;
                        // A helper's validation does not validate caller input when
                        // the call supplied constants or unresolved argument values.
                        if (category.equals("V") && !connectedInputs) continue;
                        String boundRoot = receiverRoot + root;
                        selected.add(category + "\u0000" + boundRoot);
                        result.credit(category, boundRoot);
                        result.effectCategories.add(category);
                        if (category.equals("X")) {
                            result.transformationRoots.add(boundRoot);
                            result.effectTransformationRoots.add(boundRoot);
                        }
                        if (category.equals("C")) {
                            result.fieldRoots.computeIfAbsent(category, ignored -> new TreeSet<>()).add(boundRoot);
                        }
                    }
                    effects.add(selected);
                }
                if (helper.onlyExceptional) terminatePaths(true);
                else mergeHelperAlternatives(effects);
            } else if (target instanceof ExecutableElement called
                    && called.getKind() == ElementKind.CONSTRUCTOR
                    && called.getEnclosingElement().toString().equals("java.lang.Object")) {
                // Object's zero-argument constructor has no user-defined effects.
            } else if (target instanceof ExecutableElement called && owner.equals(called.getEnclosingElement())) {
                limitations.add(result.id + ": delegation to " + called + " is not expanded");
            } else if (target instanceof ExecutableElement called && !owner.equals(called.getEnclosingElement())) {
                limitations.add(result.id + ": effects of external call " + called.getEnclosingElement() + "#" + called + " are unknown");
            } else if (!(target instanceof ExecutableElement)) {
                limitations.add(result.id + ": unresolved delegation is not credited");
            }
            return null;
        }

        private String delegateReceiverRoot(MethodInvocationTree invocation, ExecutableElement method) {
            if (method.getModifiers().contains(Modifier.STATIC)
                    || owner.equals(method.getEnclosingElement()) && exactReceiver(invocation)) return "";
            if (invocation.getMethodSelect() instanceof MemberSelectTree select) {
                TreePath path = TreePath.getPath(getCurrentPath(), select.getExpression());
                if (path != null && trees.getElement(path) instanceof VariableElement field) {
                    return JavaDepthRoles.fieldID(field) + "/delegate/";
                }
            }
            return "";
        }

        private boolean sourceDelegate(MethodInvocationTree invocation, ExecutableElement method) {
            if (method.getKind() != ElementKind.METHOD || method.isVarArgs()) return false;
            boolean localOwner = owner.equals(method.getEnclosingElement());
            // Expand implementation helpers, not every public service they use.
            // Public API graphs can span most of a monorepo and must use shared
            // semantic summaries before they are eligible for body expansion.
            boolean finalDeclaringType = method.getEnclosingElement() instanceof TypeElement declaring
                    && declaring.getModifiers().contains(Modifier.FINAL);
            boolean exactDispatch = method.getModifiers().contains(Modifier.STATIC)
                    || method.getModifiers().contains(Modifier.PRIVATE)
                    || method.getModifiers().contains(Modifier.FINAL) || finalDeclaringType;
            if (!exactDispatch) return false;
            if (!localOwner && (!JavaDepthCalls.samePackage(method, owner)
                    || sourceDelegations.size() >= 16 || active.size() >= 8)) return false;
            TreePath declaration = trees.getPath(method);
            if (declaration == null || !(declaration.getLeaf() instanceof MethodTree body)
                    || body.getBody() == null) return false;
            if (method.getModifiers().contains(Modifier.STATIC)) return true;
            if (!(method.getEnclosingElement() instanceof TypeElement declaring)) return false;
            boolean exact = method.getModifiers().contains(Modifier.PRIVATE)
                    || method.getModifiers().contains(Modifier.FINAL)
                    || declaring.getModifiers().contains(Modifier.FINAL);
            if (!exact) return false;
            if (owner.equals(declaring) && exactReceiver(invocation)) return true;
            if (!(invocation.getMethodSelect() instanceof MemberSelectTree select)) return false;
            TreePath receiverPath = TreePath.getPath(getCurrentPath(), select.getExpression());
            Element receiver = receiverPath == null ? null : trees.getElement(receiverPath);
            if (!(receiver instanceof VariableElement field) || !owner.equals(field.getEnclosingElement())
                    || !field.getModifiers().containsAll(Set.of(Modifier.PRIVATE, Modifier.FINAL))
                    || !ownField(select.getExpression())) return false;
            TreePath fieldPath = trees.getPath(field);
            if (fieldPath == null || !(fieldPath.getLeaf() instanceof VariableTree variable)
                    || !(variable.getInitializer() instanceof NewClassTree allocation)
                    || allocation.getClassBody() != null) return false;
            Element constructor = trees.getElement(TreePath.getPath(fieldPath, allocation));
            return constructor instanceof ExecutableElement created && declaring.equals(created.getEnclosingElement());
        }

        private boolean exactReceiver(MethodInvocationTree tree) {
            ExpressionTree select = tree.getMethodSelect();
            if (select instanceof IdentifierTree) return true;
            return select instanceof MemberSelectTree member && isThis(member.getExpression());
        }

        private Element element(Tree tree, TreePath parent) {
            TreePath path = TreePath.getPath(parent, tree);
            return path == null ? null : trees.getElement(path);
        }

        private ExprFacts expression(ExpressionTree tree, TreePath parent) {
            ExprFacts facts = new ExprFacts();
            TreePath path = TreePath.getPath(parent, tree);
            if (path == null) return facts;
            new ExpressionScanner(facts).scan(path, null);
            JavaDepthComputationIdentity.Result normalized = computations.assess(path, owner);
            if (normalized.identity() != null) {
                facts.reads.clear();
                facts.reads.addAll(normalized.stateReads());
                facts.connected = normalized.connected();
                facts.transform = normalized.transformed();
                facts.categories.remove("X");
            } else if (tree instanceof ConditionalExpressionTree || tree instanceof MethodInvocationTree) {
                // Unsupported branch/call syntax is not evidence of a returned transformation.
                facts.transform = false;
                facts.categories.remove("X");
                limitations.add(result.id + ": returned expression cannot be normalized; no transformation credit inferred");
            }
            return facts;
        }

        private boolean containsThrow(Tree tree) {
            if (tree == null) return false;
            final boolean[] found = {false};
            TreePath path = TreePath.getPath(getCurrentPath(), tree);
            if (path == null) return false;
            new TreePathScanner<Void, Void>() {
                @Override public Void visitThrow(ThrowTree node, Void unused) {
                    if (recognizedError(node.getExpression(), getCurrentPath())) found[0] = true;
                    return null;
                }
                @Override public Void visitClass(ClassTree node, Void unused) { return null; }
                @Override public Void visitLambdaExpression(LambdaExpressionTree node, Void unused) { return null; }
                @Override public Void visitIf(IfTree node, Void unused) {
                    Object value = JavaDepthMinimumExpressions.constant(node.getCondition(), getCurrentPath(), trees);
                    if (value instanceof Boolean selected) {
                        scan(selected ? node.getThenStatement() : node.getElseStatement(), unused);
                        return null;
                    }
                    return super.visitIf(node, unused);
                }
            }.scan(path, null);
            return found[0];
        }

        private boolean recognizedError(ExpressionTree expression, TreePath parent) {
            if (!(expression instanceof NewClassTree created)) return false;
            TreePath path = TreePath.getPath(parent, created);
            Element element = path == null ? null : trees.getElement(path);
            if (!(element instanceof ExecutableElement constructor)
                    || constructor.getKind() != ElementKind.CONSTRUCTOR
                    || !(constructor.getEnclosingElement() instanceof TypeElement type)
                    || !type.getQualifiedName().contentEquals("java.lang.IllegalArgumentException")
                    || !javaBase(type)) return false;
            List<? extends ExpressionTree> arguments = created.getArguments();
            if (arguments.isEmpty()) return true;
            if (arguments.size() != 1) return false;
            TreePath argumentPath = TreePath.getPath(parent, arguments.get(0));
            TypeMirror argumentType = argumentPath == null ? null : trees.getTypeMirror(argumentPath);
            if (argumentType == null || !argumentType.toString().equals("java.lang.String")) return false;
            ExpressionTree argument = arguments.get(0);
            if (argument instanceof LiteralTree literal) return literal.getValue() instanceof String;
            Element value = argumentPath == null ? null : trees.getElement(argumentPath);
            if (!(value instanceof VariableElement variable)
                    || !(variable.getConstantValue() instanceof String)
                    || !(argument instanceof IdentifierTree || argument instanceof MemberSelectTree member
                    && typeElement(TreePath.getPath(parent, member.getExpression())))) return false;
            return true;
        }

        private boolean typeElement(TreePath path) {
            return path != null && trees.getElement(path) instanceof TypeElement;
        }

        private boolean javaBase(TypeElement type) {
            Element enclosing = type;
            while (enclosing != null && enclosing.getKind() != ElementKind.MODULE) {
                enclosing = enclosing.getEnclosingElement();
            }
            return enclosing instanceof ModuleElement module
                    && module.getQualifiedName().contentEquals("java.base");
        }

        private boolean scalarPrivate(VariableElement field) {
            TypeKind kind = field.asType().getKind();
            return field.getModifiers().contains(Modifier.PRIVATE) && !field.getModifiers().contains(Modifier.STATIC)
                    && (kind == TypeKind.BYTE || kind == TypeKind.SHORT || kind == TypeKind.INT
                    || kind == TypeKind.LONG || kind == TypeKind.CHAR || kind == TypeKind.FLOAT
                    || kind == TypeKind.DOUBLE || kind == TypeKind.BOOLEAN);
        }

        private void tick() {
            if (++work > MAX_WORK) {
                exhausted = true;
                throw new WorkLimit();
            }
        }

        private final class ExpressionScanner extends TreePathScanner<Void, Void> {
            private final ExprFacts facts;
            ExpressionScanner(ExprFacts facts) { this.facts = facts; }
            @Override public Void visitIdentifier(IdentifierTree tree, Void unused) {
                tick();
                Element element = trees.getElement(getCurrentPath());
                if (element != null && connected.contains(element)) facts.connected = true;
                if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())) {
                    facts.reads.add(field);
                }
                if (element != null && locals.containsKey(element)) facts.merge(locals.get(element));
                return super.visitIdentifier(tree, unused);
            }
            @Override public Void visitMemberSelect(MemberSelectTree tree, Void unused) {
                tick();
                Element element = trees.getElement(getCurrentPath());
                if (element instanceof VariableElement field && owner.equals(field.getEnclosingElement())
                        && ownField(tree) && field.getConstantValue() == null) {
                    if (!constructor) facts.connected = true;
                    facts.reads.add(field);
                }
                return super.visitMemberSelect(tree, unused);
            }
            @Override public Void scan(Tree tree, Void unused) {
                if (tree == null) return null;
                tick();
                return super.scan(tree, unused);
            }
            @Override public Void visitBinary(BinaryTree tree, Void unused) {
                if (JavaDepthMinimumExpressions.constant(tree, getCurrentPath(), trees) != null) return null;
                ExpressionTree identity = JavaDepthMinimumExpressions.identity(tree, getCurrentPath(), trees);
                if (identity != null) {
                    scan(identity, unused);
                    return null;
                }
                if (isTransform(tree.getKind())) facts.transform = true;
                return super.visitBinary(tree, unused);
            }
            @Override public Void visitConditionalExpression(ConditionalExpressionTree tree, Void unused) {
                // Branch syntax alone says nothing about the returned computation.
                normalizedReturn(getCurrentPath());
                return null;
            }
            @Override public Void visitArrayAccess(ArrayAccessTree tree, Void unused) {
                facts.transform = true;
                return super.visitArrayAccess(tree, unused);
            }
            @Override public Void visitMethodInvocation(MethodInvocationTree tree, Void unused) {
                tick();
                // Substitute actual arguments into the exact helper return before
                // deciding whether its result depends on caller input.
                normalizedReturn(getCurrentPath());
                return null;
            }

            private void normalizedReturn(TreePath path) {
                JavaDepthComputationIdentity.Result normalized = computations.assess(path, owner);
                if (normalized.identity() == null) return;
                facts.reads.addAll(normalized.stateReads());
                facts.connected |= normalized.connected();
                facts.transform |= normalized.transformed();
            }

            private boolean exactReceiver(MethodInvocationTree tree) {
                ExpressionTree select = tree.getMethodSelect();
                if (select instanceof IdentifierTree) return true;
                return select instanceof MemberSelectTree member && isThis(member.getExpression());
            }
        }
    }

    private static final class PathAlternative {
        final Set<String> credits = new TreeSet<>();
        boolean live = true;
        boolean exceptional;
        TypeElement pendingExceptionType;
        Tree jumpTarget;

        PathAlternative copy() {
            PathAlternative result = new PathAlternative();
            result.credits.addAll(credits);
            result.live = live;
            result.exceptional = exceptional;
            result.pendingExceptionType = pendingExceptionType;
            result.jumpTarget = jumpTarget;
            return result;
        }

        String key() {
            String exception = pendingExceptionType == null ? "" : pendingExceptionType.getQualifiedName().toString();
            return String.join("\u0001", credits) + "\u0001" + live + "\u0001" + exceptional
                    + "\u0001" + exception + "\u0001" + System.identityHashCode(jumpTarget);
        }
    }

    private static final class ExprFacts {
        boolean connected;
        boolean transform;
        final Set<VariableElement> reads = Collections.newSetFromMap(new IdentityHashMap<>());
        final Set<String> categories = new TreeSet<>();
        void merge(ExprFacts other) {
            connected |= other.connected;
            transform |= other.transform;
            reads.addAll(other.reads);
            categories.addAll(other.categories);
        }
    }

    private static boolean isTransform(Tree.Kind kind) {
        return switch (kind) {
            case PLUS, MINUS, MULTIPLY, DIVIDE, REMAINDER, UNARY_PLUS, UNARY_MINUS,
                    EQUAL_TO, NOT_EQUAL_TO, LESS_THAN, LESS_THAN_EQUAL, GREATER_THAN,
                    GREATER_THAN_EQUAL, CONDITIONAL_AND, CONDITIONAL_OR, AND, OR, XOR,
                    LEFT_SHIFT, RIGHT_SHIFT, UNSIGNED_RIGHT_SHIFT -> true;
            default -> false;
        };
    }


    private static boolean isThis(ExpressionTree expression) {
        return expression instanceof IdentifierTree identifier
                && identifier.getName().contentEquals("this");
    }

    private static String callableID(ExecutableElement method) {
        return method.getKind() == ElementKind.CONSTRUCTOR
                ? JavaDepthRoles.constructorID(method) : JavaDepthRoles.methodID(method);
    }

    private static final class WorkLimit extends RuntimeException { }
}
