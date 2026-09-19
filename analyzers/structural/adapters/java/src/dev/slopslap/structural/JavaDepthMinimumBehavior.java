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
    static final int MAX_WORK = 8192;
    final Trees trees;
    final JavaDepthComputationIdentity.Session computations;
    final TreePath ownerPath;
    final TypeElement owner;
    final Map<ExecutableElement, JavaDepthMinimumSurface.Callable> methods;
    final Map<ExecutableElement, JavaDepthMinimumSurface.Callable> allMethods = new IdentityHashMap<>();
    final Map<ExecutableElement, Summary> cache = new IdentityHashMap<>();
    final Set<ExecutableElement> active = Collections.newSetFromMap(new IdentityHashMap<>());
    final Set<String> limitations = new TreeSet<>();
    final Map<TypeElement, JavaDepthMinimumMonitors> monitorCache = new IdentityHashMap<>();
    final Set<String> sourceDelegations = new TreeSet<>();
    int work;
    boolean exhausted;

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
            return ObligationAggregation.collect(summaries, owner);
        }

        List<List<String>> obligationAlternatives(TypeElement owner, String routeID) {
            return ObligationAggregation.alternatives(summaries, routeID);
        }
    }

    static final class ObligationAggregation {
        private ObligationAggregation() { }

        static List<Map<String, Object>> collect(List<Summary> summaries, TypeElement owner) {
            Map<String, Map<String, Object>> result = new TreeMap<>();
            for (Summary summary : summaries) addSummary(result, summary, owner);
            return new ArrayList<>(result.values());
        }

        private static void addSummary(Map<String, Map<String, Object>> result,
                                       Summary summary, TypeElement owner) {
            for (String category : summary.categories) addCategory(result, summary, owner, category);
        }

        private static void addCategory(Map<String, Map<String, Object>> result,
                                        Summary summary, TypeElement owner, String category) {
            for (String root : summary.roots(owner, category)) {
                merge(result, obligation(owner, category, root, summary.id), summary.id);
            }
        }

        private static void merge(Map<String, Map<String, Object>> result,
                                  Map<String, Object> obligation, String evidenceID) {
            String id = (String) obligation.get("id");
            Map<String, Object> existing = result.get(id);
            if (existing != null) {
                Set<String> evidence = new TreeSet<>();
                for (Object item : (List<?>) existing.get("evidence")) evidence.add((String) item);
                evidence.add(evidenceID);
                obligation = new LinkedHashMap<>(obligation);
                obligation.put("evidence", new ArrayList<>(evidence));
            }
            result.put(id, obligation);
        }

        static List<List<String>> alternatives(List<Summary> summaries, String routeID) {
            for (Summary summary : summaries) {
                if (summary.id.equals(routeID)) return alternatives(summary.alternatives);
            }
            return List.of(List.of());
        }

        private static List<List<String>> alternatives(List<Set<String>> alternatives) {
            List<List<String>> result = new ArrayList<>();
            for (Set<String> alternative : alternatives) result.add(alternativeIDs(alternative));
            return result;
        }

        private static List<String> alternativeIDs(Set<String> alternative) {
            List<String> ids = new ArrayList<>();
            for (String credit : alternative) {
                int separator = credit.indexOf('\u0000');
                if (separator <= 0 || separator == credit.length() - 1) continue;
                String category = credit.substring(0, separator);
                String root = credit.substring(separator + 1);
                ids.add(root + "/bounded/" + category);
            }
            Collections.sort(ids);
            return ids;
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

    Summary summary(ExecutableElement method) {
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
        JavaDepthMinimumBehaviorScanner scanner = new JavaDepthMinimumBehaviorScanner(this, result, method);
        try {
            scanner.scan(new TreePath(callable.path(), callable.tree().getBody()), null);
        } catch (WorkLimit ignored) {
            // The outer assessment remains useful for already visited routes.
        }
        if (method.getModifiers().contains(Modifier.SYNCHRONIZED) && scanner.context.stateEffects > 0) {
            scanner.creditCoordination(scanner.context.monitors.methodMonitor(method));
        }
        result.alternatives.clear();
        result.onlyExceptional = !scanner.context.paths.isEmpty() && scanner.context.paths.stream().allMatch(path -> path.exceptional);
        for (PathAlternative path : scanner.context.paths) if (!path.exceptional) result.alternatives.add(new TreeSet<>(path.credits));
        if (result.alternatives.isEmpty()) result.alternatives.add(new TreeSet<>());
        if (scanner.context.stateEffects > 0 && result.categories.contains("X")) result.effectCategories.add("X");
        if (!result.fieldRoots.isEmpty()) result.effectCategories.add("C");
        if (result.categories.contains("V")) result.effectCategories.add("V");
        if (result.categories.contains("Y")) result.effectCategories.add("Y");
        active.remove(method);
        return result;
    }



    static final class PathAlternative {
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

    static final class ExprFacts {
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

    static boolean isTransform(Tree.Kind kind) {
        return switch (kind) {
            case PLUS, MINUS, MULTIPLY, DIVIDE, REMAINDER, UNARY_PLUS, UNARY_MINUS,
                    EQUAL_TO, NOT_EQUAL_TO, LESS_THAN, LESS_THAN_EQUAL, GREATER_THAN,
                    GREATER_THAN_EQUAL, CONDITIONAL_AND, CONDITIONAL_OR, AND, OR, XOR,
                    LEFT_SHIFT, RIGHT_SHIFT, UNSIGNED_RIGHT_SHIFT -> true;
            default -> false;
        };
    }


    static boolean isThis(ExpressionTree expression) {
        return expression instanceof IdentifierTree identifier
                && identifier.getName().contentEquals("this");
    }

    static String callableID(ExecutableElement method) {
        return method.getKind() == ElementKind.CONSTRUCTOR
                ? JavaDepthRoles.constructorID(method) : JavaDepthRoles.methodID(method);
    }

}
