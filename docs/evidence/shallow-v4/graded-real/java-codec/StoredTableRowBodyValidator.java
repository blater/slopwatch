package io.riverdb.engine.row;

import io.riverdb.base.text.Utf8Text;
import io.riverdb.base.type.SqlTypeDescriptor;
import io.riverdb.base.type.SqlValueDomain;
import io.riverdb.engine.schema.TableDescriptor;
import io.riverdb.format.FormatBytes;
import io.riverdb.format.row.StoredTableRowHeaderCodec;
import java.nio.ByteBuffer;

/** Canonical body validation with no result publication. */
final class StoredTableRowBodyValidator {
  private StoredTableRowBodyValidator() {
  }

  static int validate(
      TableDescriptor table, ByteBuffer source, int start, int length) {
    int fixedEnd = StoredTableRowEncoder.fixedEnd(table);
    if (length < fixedEnd || length > table.encodedMaximumRowBytes()
        || !canonicalBitmap(table, source, start)) return -1;
    int textOffset = fixedEnd;
    for (int index = 0; index < table.columnCount(); index++) {
      int next = validateSlot(table, source, start, length, index, textOffset);
      if (next < 0) return -1;
      textOffset = next;
    }
    return textOffset == length ? length - fixedEnd : -1;
  }

  private static int validateSlot(
      TableDescriptor table, ByteBuffer source, int start, int length,
      int index, int textOffset) {
    boolean isNull = StoredTableRowAccess.nullAt(source, start, index);
    if (isNull && !table.isNullable(index)) return -1;
    int slot = start + table.fixedOffsetAt(index);
    if (isNull) {
      return StoredTableRowAccess.zero(source, slot, table.fixedWidthAt(index)) ? textOffset : -1;
    }
    int descriptor = table.typeDescriptorAt(index);
    if (!StoredTableRowEncoder.isText(descriptor)) {
      if (SqlTypeDescriptor.isWideDecimal(descriptor)) {
        return SqlValueDomain.fitsDecimal128(
            descriptor,
            StoredTableRowAccess.wideHigh(source, slot),
            StoredTableRowAccess.wideLow(source, slot)) ? textOffset : -1;
      }
      long value = StoredTableRowAccess.fixedValue(table, index, source, slot);
      return SqlValueDomain.fitsFixed(descriptor, value) ? textOffset : -1;
    }
    int offset = FormatBytes.getInt(source, slot);
    int bytes = FormatBytes.getInt(source, slot + Integer.BYTES);
    if (offset != textOffset || bytes < 0 || textOffset > length - bytes) return -1;
    int scalars = Utf8Text.validate(source, start + textOffset, bytes,
        SqlTypeDescriptor.parameterOne(descriptor));
    return scalars < 0 ? -1 : textOffset + bytes;
  }

  private static boolean canonicalBitmap(
      TableDescriptor table, ByteBuffer source, int start) {
    int remainder = table.columnCount() & 7;
    if (remainder == 0) return true;
    int last = start + StoredTableRowHeaderCodec.HEADER_BYTES + table.nullBitmapBytes() - 1;
    return (Byte.toUnsignedInt(source.get(last)) & (0xff << remainder)) == 0;
  }
}
