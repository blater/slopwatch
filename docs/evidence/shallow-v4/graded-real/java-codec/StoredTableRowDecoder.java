package io.riverdb.engine.row;

import io.riverdb.base.error.StatusCode;
import io.riverdb.base.type.SqlValueBuffer;
import io.riverdb.engine.schema.TableDescriptor;
import io.riverdb.format.row.StoredTableRowHeader;
import io.riverdb.format.row.StoredTableRowHeaderCodec;
import io.riverdb.storage.heap.HeapPage;
import java.nio.ByteBuffer;

/** Validates a complete row before publishing values into caller-owned storage. */
final class StoredTableRowDecoder {
  private final StoredTableRowHeader header = new StoredTableRowHeader();

  StatusCode decode(
      TableDescriptor table,
      long expectedLogicalRowId,
      ByteBuffer source,
      int start,
      int length,
      SqlValueBuffer destination) {
    if (!validArguments(table, expectedLogicalRowId, source, start, length, destination)) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    if (length < StoredTableRowHeaderCodec.HEADER_BYTES
        || length > HeapPage.MAXIMUM_ROW_BYTES
        || start > source.limit() - length) {
      return StatusCode.CORRUPTION;
    }
    StatusCode status = StoredTableRowHeaderCodec.decode(
        source, start, expectedLogicalRowId, header);
    if (!status.isOk() || header.rowLayoutId() != table.rowLayoutId()) {
      return StatusCode.CORRUPTION;
    }
    int textBytes = StoredTableRowBodyValidator.validate(table, source, start, length);
    if (textBytes < 0) return StatusCode.CORRUPTION;
    if (destination.capacity() < table.columnCount()
        || destination.textCapacity() < textBytes
        || destination.textMaximumBytes() < textBytes) {
      return StatusCode.RESOURCE_EXHAUSTED;
    }
    return StoredTableRowPublisher.publish(table, source, start, destination);
  }

  private static boolean validArguments(
      TableDescriptor table, long rowId, ByteBuffer source, int start, int length,
      SqlValueBuffer destination) {
    return table != null && table.rowLayoutId() > 0 && rowId > 0 && source != null
        && destination != null && start >= 0 && start <= source.limit() && length >= 0;
  }
}
