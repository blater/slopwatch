package dev.slopslap.structural;

import com.sun.source.tree.BlockTree;
import com.sun.source.tree.ExpressionTree;
import com.sun.source.tree.IfTree;
import com.sun.source.tree.IdentifierTree;
import com.sun.source.tree.MethodInvocationTree;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.NewClassTree;
import com.sun.source.tree.ReturnTree;
import com.sun.source.tree.StatementTree;
import com.sun.source.tree.ThrowTree;
import com.sun.source.tree.Tree;
import com.sun.source.util.TreePath;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;
import javax.lang.model.element.Element;
import javax.lang.model.element.ElementKind;
import javax.lang.model.element.ExecutableElement;
import javax.lang.model.element.Modifier;
import javax.lang.model.element.TypeElement;
import javax.lang.model.type.TypeKind;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** Groups forwarding entry routes by their resolved same-owner service target. */
final class JavaDepthRouteNormalization {
    private JavaDepthRouteNormalization() { }

    static void apply(Trees trees, TreePath ownerPath, TypeElement owner, List<Object> families) {
        Map<String, ExecutableElement> methods = methods(owner);
        if (methods.isEmpty() || families.isEmpty()) return;
        Map<String, List<Map<String, Object>>> grouped = new LinkedHashMap<>();
        List<Object> order = new ArrayList<>();
        Map<String, Boolean> seenGroups = new LinkedHashMap<>();
        for (Object value : families) {
            if (!(value instanceof Map<?, ?> family)) continue;
            Object rawRoutes = family.get("routes");
            if (!(rawRoutes instanceof List<?> routes)) {
                order.add(value);
                continue;
            }
            boolean methodFamily = false;
            for (Object rawRoute : routes) {
                if (!(rawRoute instanceof Map<?, ?> route)) continue;
                String routeID = string(route.get("id"));
                ExecutableElement method = methods.get(routeID);
                if (method == null) continue;
                methodFamily = true;
                Normalized normalized = normalize(trees, ownerPath, owner, method);
                if (normalized == null) normalized = self(method);
                Map<String, Object> copy = copy(route);
                copy.put("family", normalized.serviceID);
                copy.put("required_slots", normalized.required);
                copy.put("exposed_slots", normalized.exposed);
                if (!seenGroups.containsKey(normalized.serviceID)) {
                    seenGroups.put(normalized.serviceID, true);
                    order.add(normalized.serviceID);
                }
                grouped.computeIfAbsent(normalized.serviceID, ignored -> new ArrayList<>()).add(copy);
            }
            if (!methodFamily) order.add(value);
        }
        families.clear();
        for (Object item : order) {
            if (!(item instanceof String serviceID)) {
                families.add(item);
                continue;
            }
            List<Map<String, Object>> routes = grouped.get(serviceID);
            if (routes != null && !routes.isEmpty()) families.add(Map.of("id", serviceID, "routes", routes));
        }
    }

    private static Map<String, ExecutableElement> methods(TypeElement owner) {
        Map<String, ExecutableElement> result = new LinkedHashMap<>();
        for (Element element : owner.getEnclosedElements()) {
            if (!(element instanceof ExecutableElement method) || method.getKind() != ElementKind.METHOD) continue;
            result.put(JavaDepthRoles.methodID(method), method);
        }
        return result;
    }

