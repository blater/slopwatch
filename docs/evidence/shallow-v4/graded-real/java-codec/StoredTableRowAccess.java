package io.riverdb.engine.row;

import io.riverdb.base.type.SqlTypeDescriptor;
import io.riverdb.engine.schema.TableDescriptor;
import io.riverdb.format.FormatBytes;
import io.riverdb.format.row.StoredTableRowHeaderCodec;
import java.nio.ByteBuffer;

/** Absolute primitive access shared by validation and publication. */
final class StoredTableRowAccess {
  private StoredTableRowAccess() {
  }

  static boolean nullAt(ByteBuffer source, int start, int index) {
    int bitmap = start + StoredTableRowHeaderCodec.HEADER_BYTES + (index >>> 3);
    return (source.get(bitmap) & 1 << (index & 7)) != 0;
  }

  static long fixedValue(
      TableDescriptor table, int index, ByteBuffer source, int slot) {
    int width = table.fixedWidthAt(index);
    if (width == 1) return Byte.toUnsignedInt(source.get(slot));
    if (width == Short.BYTES) return FormatBytes.getShort(source, slot);
    if (width == Integer.BYTES) {
      int value = FormatBytes.getInt(source, slot);
      return SqlTypeDescriptor.typeId(table.typeDescriptorAt(index))
              == SqlTypeDescriptor.TYPE_ID_REAL
          ? Integer.toUnsignedLong(value) : value;
    }
    return FormatBytes.getLong(source, slot);
  }

  static long wideHigh(ByteBuffer source, int slot) {
    return FormatBytes.getLong(source, slot);
  }

  static long wideLow(ByteBuffer source, int slot) {
    return FormatBytes.getLong(source, slot + Long.BYTES);
  }

  static boolean zero(ByteBuffer source, int offset, int length) {
    for (int index = 0; index < length; index++) {
      if (source.get(offset + index) != 0) return false;
    }
    return true;
  }
}
