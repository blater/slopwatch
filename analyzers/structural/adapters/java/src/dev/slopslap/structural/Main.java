package dev.slopslap.structural;

public final class Main {
    public static void main(String[] args) throws Exception {
        try {
            Protocol.Request request = Protocol.readRequest(System.in);
            Protocol.writeSuccess(System.out, JavaParser.analyze(request));
        } catch (Exception error) {
            Protocol.writeFailure(System.out,
                    error.getMessage() == null ? error.getClass().getSimpleName() : error.getMessage());
        }
    }

    private Main() { }
}
