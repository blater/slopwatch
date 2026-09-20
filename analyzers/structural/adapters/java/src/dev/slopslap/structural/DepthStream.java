package dev.slopslap.structural;

import com.sun.source.util.JavacTask;
import com.sun.source.util.TaskEvent;
import com.sun.source.util.TaskListener;
import java.io.DataOutputStream;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.util.Map;

final class DepthStream {
    @FunctionalInterface
    interface Writer { void write(Map<String, Object> frame) throws IOException; }

    static void start() throws IOException {
        new DataOutputStream(System.out).writeInt(-1);
        System.out.flush();
    }

    static void write(Map<String, Object> frame) throws IOException {
        byte[] payload = DepthJson.encode(frame).getBytes(StandardCharsets.UTF_8);
        DataOutputStream output = new DataOutputStream(System.out);
        output.writeInt(payload.length);
        output.write(payload);
        output.flush();
    }

    static void observe(JavacTask task, Writer writer) throws IOException {
        writer.write(Map.of("type", "progress", "stage", "attribution", "completed", 0, "total", 0));
        task.addTaskListener(new TaskListener() {
            private int completed;
            public void started(TaskEvent event) { }
            public void finished(TaskEvent event) {
                if (event.getKind() != TaskEvent.Kind.ANALYZE || event.getSourceFile() == null) return;
                try {
                    writer.write(Map.of("type", "progress", "stage", "attributed_types", "completed", ++completed, "total", 0));
                } catch (IOException error) { throw new UncheckedIOException(error); }
            }
        });
    }

    private DepthStream() { }
}
