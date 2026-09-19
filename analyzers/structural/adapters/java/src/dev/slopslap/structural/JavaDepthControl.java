package dev.slopslap.structural;

import java.util.LinkedHashMap;
import java.util.Map;

/** Canonical control edge metadata shared by Java depth lowering paths. */
final class JavaDepthControl {
    private JavaDepthControl() { }

    static Map<String, Object> edge(String from, String to, String kind, String guard) {
        Map<String, Object> fields = new LinkedHashMap<>();
        fields.put("from", from);
        fields.put("to", to);
        fields.put("kind", kind);
        if (guard != null && !guard.isEmpty()) {
            fields.put("guard", guard);
            fields.put("guard_polarity", kind);
        }
        return fields;
    }
}
