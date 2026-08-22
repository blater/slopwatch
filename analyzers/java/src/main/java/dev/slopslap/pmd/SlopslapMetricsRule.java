package dev.slopslap.pmd;

import net.sourceforge.pmd.lang.java.ast.ASTClassDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTCompactConstructorDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTConstructorDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTEnumDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTMethodDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTRecordDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTTypeDeclaration;
import net.sourceforge.pmd.lang.java.ast.ASTExecutableDeclaration;
import net.sourceforge.pmd.lang.java.ast.JavaNode;
import net.sourceforge.pmd.lang.java.metrics.JavaMetrics;
import net.sourceforge.pmd.lang.java.metrics.internal.CognitiveComplexityVisitor;
import net.sourceforge.pmd.lang.java.metrics.internal.CycloVisitor;
import net.sourceforge.pmd.lang.java.rule.AbstractJavaRule;
import net.sourceforge.pmd.lang.metrics.Metric;
import net.sourceforge.pmd.lang.metrics.MetricOptions;
import net.sourceforge.pmd.lang.metrics.MetricsUtil;
import net.sourceforge.pmd.reporting.RuleContext;
import org.apache.commons.lang3.mutable.MutableInt;

/** Emits complete PMD metric observations as stable machine-readable violations.
 *
 * <p>The rule deliberately emits observations below lint thresholds. The native
 * adapter converts these records to Slopslap's normalized measurement protocol;
 * scoring and gating never occur in this rule.</p>
 */
public final class SlopslapMetricsRule extends AbstractJavaRule {

    private static final String PREFIX = "SLOPSLAP_METRIC_V1";

    @Override
    public Object visit(ASTMethodDeclaration node, Object data) {
        emitExecutable(node, data, "method");
        return super.visit(node, data);
    }

    @Override
    public Object visit(ASTConstructorDeclaration node, Object data) {
        emitExecutable(node, data, "constructor");
        return super.visit(node, data);
    }

    @Override
    public Object visit(ASTCompactConstructorDeclaration node, Object data) {
        int cognitive = compactConstructorCognitive(node);
        int cyclomatic = compactConstructorCyclomatic(node);
        long npath = metric(JavaMetrics.NPATH_COMP, node, 1L);
        String message = String.join("|",
                PREFIX,
                "scope=function",
                "kind=compact_constructor",
                "symbol=" + node.getEnclosingType().getSimpleName(),
                "cognitive=" + cognitive,
                "cyclomatic=" + cyclomatic,
                "npath=" + npath);
        context(data).addViolationWithMessage(node, message);
        return super.visit(node, data);
    }

    @Override
    public Object visit(ASTClassDeclaration node, Object data) {
        emitType(node, data, node.isInterface() ? "interface" : "class");
        return super.visit(node, data);
    }

    @Override
    public Object visit(ASTRecordDeclaration node, Object data) {
        emitType(node, data, "record");
        return super.visit(node, data);
    }

    @Override
    public Object visit(ASTEnumDeclaration node, Object data) {
        emitType(node, data, "enum");
        return super.visit(node, data);
    }

    private void emitExecutable(
            ASTExecutableDeclaration node, Object data, String kind) {
        int cognitive = metric(JavaMetrics.COGNITIVE_COMPLEXITY, node, 0);
        int cyclomatic = metric(JavaMetrics.CYCLO, node, 1);
        long npath = metric(JavaMetrics.NPATH_COMP, node, 1L);
        String message = String.join("|",
                PREFIX,
                "scope=function",
                "kind=" + kind,
                "symbol=" + node.getName(),
                "cognitive=" + cognitive,
                "cyclomatic=" + cyclomatic,
                "npath=" + npath);
        context(data).addViolationWithMessage(node, message);
    }

    private void emitType(ASTTypeDeclaration node, Object data, String kind) {
        boolean interfaceType = node.isInterface();
        int wmc = interfaceType ? interfaceWmc(node)
                                : metric(JavaMetrics.WEIGHED_METHOD_COUNT, node, 0);
        int atfd = metric(JavaMetrics.ACCESS_TO_FOREIGN_DATA, (JavaNode) node, 0);
        int fanOut = metric(JavaMetrics.FAN_OUT, (JavaNode) node, 0);
        String tcc = interfaceType
                ? "unavailable"
                : Double.toString(metric(JavaMetrics.TIGHT_CLASS_COHESION, node, 0.0d));
        String message = String.join("|",
                PREFIX,
                "scope=type",
                "kind=" + kind,
                "symbol=" + node.getSimpleName(),
                "wmc=" + wmc,
                "atfd=" + atfd,
                "tcc=" + tcc,
                "fan_out=" + fanOut);
        context(data).addViolationWithMessage((JavaNode) node, message);
    }

    private int interfaceWmc(ASTTypeDeclaration node) {
        // PMD's WMC implementation sums its CYCLO metric across getOperations(),
        // but its public metric contract rejects interface nodes before running
        // that implementation. Apply the same aggregation to interface methods.
        return node.getOperations().sumByInt(
                operation -> metric(JavaMetrics.CYCLO, operation, 1));
    }

    private int compactConstructorCognitive(ASTCompactConstructorDeclaration node) {
        // PMD's public metric filter accepts ASTExecutableDeclaration only,
        // while a compact constructor is a ReturnScopeNode with an executable
        // body. Use the same pinned PMD visitor that backs JavaMetrics.
        CognitiveComplexityVisitor.State state =
                new CognitiveComplexityVisitor.State(node);
        node.acceptVisitor(CognitiveComplexityVisitor.INSTANCE, state);
        return state.getComplexity();
    }

    private int compactConstructorCyclomatic(ASTCompactConstructorDeclaration node) {
        // PMD starts ordinary methods and constructors at one before walking
        // their decisions. A compact constructor needs the same baseline.
        MutableInt value = new MutableInt(1);
        node.acceptVisitor(
                new CycloVisitor(MetricOptions.emptyOptions(), node), value);
        return value.intValue();
    }

    private static RuleContext context(Object data) {
        return (RuleContext) data;
    }

    private static <N extends net.sourceforge.pmd.lang.ast.Node,
                    R extends Number> R metric(
            Metric<? super N, R> metric, N node, R fallback) {
        R value = MetricsUtil.computeMetric(metric, node);
        return value == null ? fallback : value;
    }
}
