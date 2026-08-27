# Agent-assisted Fix policy decisions

Status: owner direction incorporated.

## Non-configurable correctness boundaries

- An agent receives targets, measurements and preferences through the typed
  harness request. It does not read Slopwatch caches or preference files.
- The baseline measures the selected contents as they exist when the job is
  prepared; it is not defined by, or required to match, Git `HEAD`.
- Slopwatch measures the result; provider completion text is not a score.
- Policy diagnostics are warnings. Preparation and admission do not try to
  predict every condition that may affect execution or delivery; operations
  run and report concrete failures when they occur. Only malformed requests or
  conditions that would act on the wrong resource fail before runtime.
- Provider events are associated with the exact job, session and turn before
  they affect job state.
- Cancellation is per job and releases target reservations immediately.

## Configurable product policy

| Area | Behaviour |
| --- | --- |
| Authentication | Codex is first and defaults to its provider-owned ChatGPT/API-key login. OpenAI API is a separate explicit choice. There is no silent fallback. |
| Duration | No Slopwatch attempt timeout and no inactivity disconnect. Readiness timeout and post-cancel grace are preferences-file-only adapter settings. |
| Iteration | The agent is automatically called again with fresh measurements until the target is met or the job is canceled/fails. There is no attempt cap. |
| Concurrency | Agent and verifier counts are user-configurable. New jobs may be submitted while others run. |
| Persistence | Each job has one plain JSON state document. Transcripts and configurable persistence limits are excluded. |
| Models and effort | Values come from the selected adapter's live capabilities. |
| SCORE and metrics | SCORE and enabled, measured component metrics can be selected as focus metrics. |
| Prompt | One global master template is stored in Fix Defaults and supplies every job. There is no per-job detached prompt. |
| Execution workspace | Editing the current workspace or using an isolated worktree is a user choice. Worktrees are not a prerequisite for Fix. |
| Branch naming | User-configurable; generated job tokens are optional and never an organisation requirement. |
| Delivery | Optional. The user may apply changes without Git delivery, push a branch, or open a pull request. Git checks run only when Git delivery is selected and reaches runtime. |
| PR base | Chosen by the user and checked for the configured remote. |
| PR state | Draft or ready for review is a user setting. |

## Job interaction policy

Cancel is the only job command. There is no manual Retry, Resume, Publish,
Keep, Archive, Discard, conflict acknowledgement or generic Actions menu.
Failures use plain FAILED state and release the target so another fix can start.
When delivery is selected, Git branches and pull requests may provide the
review workflow; they are not required to retain a successful result.

## Settings presentation policy

The Settings list is alphabetical. Primary dialogs show only essential choices.
Executable paths, runtime internals, probe timeouts and cancellation grace stay
in preferences. Settings rows do not append source/origin blurbs or selected-row
help paragraphs. Connection dialogs test automatically and render a clear,
wrapped result.

## Deferred

- Files-view multi-select;
- multiple accounts for one provider;
- Claude, Grok and other strategy adapters; and
- additional pull-request provider adapters.

These additions must preserve the existing deep strategy boundary and may not
introduce provider-specific dependencies into the coordinator or UI state.
