package io.riverdb.engine.row;

import io.riverdb.base.error.StatusCode;
import io.riverdb.base.type.SqlTypeDescriptor;
import io.riverdb.base.type.SqlValueBuffer;
import io.riverdb.base.type.SqlValueDomain;
import io.riverdb.engine.schema.TableDescriptor;
import io.riverdb.format.FormatBytes;
import io.riverdb.format.row.StoredTableRowHeaderCodec;
import io.riverdb.storage.heap.HeapPage;
import java.nio.ByteBuffer;

/** Prevalidates then atomically writes one canonical stored row. */
final class StoredTableRowEncoder {
  private StoredTableRowEncoder() {
  }

  static StatusCode encode(
      TableDescriptor table,
      long logicalRowId,
      SqlValueBuffer values,
      ByteBuffer target,
      int start,
      StoredTableRowEncodeResult result) {
    if (result == null) return StatusCode.INVALID_EXTERNAL_INPUT;
    result.reset();
    if (!validArguments(table, logicalRowId, values, target, start)) {
      return StatusCode.INVALID_EXTERNAL_INPUT;
    }
    int length = checkedLength(table, values);
    if (length < 0) return StatusCode.INVALID_EXTERNAL_INPUT;
    if (length > HeapPage.MAXIMUM_ROW_BYTES) return StatusCode.RESOURCE_EXHAUSTED;
    if (start > target.limit() - length) return StatusCode.RESOURCE_EXHAUSTED;

    StoredTableRowHeaderCodec.encode(target, start, table.rowLayoutId(), logicalRowId);
    writeBitmap(table, values, target, start);
    writeSlots(table, values, target, start);
    result.setLength(length);
    return StatusCode.OK;
  }

  private static boolean validArguments(
      TableDescriptor table, long logicalRowId, SqlValueBuffer values,
      ByteBuffer target, int start) {
    return table != null && table.rowLayoutId() > 0 && logicalRowId > 0
        && values != null && values.count() == table.columnCount()
        && target != null && !target.isReadOnly() && start >= 0 && start <= target.limit();
  }

  private static int checkedLength(TableDescriptor table, SqlValueBuffer values) {
    int count = table.columnCount();
    int length = fixedEnd(table);
    for (int index = 0; index < count; index++) {
      int descriptor = table.typeDescriptorAt(index);
      if (values.descriptorAt(index) != descriptor
          || values.isNull(index) && !table.isNullable(index)) return -1;
      if (values.isNull(index)) continue;
      if (isText(descriptor)) {
        int bytes = values.textByteLengthAt(index);
        if (bytes < 0 || bytes > HeapPage.MAXIMUM_ROW_BYTES - length) return -1;
        length += bytes;
      } else if (SqlTypeDescriptor.isWideDecimal(descriptor)) {
        if (!SqlValueDomain.validDecimal128(
            descriptor, values.highValueAt(index), values.valueAt(index))) return -1;
      } else if (!SqlValueDomain.validFixed(descriptor, values.valueAt(index))) return -1;
    }
    return length <= table.encodedMaximumRowBytes() ? length : -1;
  }

  private static void writeBitmap(
      TableDescriptor table, SqlValueBuffer values, ByteBuffer target, int start) {
    for (int index = 0; index < table.nullBitmapBytes(); index++) {
      long word = values.nullWord(index >>> 3);
      target.put(start + StoredTableRowHeaderCodec.HEADER_BYTES + index,
          (byte) (word >>> ((index & 7) * Byte.SIZE)));
    }
  }

  private static void writeSlots(
      TableDescriptor table, SqlValueBuffer values, ByteBuffer target, int start) {
    int textOffset = fixedEnd(table);
    for (int index = 0; index < table.columnCount(); index++) {
      int slot = start + table.fixedOffsetAt(index);
      int width = table.fixedWidthAt(index);
      if (values.isNull(index)) zero(target, slot, width);
      else if (isText(table.typeDescriptorAt(index))) {
        int length = values.textByteLengthAt(index);
        FormatBytes.putInt(target, slot, textOffset);
        FormatBytes.putInt(target, slot + Integer.BYTES, length);
        for (int byteIndex = 0; byteIndex < length; byteIndex++) {
          target.put(start + textOffset + byteIndex,
              (byte) values.textByteAt(index, byteIndex));
        }
        textOffset += length;
      } else if (SqlTypeDescriptor.isWideDecimal(table.typeDescriptorAt(index))) {
        FormatBytes.putLong(target, slot, values.highValueAt(index));
        FormatBytes.putLong(target, slot + Long.BYTES, values.valueAt(index));
      } else writeFixed(target, slot, width, values.valueAt(index));
    }
  }

  private static void writeFixed(ByteBuffer target, int slot, int width, long value) {
    if (width == 1) target.put(slot, (byte) value);
    else if (width == Short.BYTES) FormatBytes.putShort(target, slot, (short) value);
    else if (width == Integer.BYTES) FormatBytes.putInt(target, slot, (int) value);
    else FormatBytes.putLong(target, slot, value);
  }

  private static void zero(ByteBuffer target, int offset, int length) {
    for (int index = 0; index < length; index++) target.put(offset + index, (byte) 0);
  }

  static int fixedEnd(TableDescriptor table) {
    int last = table.columnCount() - 1;
    return table.fixedOffsetAt(last) + table.fixedWidthAt(last);
  }

  static boolean isText(int descriptor) {
    return SqlTypeDescriptor.typeId(descriptor) == SqlTypeDescriptor.TYPE_ID_VARCHAR;
  }
}
