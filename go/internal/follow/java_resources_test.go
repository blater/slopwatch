package follow

import "testing"

func TestWatcherLanguageClassificationExcludesJavaResourceTemplates(t *testing.T) {
	watcher := &sourceWatcher{includeTests: true}
	if language, ok := languageFor(watcher, "src/main/resources/archetype/src/main/java/Template.java"); ok || language != "" {
		t.Fatalf("resource template classified as %q, included=%t", language, ok)
	}
	if language, ok := languageFor(watcher, "src/main/java/example/resources/Real.java"); !ok || language != "java" {
		t.Fatalf("ordinary Java package classified as %q, included=%t", language, ok)
	}
}
