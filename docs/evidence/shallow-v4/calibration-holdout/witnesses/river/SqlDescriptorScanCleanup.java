package io.riverdb.engine.sql;

import io.riverdb.base.error.StatusCode;

/** Closes physical scan resources and resets retained descriptor execution modes. */
final class SqlDescriptorScanCleanup {
  private final SqlDescriptorScanContext context;

  SqlDescriptorScanCleanup(SqlDescriptorScanContext owner) { context = owner; }

  StatusCode close() {
    StatusCode status = context.cursor.isActive()
        ? context.session.descriptorRows().closeScan(context.cursor)
        : context.pin.isActive() ? context.pin.release() : StatusCode.OK;
    StatusCode childStatus = context.subqueries.close();
    if (status.isOk()) status = childStatus;
    StatusCode orderedStatus = context.ordered.close();
    if (status.isOk()) status = orderedStatus;
    if (!context.cursor.isActive()) {
      StatusCode reset = context.cursor.reset();
      if (status.isOk()) status = reset;
    }
    if (!status.isOk()) return status;
    context.active = false;
    context.matched = false;
    context.materialized = false;
    context.scalarAggregate = false;
    context.forUpdate = false;
    context.boundPredicate.reset();
    context.predicate.reset();
    context.sets.reset();
    context.index.reset();
    return context.scalar.reset();
  }
}
