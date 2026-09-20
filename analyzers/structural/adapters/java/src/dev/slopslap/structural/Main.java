package dev.slopslap.structural;

public final class Main {
    public static void main(String[] args) throws Exception {
        boolean[] prefixWritten = {false};
        try {
            Protocol.Request request = Protocol.readRequest(System.in);
            boolean stream = args.length > 0 && args[0].equals("--stream-depth");
            Facts.Program program = JavaParser.analyze(request, value -> {
                Protocol.writeSuccessPrefix(System.out, value);
                prefixWritten[0] = true;
                if (stream) DepthStream.start();
            }, stream ? DepthStream::write : null);
            if (stream) DepthStream.write(java.util.Map.of("type", "done"));
            else Protocol.writeSuccessDepth(System.out, program.depth);
        } catch (Exception error) {
            if (prefixWritten[0]) {
                System.err.println(error.getMessage() == null ? error.getClass().getSimpleName() : error.getMessage());
                System.exit(1);
                return;
            }
            Protocol.writeFailure(System.out,
                    error.getMessage() == null ? error.getClass().getSimpleName() : error.getMessage());
        }
    }

    private Main() { }
}
