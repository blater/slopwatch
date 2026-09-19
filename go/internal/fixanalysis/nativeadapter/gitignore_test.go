package nativeadapter

import (
	"context"
	"errors"
	"github.com/blater/slopwatch/internal/native"
	"github.com/blater/slopwatch/internal/report"
	"os"
	"path/filepath"
	"testing"
)

func TestFixAnalysisRefreshesGitignorePolicyForEveryInvocation(t *testing.T) {
	disabled := false
	factory := &fakeFactory{documents: []report.Document{testDocument("a.go", 80, 8, 5), testDocument("a.go", 80, 8, 5)}}
	service, err := NewWithFactory(Config{InstallationRoot: "/installation", GitignoreDisabled: func() (bool, error) { return disabled, nil }}, factory)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.analyze(context.Background(), t.TempDir(), []string{"a.go"}, true); err != nil {
		t.Fatal(err)
	}
	disabled = true
	if _, _, err := service.analyze(context.Background(), t.TempDir(), []string{"a.go"}, false); err != nil {
		t.Fatal(err)
	}
	if factory.calls[0].options.DisableGitignore || !factory.calls[1].options.DisableGitignore || factory.calls[1].options.ReadCache {
		t.Fatal("latest ignore policy or final cache isolation lost")
	}
}

func TestNativeFactoryAppliesIgnorePolicyToRealDiscovery(t *testing.T) {
	root, installation := t.TempDir(), t.TempDir()
	for path, data := range map[string]string{filepath.Join(root, ".gitignore"): "*.go\n", filepath.Join(root, "a.go"): "package p", filepath.Join(installation, "component-catalog.json"): "{}"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, disabled := range []bool{false, true} {
		analyzer, err := (nativeFactory{}).New(root, installation, AnalyzerOptions{DisableGitignore: disabled})
		if err != nil {
			t.Fatal(err)
		}
		_, err = analyzer.Analyze(context.Background(), []string{"."}, nil)
		// The empty test catalog has no scoring components; reaching inventory
		// validation when disabled still proves the real factory included a.go.
		if errors.Is(err, native.ErrNoSources) == disabled {
			t.Fatalf("disabled=%v err=%v", disabled, err)
		}
	}
}
