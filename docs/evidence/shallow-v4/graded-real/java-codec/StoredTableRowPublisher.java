package io.riverdb.engine.row;

import io.riverdb.base.error.StatusCode;
import io.riverdb.base.type.SqlValueBuffer;
import io.riverdb.engine.schema.TableDescriptor;
import io.riverdb.format.FormatBytes;
import java.nio.ByteBuffer;

/** Publishes a previously validated row into reusable value storage. */
final class StoredTableRowPublisher {
  private StoredTableRowPublisher() {
  }

  static StatusCode publish(
      TableDescriptor table, ByteBuffer source, int start, SqlValueBuffer destination) {
    StatusCode status = destination.clearForSize(table.columnCount());
    if (!status.isOk()) return status;
    for (int index = 0; index < table.columnCount(); index++) {
      status = publishSlot(table, source, start, index, destination);
      if (!status.isOk()) return StatusCode.INVARIANT_BROKEN;
    }
    return StatusCode.OK;
  }

  private static StatusCode publishSlot(
      TableDescriptor table, ByteBuffer source, int start, int index,
      SqlValueBuffer destination) {
    int descriptor = table.typeDescriptorAt(index);
    if (StoredTableRowAccess.nullAt(source, start, index)) {
      return destination.setNull(index, descriptor);
    }
    int slot = start + table.fixedOffsetAt(index);
    if (!StoredTableRowEncoder.isText(descriptor)) {
      if (io.riverdb.base.type.SqlTypeDescriptor.isWideDecimal(descriptor)) {
        return destination.setDecimal128(
            index,
            descriptor,
            StoredTableRowAccess.wideHigh(source, slot),
            StoredTableRowAccess.wideLow(source, slot));
      }
      long value = StoredTableRowAccess.fixedValue(table, index, source, slot);
      return destination.setFixed(index, descriptor, value);
    }
    int offset = FormatBytes.getInt(source, slot);
    int bytes = FormatBytes.getInt(source, slot + Integer.BYTES);
    return destination.setTextBytes(index, descriptor, source, start + offset, bytes);
  }
}
