package openairesponses

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blater/slopwatch/internal/agent"
)

func TestToolBrokerRejectsSymlinkAndDoesNotExposeTarget(t *testing.T) {
	root := canonicalTempDir(t)
	outsideRoot := canonicalTempDir(t)
	outside := filepath.Join(outsideRoot, "secret.txt")
	if err := os.WriteFile(outside, []byte("candidate-external-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	provider := &scriptedProvider{t: t, responses: []string{
		functionResponse("call-read", "read_file", `{"path":"linked.go"}`, 1, 1),
		messageResponse("Skipped the unsupported link.", 1, 1, 0),
	}}
	strategy := newTestStrategy(t, provider, Config{})
	result := strategy.Execute(t.Context(), testProfile(), testRequest(t, root), nil)
	if result.Status != agent.ResultCompleted {
		t.Fatalf("Execute() = %#v", result)
	}
	returned := string(provider.requests[1])
	if !strings.Contains(returned, "symbolic links are not accessible") || strings.Contains(returned, "candidate-external-secret") {
		t.Fatalf("symlink result was unsafe: %s", returned)
	}
}
