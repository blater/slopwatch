package follow

import (
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/style"
	"github.com/charmbracelet/x/ansi"
)

func TestNotificationLossRemainsNonInterruptingAndExpires(t *testing.T) {
	now := time.Unix(100, 0)
	model := Model{width: 80, height: 24}
	recordNotificationLoss(&model, 1, now)
	recordNotificationLoss(&model, 2, now.Add(10*time.Second))
	if model.runtimeError != "" || len(model.overlays.frames) != 0 {
		t.Fatal("loss interrupted session")
	}
	if model.runtime.notificationLoss.count != 3 {
		t.Fatal("loss did not coalesce")
	}
	for _, view := range []MainView{MainViewFiles, MainViewAgents} {
		model.mainView = view
		text := notificationFooter(model, "left")
		if !strings.HasSuffix(ansi.Strip(text), "! Some file changes may be missed") {
			t.Fatal(text)
		}
	}
	for _, theme := range []style.Theme{style.ThemeDark, style.ThemeLight} {
		model.theme = theme
		if !strings.Contains(ansi.Strip(notificationFooter(model, "")), "!") {
			t.Fatal("warning invisible")
		}
	}
	expireNotificationLoss(&model, now.Add(69*time.Second))
	if !model.runtime.notificationLoss.visible {
		t.Fatal("repeat did not extend deadline")
	}
	expireNotificationLoss(&model, now.Add(70*time.Second))
	if model.runtime.notificationLoss.visible {
		t.Fatal("warning did not expire")
	}
	openNotificationLoss(&model)
	if !overlayPresent(model.overlays, OverlayNotificationLoss) {
		t.Fatal("retained warning unavailable")
	}
	if !strings.Contains(ansi.Strip(notificationLossPopup(model)), "WARNING") {
		t.Fatal("warning title missing")
	}
	handleNotificationLossKey(&model, "esc")
	if model.runtime.notificationLoss.count != 3 || model.runtime.rescanPending || model.runtime.rescanRunning {
		t.Fatal("dismissal changed warning or started rescan")
	}
}

func TestManualRescanCoalescesAndUsesStartupAnalysis(t *testing.T) {
	analyzer := &refreshAnalyzer{}
	model := refreshModel(t.TempDir(), analyzer.document, analyzer)
	model.watcher = nil
	model.analyzing = true
	if requestRescan(&model) != nil || !model.runtime.rescanPending {
		t.Fatal("rescan competed with analysis")
	}
	requestRescan(&model)
	model.analyzing = false
	_, command := continueQueuedAnalysis(&model)
	if command == nil || !model.runtime.rescanRunning {
		t.Fatal("queued rescan did not start")
	}
	if requestRescan(&model) != nil || model.runtime.rescanPending {
		t.Fatal("running rescan duplicated")
	}
	result := command().(analysisResult)
	if !result.full || len(analyzer.targets) != 1 {
		t.Fatal("startup analysis path not used")
	}
	handleAnalysisResult(&model, result)
	if model.runtime.rescanRunning || model.runtime.rescanPending {
		t.Fatal("rescan repeated automatically")
	}
}
