package io.riverdb.engine.sql;

import io.riverdb.base.error.StatusDetail;
import io.riverdb.base.error.StatusCode;
import io.riverdb.base.type.SqlValueBuffer;
import io.riverdb.engine.relational.RelationalDescriptorScanCursor;
import io.riverdb.engine.relational.RelationalLockedCandidateResult;
import io.riverdb.engine.relational.RelationalRowIdentityResult;
import io.riverdb.engine.relational.RelationalSession;
import io.riverdb.engine.schema.cache.SchemaPin;

/** Retained resources and primitive mode flags shared by descriptor scan phases. */
final class SqlDescriptorScanContext {
  final RelationalSession session;
  final SchemaPin pin = new SchemaPin();
  final StatusDetail detail = new StatusDetail(128);
  final RelationalDescriptorScanCursor cursor = new RelationalDescriptorScanCursor();
  final RelationalRowIdentityResult identity = new RelationalRowIdentityResult();
  final RelationalLockedCandidateResult lockedCandidate = new RelationalLockedCandidateResult();
  final SqlDescriptorMutationValues values = new SqlDescriptorMutationValues();
  final SqlDescriptorProjection projection = new SqlDescriptorProjection();
  final SqlDescriptorPredicate predicate = new SqlDescriptorPredicate();
  final SqlDescriptorBoundPredicate boundPredicate;
  final SqlDescriptorIndexAccess index = new SqlDescriptorIndexAccess();
  final SqlDescriptorOrderedRows ordered;
  final SqlDescriptorSubqueryExecution subqueries;
  final SqlDescriptorSetExecution sets;
  final SqlDescriptorScalarAggregate scalar;
  boolean materialized;
  boolean scalarAggregate;
  boolean forUpdate;
  boolean matched;
  boolean active;

  SqlDescriptorScanContext(
      RelationalSession owner,
      SqlTemporalContext temporal,
      SqlSessionShapeBudget shapeBudget,
      SqlBoundPredicateEvaluator predicateEvaluator) {
    session = owner;
    ordered = new SqlDescriptorOrderedRows(shapeBudget);
    boundPredicate = new SqlDescriptorBoundPredicate(predicateEvaluator, shapeBudget);
    subqueries = new SqlDescriptorSubqueryExecution(
        owner,
        shapeBudget,
        predicateEvaluator.subqueryPlan(),
        predicateEvaluator.subqueryCache());
    sets = new SqlDescriptorSetExecution(temporal, shapeBudget);
    scalar = new SqlDescriptorScalarAggregate(temporal, shapeBudget);
  }

  StatusCode evaluatePredicate(SqlValueBuffer row) {
    return boundPredicate.active()
        ? boundPredicate.evaluate(row) : predicate.evaluate(row);
  }

  boolean predicateMatched() {
    return boundPredicate.active()
        ? boundPredicate.matched() : predicate.matched();
  }
}