    private static Normalized normalize(Trees trees, TreePath ownerPath, TypeElement owner,
                                        ExecutableElement method) {
        ExecutableElement target = forwardedTarget(trees, ownerPath, owner, method);
        if (target == null) return self(method);
        String serviceID = JavaDepthRoles.methodID(target);
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree)
                || tree.getBody() == null) return self(method);
        ReturnTree returned = returnedCall(trees, methodPath, method, tree.getBody());
        if (returned == null || !(returned.getExpression() instanceof MethodInvocationTree call)) return self(method);
        if (call.getArguments().size() != target.getParameters().size()) return self(method);
        boolean[] usedParameters = new boolean[method.getParameters().size()];
        List<String> required = new ArrayList<>();
        List<String> exposed = new ArrayList<>();
        for (int index = 0; index < target.getParameters().size(); index++) {
            if (index >= call.getArguments().size()) return self(method);
            ExpressionTree argument = call.getArguments().get(index);
            Integer dependency = parameterIndex(trees, methodPath, method, argument);
            if (dependency == null) {
                if (!typedLiteral(trees, methodPath, argument, target.getParameters().get(index).asType())) return self(method);
                continue;
            }
            if (!sameType(method.getParameters().get(dependency).asType(), target.getParameters().get(index).asType())
                    || !markOnce(usedParameters, dependency)) return self(method);
            String slot = serviceID + "/arg" + index;
            exposed.add(slot);
            required.add(slot);
        }
        for (boolean used : usedParameters) if (!used) return self(method);
        return new Normalized(serviceID, required, exposed);
    }

    private static ExecutableElement forwardedTarget(Trees trees, TreePath ownerPath, TypeElement owner,
                                                      ExecutableElement method) {
        if (method.getModifiers().contains(Modifier.ABSTRACT) || method.getModifiers().contains(Modifier.NATIVE)
                || method.getModifiers().contains(Modifier.SYNCHRONIZED)) return null;
        TreePath methodPath = trees.getPath(method);
        if (methodPath == null || !(methodPath.getLeaf() instanceof MethodTree tree) || tree.getBody() == null) return null;
        ReturnTree returned = returnedCall(trees, methodPath, method, tree.getBody());
        if (returned == null || !(returned.getExpression() instanceof MethodInvocationTree call)) return null;
        ExecutableElement target = JavaDepthCalls.resolve(trees, methodPath, call);
        if (target == null || target.getKind() != ElementKind.METHOD || target.equals(method)
                || !owner.equals(target.getEnclosingElement()) || !target.getModifiers().contains(Modifier.STATIC)
                || !JavaDepthCalls.staticSelection(trees, methodPath, call, target)
                || !JavaDepthCalls.exactArgumentTypes(trees, methodPath, call, target)) return null;
        return target;
    }

    private static ReturnTree returnedCall(Trees trees, TreePath methodPath, ExecutableElement method,
                                           BlockTree body) {
        List<? extends StatementTree> statements = body.getStatements();
        if (statements.isEmpty()) return null;
        StatementTree last = statements.get(statements.size() - 1);
        if (!(last instanceof ReturnTree returned) || !(returned.getExpression() instanceof MethodInvocationTree)) return null;
        for (int index = 0; index + 1 < statements.size(); index++) {
            if (!validationGuard(trees, methodPath, method, statements.get(index))) return null;
        }
        return returned;
    }

    private static boolean validationGuard(Trees trees, TreePath methodPath, ExecutableElement method,
                                           StatementTree statement) {
        if (!(statement instanceof IfTree branch) || branch.getElseStatement() != null) return false;
        return purePredicate(trees, methodPath, method, branch.getCondition())
                && throwsIllegalArgument(trees, methodPath, branch.getThenStatement());
    }

    private static boolean throwsIllegalArgument(Trees trees, TreePath methodPath, Tree tree) {
        if (tree instanceof ThrowTree thrown) return isIllegalArgument(trees, methodPath, thrown.getExpression());
        if (!(tree instanceof BlockTree block) || block.getStatements().size() != 1) return false;
        StatementTree statement = block.getStatements().get(0);
        return statement instanceof ThrowTree thrown && isIllegalArgument(trees, methodPath, thrown.getExpression());
    }

    private static boolean isIllegalArgument(Trees trees, TreePath methodPath, ExpressionTree expression) {
        return expression instanceof NewClassTree created
                && isIllegalArgumentType(trees, methodPath, created)
                && created.getArguments().stream().allMatch(JavaDepthRouteNormalization::isPureArgument);
    }

    private static boolean isIllegalArgumentType(Trees trees, TreePath methodPath, NewClassTree created) {
        Element element = trees.getElement(TreePath.getPath(methodPath, created.getIdentifier()));
        return element instanceof TypeElement type
                && type.getQualifiedName().contentEquals("java.lang.IllegalArgumentException");
    }

    private static boolean isPureArgument(ExpressionTree expression) {
        return expression instanceof com.sun.source.tree.LiteralTree;
    }

    private static boolean purePredicate(Trees trees, TreePath methodPath, ExecutableElement method,
                                         ExpressionTree condition) {
        if (trees.getTypeMirror(TreePath.getPath(methodPath, condition)) == null
                || trees.getTypeMirror(TreePath.getPath(methodPath, condition)).getKind() != TypeKind.BOOLEAN) return false;
        final boolean[] pure = {true};
        new TreeScanner<Void, Void>() {
            int remaining = 256;
            @Override public Void scan(Tree node, Void unused) {
                if (node == null) return null;
                if (--remaining < 0) { pure[0] = false; return null; }
                switch (node.getKind()) {
                    case IDENTIFIER, PARENTHESIZED, BOOLEAN_LITERAL, CHAR_LITERAL,
                         INT_LITERAL, LONG_LITERAL, FLOAT_LITERAL, DOUBLE_LITERAL,
                         STRING_LITERAL, NULL_LITERAL, UNARY_PLUS, UNARY_MINUS,
                         LOGICAL_COMPLEMENT, BITWISE_COMPLEMENT, PLUS, MINUS,
                         MULTIPLY, DIVIDE, REMAINDER, LESS_THAN, LESS_THAN_EQUAL,
                         GREATER_THAN, GREATER_THAN_EQUAL, EQUAL_TO, NOT_EQUAL_TO,
                         AND, OR, XOR, CONDITIONAL_AND, CONDITIONAL_OR,
                         LEFT_SHIFT, RIGHT_SHIFT, UNSIGNED_RIGHT_SHIFT -> { }
                    default -> { pure[0] = false; return null; }
                }
                return super.scan(node, unused);
            }
            @Override public Void visitIdentifier(IdentifierTree node, Void unused) {
                Element element = trees.getElement(TreePath.getPath(methodPath, node));
                boolean parameter = false;
                for (var candidate : method.getParameters()) parameter |= candidate.equals(element);
                if (!parameter && !node.getName().contentEquals("true") && !node.getName().contentEquals("false")) pure[0] = false;
                return super.visitIdentifier(node, unused);
            }

        }.scan(condition, null);
        return pure[0];
    }

    private static Integer parameterIndex(Trees trees, TreePath methodPath, ExecutableElement method,
                                          ExpressionTree expression) {
        if (!(expression instanceof IdentifierTree identifier)) return null;
        Element element = trees.getElement(TreePath.getPath(methodPath, identifier));
        for (int index = 0; index < method.getParameters().size(); index++) {
            if (method.getParameters().get(index).equals(element)) return index;
        }
        return null;
    }

    private static boolean markOnce(boolean[] used, int index) {
        if (index < 0 || index >= used.length || used[index]) return false;
        used[index] = true;
        return true;
    }

    private static boolean typedLiteral(Trees trees, TreePath methodPath, ExpressionTree expression,
                                        javax.lang.model.type.TypeMirror expected) {
        if (!(expression instanceof com.sun.source.tree.LiteralTree)) return false;
        javax.lang.model.type.TypeMirror actual = trees.getTypeMirror(TreePath.getPath(methodPath, expression));
        return sameType(actual, expected);
    }

    private static Normalized self(ExecutableElement method) {
        String serviceID = JavaDepthRoles.methodID(method);
        List<String> slots = new ArrayList<>();
        for (int index = 0; index < method.getParameters().size(); index++) slots.add(serviceID + "/arg" + index);
        return new Normalized(serviceID, slots, new ArrayList<>(slots));
    }

    private static Map<String, Object> copy(Map<?, ?> source) {
        Map<String, Object> result = new LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : source.entrySet()) if (entry.getKey() instanceof String key) result.put(key, entry.getValue());
        return result;
    }

    private static String string(Object value) { return value instanceof String result ? result : ""; }

    private static boolean sameType(javax.lang.model.type.TypeMirror left, javax.lang.model.type.TypeMirror right) {
        return left != null && right != null && left.toString().contentEquals(right.toString());
    }

    private record Normalized(String serviceID, List<String> required, List<String> exposed) { }
}
