package dev.slopslap.structural;

import com.sun.source.tree.ExpressionTree;
import javax.tools.JavaFileObject;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

/** Canonical Java source path and test-package handling. */
final class JavaParsePaths {
    private JavaParsePaths() { }
    static Path sourcePath(Path workspace, String requested) throws IOException {
        if (requested.isEmpty() || requested.indexOf('\\') >= 0 || !requested.endsWith(".java")) {
            throw new IllegalArgumentException("non-canonical Java source path: " + requested);
        }
        Path current = workspace;
        for (String part : requested.split("/", -1)) {
            if (part.isEmpty() || part.equals(".") || part.equals("..")) throw new IllegalArgumentException("non-canonical Java source path: " + requested);
            current = current.resolve(part);
        }
        if (!Files.isRegularFile(current)) throw new IllegalArgumentException("Java source is not a regular file: " + requested);
        if (!Files.isReadable(current)) throw new IOException("Java source is not readable: " + requested);
        Path resolved = current.toRealPath();
        if (!resolved.startsWith(workspace)) throw new IOException("Java source escapes workspace: " + requested);
        return current;
    }
    static String sourceFailureCode(String requested, Exception sourceError) {
        if (sourceError != null && sourceError.getMessage() != null && sourceError.getMessage().contains("escapes workspace")) return "SOURCE_PATH_ERROR";
        if (requested.isEmpty() || requested.indexOf('\\') >= 0 || requested.startsWith("/")
                || Arrays.stream(requested.split("/", -1)).anyMatch(part -> part.isEmpty() || part.equals(".") || part.equals(".."))) return "SOURCE_PATH_ERROR";
        if (!requested.endsWith(".java")) return "UNSUPPORTED_SOURCE";
        return "SOURCE_READ_ERROR";
    }
    static String relativePath(Path workspace, JavaFileObject source) {
        return workspace.relativize(Path.of(source.toUri())).toString().replace('\\', '/');
    }
    static boolean testPackage(ExpressionTree packageName) {
        return packageName != null && Arrays.stream(packageName.toString().split("\\.")).anyMatch(
                part -> part.equalsIgnoreCase("test") || part.equalsIgnoreCase("tests"));
    }
}
