package follow

const (
	helpCommandLine = "command-line"
	helpMainScreen  = "main-screen"
	helpScoring     = "scoring"
)

type helpTopic struct {
	key   string
	label string
}

type helpEntry struct {
	label       string
	description string
}

var helpTopics = []helpTopic{
	{key: helpCommandLine, label: "Command-line options"},
	{key: helpMainScreen, label: "Main-screen controls"},
	{key: helpScoring, label: "Scoring system"},
}

func helpTopicFor(key string) (helpTopic, bool) {
	for _, topic := range helpTopics {
		if topic.key == key {
			return topic, true
		}
	}
	return helpTopic{}, false
}

var commandLineHelp = []helpEntry{
	{label: "TARGET ...", description: "Files or directories to analyze. The current directory is used when no target is supplied."},
	{label: "--backend language=backend", description: "Override a language analyzer backend. Repeat the option to override more than one language."},
	{label: "-c, --compact", description: "Show only SCORE and PATH in text output and in the live dashboard."},
	{label: "--config FILE", description: "Use the named configuration file."},
	{label: "-f, --follow", description: "Open the live ranking dashboard and refresh affected results as source files change."},
	{label: "--follow-symlinks", description: "Follow symbolic links found inside target directories. An explicitly named symlink target is always followed."},
	{label: "--format text|json", description: "Select human-readable text or machine-readable JSON output. JSON cannot be combined with --follow."},
	{label: "--include-tests", description: "Include test source files, which are excluded by default."},
	{label: "--languages LIST", description: "Analyze only the comma-separated languages in LIST."},
	{label: "--limit NUMBER", description: "Return at most NUMBER ranked files. Zero, the default, returns every result."},
	{label: "--pass-score SCORE", description: "Fail with exit status 3 when any analyzed file scores above SCORE."},
	{label: "--trend-window DURATION", description: "Override the saved duration for movement and edit highlights. Uses values such as 30s, 10m, or 1h."},
	{label: "--typescript-types", description: "Enable slower compiler-aware TypeScript type-safety analysis."},
	{label: "--use-cache", description: "Reuse verified cached analysis units. Without this option, a normal report updates the cache but does not read from it."},
}

// Main-screen entries are deliberately ordered by action name so this page is
// predictable even when the footer has to hide shortcuts on a narrow terminal.
var mainScreenHelp = []helpEntry{
	{label: "Agents", description: "Tab switches Files/Agents; A jumps directly to Agents. a toggles Active/All, f finds jobs, and o cycles job sorting."},
	{label: "Cancel", description: "C cancels the selected running fix job."},
	{label: "Clear marked", description: "M clears all marked files."},
	{label: "Columns", description: "c opens Settings with Columns selected; Enter chooses which metric columns are visible."},
	{label: "Find", description: "f or / searches file paths. Enter accepts the query and Esc cancels it."},
	{label: "Fix", description: "x opens Fix for all marked files, or for the current file when none are marked."},
	{label: "Help", description: "h opens this help system."},
	{label: "Info", description: "i or Enter opens the full analysis for the selected file."},
	{label: "Job details", description: "In Agents, Enter expands a job; i inspects it, d opens its diff, l opens sanitized logs, and C cancels it after confirmation. On an expanded file, Enter inspects the job, v opens candidate source, and i focuses its metrics."},
	{label: "Job status", description: "QUEUED, RUNNING, VERIFYING, COMMITTING, CANCELED, FAILED and DONE are textual states."},
	{label: "Jump to bottom", description: "G or End selects the final file immediately."},
	{label: "Jump to top", description: "g or Home selects the first file immediately."},
	{label: "Mark files", description: "m enters or finishes marking. Space toggles the current file; Shift-Up and Shift-Down toggle files while moving."},
	{label: "Move down", description: "Down or j selects the next file."},
	{label: "Move up", description: "Up or k selects the previous file."},
	{label: "Next match", description: "n selects the next result for the current search."},
	{label: "Page down", description: "Page Down or Ctrl-F moves down by one screen."},
	{label: "Page up", description: "Page Up or Ctrl-B moves up by one screen."},
	{label: "Path scroll", description: "Left and Right reveal horizontally clipped file paths."},
	{label: "Previous match", description: "N selects the previous result for the current search."},
	{label: "Quit", description: "q or Ctrl-C exits when idle. With active fixes, Slopmochi confirms cancel-all and visibly joins them before exit."},
	{label: "Settings", description: "s opens alphabetically ordered settings. Agents shows provider availability, highlights the active provider, and automatically checks connections in provider-specific dialogs."},
	{label: "Sort", description: "o chooses the sort field and direction."},
	{label: "View source", description: "v opens the selected file with syntax highlighting."},
}
