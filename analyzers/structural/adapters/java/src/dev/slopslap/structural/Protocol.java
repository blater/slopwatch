package dev.slopslap.structural;

import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;

final class Protocol {
    static final int SCHEMA_VERSION = 1;
    private static final int REQUEST_MAGIC = 0x53534a46;
    private static final int RESPONSE_MAGIC = 0x53534a4f;
    private static final int MAX_STRING = 16 * 1024 * 1024;
    private static final int MAX_ITEMS = 1_000_000;

    record Request(String workspace, List<String> paths, boolean includeTests) { }

    static Request readRequest(InputStream input) throws IOException {
        DataInputStream data = new DataInputStream(input);
        if (data.readInt() != REQUEST_MAGIC || data.readInt() != SCHEMA_VERSION) {
            throw new IOException("unsupported Java fact request");
        }
        boolean includeTests = data.readBoolean();
        String workspace = readString(data);
        int count = count(data);
        java.util.ArrayList<String> paths = new java.util.ArrayList<>(count);
        for (int index = 0; index < count; index++) {
            paths.add(readString(data));
        }
        return new Request(workspace, List.copyOf(paths), includeTests);
    }

    static void writeSuccess(OutputStream output, Facts.Program program) throws IOException {
        DataOutputStream data = new DataOutputStream(output);
        data.writeInt(RESPONSE_MAGIC);
        data.writeInt(SCHEMA_VERSION);
        data.writeBoolean(true);
        writeProgram(data, program);
        data.flush();
    }

    static void writeFailure(OutputStream output, String message) throws IOException {
        DataOutputStream data = new DataOutputStream(output);
        data.writeInt(RESPONSE_MAGIC);
        data.writeInt(SCHEMA_VERSION);
        data.writeBoolean(false);
        writeString(data, message);
        data.flush();
    }

    private static int count(DataInputStream data) throws IOException {
        int value = data.readInt();
        if (value < 0 || value > MAX_ITEMS) {
            throw new IOException("invalid Java fact collection size");
        }
        return value;
    }

    private static String readString(DataInputStream data) throws IOException {
        int length = data.readInt();
        if (length < 0 || length > MAX_STRING) {
            throw new IOException("invalid Java fact string size");
        }
        byte[] encoded = data.readNBytes(length);
        if (encoded.length != length) {
            throw new IOException("truncated Java fact string");
        }
        return new String(encoded, StandardCharsets.UTF_8);
    }

    private static void writeString(DataOutputStream data, String value) throws IOException {
        byte[] encoded = value.getBytes(StandardCharsets.UTF_8);
        data.writeInt(encoded.length);
        data.write(encoded);
    }

    private static void writeLocation(DataOutputStream data, Facts.Location value) throws IOException {
        writeString(data, value.path());
        data.writeInt(value.line());
        data.writeInt(value.column());
        data.writeInt(value.endLine());
        data.writeInt(value.endColumn());
    }

    private static void writeExpression(DataOutputStream data, Facts.Expression value) throws IOException {
        data.writeByte(value.kind);
        writeList(data, value.children, Protocol::writeExpression);
        writeStrings(data, value.calls);
        writeList(data, value.nested, Protocol::writeFunction);
    }

    private static void writeCase(DataOutputStream data, Facts.CaseFact value) throws IOException {
        data.writeBoolean(value.isDefault);
        data.writeBoolean(value.fallsThrough);
        writeList(data, value.expressions, Protocol::writeExpression);
        writeList(data, value.body, Protocol::writeStatement);
    }

    private static void writeStatement(DataOutputStream data, Facts.Statement value) throws IOException {
        data.writeByte(value.kind);
        writeLocation(data, value.location);
        data.writeBoolean(value.condition != null);
        if (value.condition != null) {
            writeExpression(data, value.condition);
        }
        writeList(data, value.expressions, Protocol::writeExpression);
        writeList(data, value.body, Protocol::writeStatement);
        writeList(data, value.elseBody, Protocol::writeStatement);
        writeList(data, value.cases, Protocol::writeCase);
        data.writeBoolean(value.maySkip);
        data.writeBoolean(value.labeled);
    }

    private static void writeFunction(DataOutputStream data, Facts.Function value) throws IOException {
        writeString(data, value.name);
        writeString(data, value.receiver);
        writeString(data, value.receiverVar);
        writeLocation(data, value.location);
        writeList(data, value.body, Protocol::writeStatement);
    }

    private static void writeType(DataOutputStream data, Facts.TypeFact value) throws IOException {
        writeString(data, value.name);
        writeString(data, value.kind);
        writeLocation(data, value.location);
        writeList(data, value.methods, Protocol::writeFunction);
        data.writeInt(value.interfaceMethodCount);
        writeStrings(data, value.foreignTypes);
        data.writeInt(value.methodFields.size());
        for (Map.Entry<String, List<String>> entry : value.methodFields.entrySet()) {
            writeString(data, entry.getKey());
            writeStrings(data, entry.getValue());
        }
        writeStrings(data, value.foreignFields);
    }

    private static void writeShape(DataOutputStream data, Facts.TypeShape value) throws IOException {
        writeString(data, value.stableId);
        writeString(data, value.kind);
        writeString(data, value.name);
        writeList(data, value.children, Protocol::writeShape);
        writeStrings(data, value.exposedMembers);
        data.writeInt(value.complexity);
    }

    private static void writeOperation(DataOutputStream data, Facts.PublicOperation value) throws IOException {
        writeString(data, value.stableId);
        writeString(data, value.name);
        writeString(data, value.ownerType);
        writeLocation(data, value.location);
        writeList(data, value.parameters, Protocol::writeShape);
        writeList(data, value.results, Protocol::writeShape);
        data.writeBoolean(value.emitsOutput);
        data.writeBoolean(value.observableMutation);
    }

    private static void writeExposure(DataOutputStream data, Facts.RepresentationExposure value) throws IOException {
        writeString(data, value.stableId);
        writeString(data, value.kind);
        writeString(data, value.entity);
        writeLocation(data, value.location);
        writeString(data, value.evidence);
        writeString(data, value.confidence);
    }

    private static void writeProgram(DataOutputStream data, Facts.Program value) throws IOException {
        writeList(data, value.functions, Protocol::writeFunction);
        writeList(data, value.types, Protocol::writeType);
        writeList(data, value.publicOperations, Protocol::writeOperation);
        writeList(data, value.representation, Protocol::writeExposure);
        writeStrings(data, value.files);
    }

    private static void writeStrings(DataOutputStream data, List<String> values) throws IOException {
        data.writeInt(values.size());
        for (String value : values) {
            writeString(data, value);
        }
    }

    private static <T> void writeList(DataOutputStream data, List<T> values, Writer<T> writer)
            throws IOException {
        data.writeInt(values.size());
        for (T value : values) {
            writer.write(data, value);
        }
    }

    @FunctionalInterface
    private interface Writer<T> {
        void write(DataOutputStream data, T value) throws IOException;
    }

    private Protocol() { }
}
