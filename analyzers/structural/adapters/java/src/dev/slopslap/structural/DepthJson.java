package dev.slopslap.structural;

import java.util.List;
import java.util.Map;

/** Bounded JSON transport, independent of any project libraries. */
final class DepthJson {
    static final class LimitExceeded extends IllegalArgumentException {
        LimitExceeded() { super("Java depth payload limit exceeded"); }
    }
    private final StringBuilder out = new StringBuilder();
    private static final int MAX_CHARS = (64 << 20) / 3;
    static String encode(Object value) {
        DepthJson writer = new DepthJson();
        writer.value(value);
        return writer.out.toString();
    }
    private void append(String text) {
        // Conservative UTF-8 byte bound; no oversized intermediate payload.
        ensure(text.length());
        out.append(text);
    }
    private void append(char value) {
        ensure(1);
        out.append(value);
    }
    private void appendSpan(String text, int start, int end) {
        if (start >= end) return;
        ensure(end - start);
        out.append(text, start, end);
    }
    private void ensure(int additional) {
        if ((long) out.length() + additional > MAX_CHARS) throw new LimitExceeded();
    }
    private void value(Object value) {
        if (value instanceof Map<?, ?> map) { object(map); return; }
        if (value instanceof List<?> list) { array(list); return; }
        if (value instanceof String text) { string(text); return; }
        if (value instanceof Boolean || value instanceof Number) { append(value.toString()); return; }
        if (value == null) { append("null"); return; }
        throw new IllegalArgumentException("unsupported depth transport value");
    }
    private void object(Map<?, ?> map) {
        append("{");
        boolean separator = false;
        for (Map.Entry<?, ?> entry : map.entrySet()) {
            if (separator) append(",");
            string((String) entry.getKey()); append(":"); value(entry.getValue());
            separator = true;
        }
        append("}");
    }
    private void array(List<?> list) {
        append("[");
        boolean separator = false;
        for (Object item : list) {
            if (separator) append(",");
            value(item); separator = true;
        }
        append("]");
    }
    private void string(String text) {
        append('"');
        int spanStart = 0;
        for (int index = 0; index < text.length(); index++) {
            char c = text.charAt(index);
            if (c != '"' && c != '\\' && c >= 32) continue;
            appendSpan(text, spanStart, index);
            if (c == '"' || c == '\\') {
                append('\\');
                append(c);
            } else {
                appendControl(c);
            }
            spanStart = index + 1;
        }
        appendSpan(text, spanStart, text.length());
        append('"');
    }
    private void appendControl(char value) {
        ensure(6);
        out.append("\\u");
        out.append(HEX[value >>> 12]);
        out.append(HEX[(value >>> 8) & 15]);
        out.append(HEX[(value >>> 4) & 15]);
        out.append(HEX[value & 15]);
    }
    private static final char[] HEX = "0123456789abcdef".toCharArray();
}
