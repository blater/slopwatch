package dev.slopslap.structural;

import java.util.List;
import java.util.Map;

final class JavaDepthLimited {
    static String compact(Object original, List<String> files) {
        Map<String, Object> source = original instanceof Map<?, ?> map ? cast(map) : Map.of();
        Object identity = source.getOrDefault("identity", Map.of("artifact", "java:source-unit",
                "audience", "external", "view", "namespace", "symbol", "<oversized Java boundary>"));
        Object boundaryFiles = source.getOrDefault("files", files);
        Map<String, Object> reason = Map.of("code", "depth_payload_limit", "dimension", "inventory",
                "message", "Java boundary exceeded its bounded payload; attribution is unavailable for this boundary");
        Map<String, Object> boundary = Map.of("identity", identity, "state", "partial", "files", boundaryFiles,
                "knowledge", Map.of("inventory", Map.of("state", "partial", "essential", true)),
                "reasons", List.of(reason));
        return DepthJson.encode(Map.of("boundaries", List.of(boundary), "flows", List.of(), "reasons", List.of(reason)));
    }

    @SuppressWarnings("unchecked")
    private static Map<String, Object> cast(Map<?, ?> source) {
        Map<String, Object> result = new java.util.LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : source.entrySet()) {
            if (entry.getKey() instanceof String key) result.put(key, entry.getValue());
        }
        return result;
    }

    private JavaDepthLimited() { }
}
