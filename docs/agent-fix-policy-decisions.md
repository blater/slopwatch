# Agent-assisted Fix policy decisions

Status: owner direction incorporated.

## Non-configurable correctness boundaries

- An agent receives targets, measurements and preferences through the typed
  harness request. It does not read Slopwatch caches or preference files.
- Every job edits an isolated worktree. The user's checkout remains untouched.
- Slopwatch measures the result; provider completion text is not a score.
- Git delivery uses exact, create-only commits and refs. Slopwatch does not
  force-push, overwrite an existing branch, auto-merge or give Git credentials
  to an agent.
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
| Retention | Retained jobs and transcript bytes are configurable. Finished history rolls over automatically; it is not a lifetime quota. |
| Models and effort | Values come from the selected adapter's live capabilities. |
| Delegation | Shown only when the selected adapter offers a real choice. |
| SCORE and metrics | SCORE is always selected. Only enabled, measured component metrics are offered as additional focus. |
| Prompt | One global master template is stored in Fix Defaults and supplies every job. There is no per-job detached prompt. |
| Branch naming | User-configurable; generated job tokens are optional and never an organisation requirement. |
| Delivery | Branch or pull request. Commit, push and optional PR creation happen automatically after the target is met. |
| PR base | Chosen by the user and checked for the configured remote. |
| PR state | Draft or ready for review is a user setting. |
| Validation | Optional trusted capability. Docker-backed validation is optional and Docker is not a product dependency. |

## Job interaction policy

Cancel is the only job command. There is no manual Retry, Resume, Publish,
Keep, Archive, Discard, conflict acknowledgement or generic Actions menu.
Failures use plain FAILED state and release the target so another fix can start.
Git branches and pull requests provide the review workflow.

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
