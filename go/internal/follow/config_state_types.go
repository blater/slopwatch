package follow

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/appconfig"
)

type configSettingsState struct {
	open            bool
	kind            configSettingsKind
	generation      uint64
	loading         bool
	saving          bool
	dirty           bool
	cursor          int
	resolved        appconfig.Resolved
	working         appconfig.Resolved
	probes          map[agent.ProfileID]agent.ProbeResult
	status          string
	input           textinput.Model
	prompt          textarea.Model
	promptOriginal  string
	promptError     string
	editing         bool
	editField       int
	choiceOpen      bool
	choiceCursor    int
	dirtyCursor     int
	closeAfterSave  bool
	returnToFix     bool
	profileEditing  bool
	profileCursor   int
	providerCursor  int
	providerRuntime agent.RuntimeKind
	probing         map[agent.ProfileID]bool
	probeAttempts   map[agent.ProfileID]uint64
	probeSequence   uint64
	pendingActive   agent.ProfileID
	pendingChange   bool
	pendingOriginal *agent.Profile
	pendingFix      *appconfig.FixDefaults
	pendingWasDirty bool
	pendingDefault  bool
	defaultChanged  bool
	connectionTitle string
	connectionError string
}

type configDirtyAction uint8

const (
	configDirtyNone configDirtyAction = iota
	configDirtySave
	configDirtyDiscard
	configDirtyCancel
)
