# Slopmark, Slopslap, and Slopmochi implementation specification

Status: approved architecture; implementation not started.

This document is the normative implementation plan for splitting the current
codebase into three separately usable products while retaining a complete
Slopmochi consumer bundle.

The words MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY describe requirements in
the RFC 2119 sense. A phase is complete only when its exit criteria pass from a
clean checkout and from the packaged artifacts produced by CI.

## 1. Product definitions

### 1.1 Slopmark

Slopmark is the static-analysis product. It owns source discovery, analysis
planning, analyzer execution, scoring, reporting, and analysis caches.

Slopmark MUST be usable without Slopslap or Slopmochi through:

- a command-line interface suitable for CI/CD;
- JSON output with a versioned schema; and
- a read-only MCP server.

Slopmark MUST NOT import UI, agent-provider, candidate, Git delivery, pull
request, or Slopslap job-management packages. Its operational MCP surface MUST
be read-only with respect to the workspace and MUST NOT expose a general
file-reading, shell, Git, or network tool.

### 1.2 Slopslap

Slopslap is the headless agent remediation product. It owns agent providers,
prompt compilation, candidate workspaces, job coordination, verification,
delivery, publication, persistence, and recovery.

Slopslap MUST be usable without Slopmochi as an MCP server. It MUST obtain all
quality measurements from Slopmark through a Slopmark client boundary. It MUST
NOT import Slopmark implementation packages.

### 1.3 Slopmochi

Slopmochi is the consumer-facing terminal application. It owns presentation,
interaction, file watching, UI preferences, and process supervision for its
packaged Slopmark and Slopslap children.

Slopmochi MUST communicate with Slopmark and Slopslap through MCP. It MUST NOT
import their implementation or domain packages. The released Slopmochi bundle
MUST contain tested versions of all three executables and all analyzer
runtimes.

Slopmochi MUST remain useful when Slopslap is unavailable: analysis and file
browsing continue, while agent features show a precise unavailable reason.
Slopmark is mandatory.

## 2. Target runtime architecture

```text
                          Slopmochi
                       terminal UI/client
                        /             \
               MCP over STDIO     MCP over STDIO
                     /                 \
                Slopmark             Slopslap
             analysis server        job server
                                          \
                                      MCP client
                                            \
                                           Slopmark
```

When launched from the Slopmochi bundle, each MCP connection consists of a
fresh child process and private parent-child STDIO pipes:

- parent writes MCP JSON-RPC messages to child stdin;
- parent reads MCP JSON-RPC messages from child stdout;
- child stderr is a separate bounded diagnostic stream;
- stdout contains MCP messages only;
- no shell interprets the executable or arguments; and
- no listening socket or named filesystem endpoint is created.

Slopmochi MAY use one Slopmark child and share its client inside the UI. In the
first implementation, Slopslap MUST launch its own Slopmark child. Sharing a
Slopmark process between Slopmochi and Slopslap would require multiplexed
ownership and lifecycle semantics and is deferred.

An independently launched Slopslap server MUST resolve Slopmark from an
explicit canonical `--slopmark` path or from a canonical sibling path in the
same installation. It MUST NOT select Slopmark from ambient `PATH` at runtime.

## 3. Repository and Go module layout

The split MUST first be completed in this repository. Separate repositories
are deferred until the module and process boundaries are proven by releases.

Target layout:

```text
go.work
products/
  slopmark/
    go.mod
    cmd/slopmark/
    internal/
    analyzers/
  slopslap/
    go.mod
    cmd/slopslap/
    internal/
  slopmochi/
    go.mod
    cmd/slopmochi/
    internal/
contracts/
  slopmark/v1/
  slopslap/v1/
build/
dist/
```

`go.work` exists for local development only. Release builds MUST build each
module independently without relying on workspace-only replacements.

Permitted module dependencies:

```text
slopmochi -> official MCP Go SDK
slopslap  -> official MCP Go SDK
slopmark  -> official MCP Go SDK
```

The products MUST NOT import one another's Go packages. Canonical JSON Schemas
and protocol fixtures live under `contracts/`. Each consumer generates or owns
product-local wire DTOs and translates them immediately into its own domain or
view types. CI validates all implementations against the same fixtures. The
contracts directory is release input, not a fourth runtime product or Go
module.

Initial ownership mapping:

| Current area | Target product |
| --- | --- |
| `analysiscache`, `native`, `report`, `scoring`, `sourcepath`, `unitplan` | Slopmark |
| structural and TypeScript analyzers and catalog | Slopmark |
| `agent`, `candidate`, `delivery`, `fix`, `fixanalysis`, `fixapp`, `fixprompt` | Slopslap |
| `appconfig`, `gitmanifest`, `isolation`, `jobstore`, `publisher` | Slopslap, with product-local helpers where required |
| `follow`, `style`, `workspace` | Slopmochi |
| `preferences` | split into product-owned documents |
| `userdata` | split into product-owned paths |

Generic code MAY be copied during extraction and consolidated only after the
boundaries are stable. A fourth shared-internals module MUST NOT be introduced.

## 4. Commands and compatibility

### 4.1 Slopmark commands

```text
slopmark analyze [OPTIONS] [TARGET ...]
slopmark mcp --workspace ABSOLUTE_PATH [--config ABSOLUTE_PATH]
             [--allow-target PATH ...] [--admin] [SERVER OPTIONS]
slopmark version [--json]
```

For one compatibility release, `slopmark [OPTIONS] [TARGET ...]` MUST behave as
`slopmark analyze [OPTIONS] [TARGET ...]`. The existing `--follow` option MUST
move to Slopmochi and MUST produce an actionable deprecation error from
Slopmark during that release. It is removed in the following major release.

`--allow-target` is a server authority option, not an analysis option. It is
accepted only from the trusted launching process, canonicalized before the MCP
session starts, and used solely to preserve explicitly named symlink targets
outside the primary workspace. Repository configuration and MCP requests
cannot set it.

CI exit codes remain:

- `0`: analysis completed and configured thresholds passed;
- `2`: invocation, configuration, analysis, or output failure; and
- `3`: analysis completed and a configured threshold failed.

### 4.2 Slopslap commands

```text
slopslap mcp --workspace ABSOLUTE_PATH --slopmark ABSOLUTE_PATH
             [--config ABSOLUTE_PATH] [--admin] [SERVER OPTIONS]
slopslap version [--json]
```

A human-oriented `slopslap fix` CLI is outside the first split. The MCP server
is the first public interface.

For both servers, an omitted `--config` uses the product's standard user path.
An explicit path must be absolute, canonical, user-owned, outside the selected
repository, and not group/world writable. Repository configuration cannot
redirect the user configuration path.

### 4.3 Slopmochi commands

```text
slopmochi [OPTIONS] [TARGET ...]
slopmochi version [--json]
```

Slopmochi accepts the former follow-mode analysis flags and translates them to
Slopmark MCP inputs. It does not forward an arbitrary argument string to
children.

All `version --json` responses MUST include product version, build commit,
report-schema versions, preferred MCP version, and supported MCP versions.

## 5. MCP protocol policy

All products MUST use `github.com/modelcontextprotocol/go-sdk/mcp` v1.7.0 or a
later tested patch/minor release that preserves the required protocol set.
The SDK transport owns JSON-RPC framing, lifecycle, discovery, and legacy
negotiation; product code MUST NOT implement a parallel MCP codec.

Servers are built with `mcp.NewServer`, typed `mcp.AddTool` handlers, and
`mcp.StdioTransport`. Clients are built with `mcp.NewClient` and an
`mcp.CommandTransport` configured with an already validated `exec.Cmd`.
Before connection, Slopmochi/Slopslap set the exact executable, argument vector,
working directory, allowlisted environment, bounded stderr writer, close-on-exec
descriptors, and platform process-group controls. If the SDK transport cannot
terminate the complete owned process group on a supported platform, a
product-local SDK `Transport` adapter MUST supply that lifecycle behavior
without reimplementing MCP or JSON-RPC framing.

Protocol policy:

```text
preferred: 2026-07-28
supported:
  - 2026-07-28
  - 2025-11-25
  - 2025-06-18
  - 2025-03-26
  - 2024-11-05
```

New clients MUST attempt `server/discover` and select the highest mutually
supported version. They MUST fall back to the legacy `initialize` lifecycle
when connecting to a legacy server. Servers MUST accept every version in the
supported list.

MCP protocol versions MUST NOT enter product service interfaces. Version and
wire-era translation stays inside the MCP transport package.

STDIO is the only required transport in the first release. Streamable HTTP is
deferred. Deprecated HTTP+SSE is not supported. When Streamable HTTP is added,
`2026-07-28` MUST use stateless mode and HTTP MUST be disabled by default.

The initial tool contract MUST use ordinary tools and explicit application
handles. It MUST NOT require MCP Tasks, subscriptions, roots, sampling, or
elicitation. This provides identical semantics on modern and legacy versions.
Modern subscriptions MAY be added as an optimization after polling is proven.

Each server has two tool profiles:

- operational, the default profile intended for generic MCP clients; and
- admin, enabled only by explicit `--admin` on STDIO and used by Slopmochi's
  private child connection.

Admin mode MUST be rejected for any future network transport. It adds only the
configuration operations defined below; it does not weaken workspace,
candidate, credential, executable, or delivery validation. Slopmochi MUST
launch its packaged children in admin mode. Standalone MCP configuration
examples MUST use the operational profile.

Initial tool inventory:

| Server/profile | Tools |
| --- | --- |
| Slopmark operational | `analyze`, `list_findings`, `get_file_report`, `check_threshold`, `explain_metric` |
| Slopmark admin additions | `get_settings`, `update_settings` |
| Slopslap operational | `plan_fix`, `start_fix`, `list_jobs`, `get_job`, `get_job_log`, `get_job_diff`, `cancel_job`, `list_agent_profiles`, `probe_agent_profile` |
| Slopslap admin additions | `get_settings`, `update_settings` |

Every tool MUST provide both a JSON Schema input and structured output.
Unknown input fields MUST be rejected. Numeric values MUST reject NaN and
infinity. Strings and arrays MUST have explicit size limits.

Every successful structured result contains `api_version: "v1"`. Tool failures
set MCP `isError` and place the error object from section 6.2 in structured
content; text content contains only the same sanitized human message.

MCP tool annotations are descriptive only and MUST NOT be treated as an
authorization control.

## 6. Common wire conventions

### 6.1 Identifiers

- Display identifiers are human-readable and are not secrets.
- Application handles crossing MCP are at least 128 bits from a
  cryptographically secure random source and encoded as unpadded base64url or
  lowercase base32.
- Handles MUST NOT appear in logs unless redacted to a short prefix.
- In HTTP mode, handles MUST be bound server-side to the authenticated
  principal and workspace. Possession is never authentication.

Slopslap retains the current readable job name as `display_id` and adds an
opaque `job_handle`. Internal persistence uses a stable cryptographic job key;
filenames MUST NOT be derived from unvalidated client input.

### 6.2 Errors

Tool errors return a stable structured body:

```json
{
  "code": "target_outside_workspace",
  "message": "Target is outside the configured workspace",
  "retryable": false,
  "details": {}
}
```

`code` is a documented machine value. `message` is sanitized and contains no
credentials, absolute private state paths, provider response bodies, or raw
command output. `details` is bounded and contains only schema-defined fields.

The initial common codes are:

- `invalid_request`
- `unsupported_option`
- `target_outside_workspace`
- `resource_not_found`
- `resource_busy`
- `limit_exceeded`
- `not_authorized`
- `dependency_unavailable`
- `canceled`
- `internal_error`

Product-specific codes extend this set without changing existing meanings.

### 6.3 Pagination

List and log tools use opaque cursors. The server chooses a default page size
of 100 and enforces a maximum of 500. Cursors are scoped to the originating
workspace, principal where applicable, query, and result generation. Invalid
or expired cursors fail with `invalid_request`; they never restart silently.

### 6.4 Idempotency

Every Slopslap mutation accepts `request_id`, a caller-generated opaque string
of 16 to 128 ASCII characters. The server stores the result keyed by operation,
principal, workspace, and request ID. Repeating an identical request returns
the original result. Reusing a request ID with different normalized input
fails with `invalid_request`.

Idempotency records MUST survive process restart for `start_fix` and
`cancel_job`. Delivery is durably reconciled as part of the accepted job.

### 6.5 Contract versioning

MCP protocol negotiation and product API versioning are independent. The MCP
revision controls transport semantics; `api_version` controls Slopmark or
Slopslap structured content.

Within `v1`, servers may add optional output fields and new tools. They MUST NOT
remove or rename fields, change field meanings, add required input fields, or
broaden a tool's authority. A breaking change uses new tool names and contract
schemas under `contracts/<product>/v2`. Consumers reject unknown major API
versions with `dependency_unavailable`.

The Slopmark CLI report remains schema version 3 during the split. Transport
wrappers do not renumber that report. Any later report-schema change follows
its existing schema policy and is listed independently in the bundle manifest.

## 7. Slopmark service and MCP contract

### 7.1 Service boundary

Both CLI and MCP adapters call one service:

```go
type Analyzer interface {
    Analyze(context.Context, AnalyzeRequest) (Analysis, error)
    FindingPage(context.Context, FindingPageRequest) (FindingPage, error)
    FileReport(context.Context, FileReportRequest) (FileReport, error)
}
```

The types above belong to Slopmark. The service accepts already validated,
workspace-relative paths and contains no CLI or MCP types.

An analysis snapshot records:

- opaque analysis handle;
- canonical workspace identity digest, never the canonical path on the wire;
- report schema version;
- analyzer/profile-set identity;
- normalized options;
- source fingerprint;
- creation time;
- completion and threshold state; and
- ranked file results.

Snapshots may be reconstructed from the content-addressed cache. A handle MUST
not permit access to another configured workspace.

### 7.2 `analyze`

Input:

```json
{
  "targets": ["."],
  "languages": ["go", "java", "rust", "typescript"],
  "include_tests": false,
  "typescript_types": false,
  "follow_symlinks": false,
  "read_cache": true,
  "pass_score": 100
}
```

Rules:

- `targets` defaults to `["."]`, maximum 256 entries;
- every target is a normalized relative path within the server workspace;
- absolute paths, NUL, empty components, `..`, and platform aliases that
  escape the canonical root are rejected;
- languages are a closed set advertised by the installed catalog;
- `pass_score` is optional, finite, and non-negative; and
- nested symlink traversal is opt-in; and
- an explicitly targeted symlink resolving outside the workspace is accepted
  only when that exact logical path and canonical destination were frozen by a
  trusted `--allow-target` at server launch. MCP cannot add allowed roots.

Output:

```json
{
  "api_version": "v1",
  "analysis_id": "a_...",
  "schema_version": 3,
  "complete": true,
  "passed": true,
  "file_count": 123,
  "profile_set_hash": "...",
  "source_fingerprint": "...",
  "created_at": "2026-08-27T12:00:00Z"
}
```

`passed` is omitted when no threshold was requested. The complete report is not
returned from this call.

### 7.3 `list_findings`

Input:

```json
{
  "analysis_id": "a_...",
  "cursor": "...",
  "limit": 100,
  "language": "go",
  "minimum_score": 0
}
```

Output contains ranked summaries only: relative path, language, rank, score,
pass state, completion, coverage summary, and component contributions. It does
not contain source contents or arbitrary diagnostic payloads.

### 7.4 `get_file_report`

Input contains `analysis_id` and an exact relative `path`. Output contains the
existing versioned Slopmark file report, including bounded measurement
evidence and source ranges. It MUST NOT return source file contents.

### 7.5 `check_threshold`

Input contains `analysis_id` and an optional replacement threshold. Output
contains pass state, failing file count, and a bounded page of failing paths.
It never reinterprets an incomplete analysis as a pass.

### 7.6 `explain_metric`

Input contains a metric ID. Output comes from the installed, trusted component
catalog. Repository content cannot redefine metric help.

### 7.7 Admin settings tools

Admin mode adds `get_settings` and `update_settings`. `get_settings` returns a
sanitized analysis-settings document and an opaque revision. `update_settings`
accepts:

```json
{
  "request_id": "caller-generated-idempotency-key",
  "expected_revision": "...",
  "patch": {}
}
```

The patch schema contains only documented Slopmark analysis/profile/cache
settings. It cannot set workspaces, executable paths, environment variables,
credentials, or network destinations. Updates use compare-and-swap revision
semantics, validate the complete resulting document, write atomically, and
return the new revision. A stale revision fails with `resource_busy` and never
merges silently.

Cache deletion is not part of `update_settings`. A future explicit cache tool
requires its own bounded contract.

### 7.8 Limits

Defaults, configurable only by trusted server configuration:

- maximum MCP request body: 1 MiB;
- maximum structured tool result: 4 MiB;
- maximum evidence entries per file report: 2,000;
- maximum concurrent analyses: number of logical CPUs, capped at 8;
- maximum queued analyses: 32; and
- cache and snapshot directories: private owner-only permissions.

Slopmark MUST apply its existing analyzer output limits below the MCP result
limit. Analyzer executables are installation-owned canonical paths and receive
an allowlisted environment with no credentials and no network proxy variables.

## 8. Slopslap service and MCP contract

### 8.1 Core service boundary

The current `fixapp.Manager` remains the initial coordinator core, renamed and
moved as appropriate. The MCP adapter maps tools onto typed service methods; it
does not directly call providers, Git, the job store, or Slopmark.

The Slopmark dependency is expressed as:

```go
type AnalysisService interface {
    PrepareBaseline(context.Context, BaselineRequest) (Baseline, error)
    Verify(context.Context, VerificationRequest) (Verification, error)
}
```

The production implementation uses the Slopmark MCP client. Tests continue to
use fakes. Slopslap MUST treat Slopmark results as authoritative and provider
completion text as untrusted commentary.

### 8.2 `plan_fix`

This is read-only and performs the current load/preparation operation without
creating a candidate or starting an agent.

Input:

```json
{
  "targets": ["go/internal/example/service.go"],
  "target_score": 100,
  "focus": [{"metric": "score", "maximum": 100}],
  "change_scope": "targets",
  "workspace_mode": "worktree",
  "agent_profile": "codex-default",
  "model": "",
  "effort": "high",
  "delivery": {
    "git": "uncommitted",
    "publish": "local"
  }
}
```

The server, not the client, resolves allowed paths, provider capabilities,
trusted preferences, executable paths, credentials, concurrency policy, and
delivery policy.

Output includes:

- opaque `plan_handle` with a maximum lifetime of 10 minutes;
- normalized targets and frozen allowed paths;
- baseline scores and fingerprint;
- selected provider/model/effort labels;
- candidate mode;
- warnings; and
- the normalized accepted delivery plan.

The plan is bound to the exact baseline fingerprint, workspace, server policy,
and principal where applicable. A stale plan fails; it is never silently
recomputed by `start_fix`.

### 8.3 `start_fix`

Input:

```json
{
  "request_id": "caller-generated-idempotency-key",
  "plan_handle": "p_..."
}
```

Output:

```json
{
  "api_version": "v1",
  "job_handle": "j_...",
  "display_id": "job-readable-name",
  "phase": "queued",
  "created_at": "2026-08-27T12:00:00Z"
}
```

`start_fix` MUST NOT accept raw prompts, executable paths, environment
variables, secret references, arbitrary commands, remote URLs, or credentials.
The prompt is compiled from trusted Slopslap configuration and the typed plan.

The default plan uses an isolated worktree and leaves the successful result
uncommitted. Current-workspace edits, repository-wide scope, commits, pushes,
and pull requests are trusted-policy features and are disabled for generic MCP
callers by default. A delivery-bearing plan is accepted only when server policy
allows the exact operation. In future HTTP mode it additionally requires the
corresponding delivery authorization scope.

After `start_fix`, Slopslap owns the entire accepted lifecycle, including any
delivery recorded in the immutable plan. Reconnects and Slopmochi restarts do
not suppress an accepted delivery. This preserves the current durable delivery
saga rather than introducing a second manual publish command.

### 8.4 Job read tools

`list_jobs` returns bounded job summaries and opaque handles only for jobs
owned by the current workspace and principal.

`get_job` returns the current presentation, targets, attempts, normalized
usage, scope state, verification state, and sanitized issue.

`get_job_log` is cursor-paginated. It exposes normalized sanitized events, not
raw provider transcripts or environment data.

`get_job_diff` is cursor-paginated and returns metadata plus bounded unified
diff fragments. It rejects binary contents and source paths outside the frozen
candidate inventory.

### 8.5 `cancel_job`

Input contains `request_id` and `job_handle`. Cancellation remains job-local,
idempotent, and joins owned provider/process activity. It releases target
reservations according to the existing coordinator policy. Already written
current-workspace changes are not silently reverted.

### 8.6 Agent discovery and admin settings

The operational profile exposes `list_agent_profiles` and
`probe_agent_profile`. Results contain provider descriptors, live sanitized
capabilities, and readiness diagnostics. They never contain credentials,
provider response bodies, environment values, or private executable paths.

Admin mode additionally exposes `get_settings` and `update_settings` using the
revisioned, idempotent patch contract from section 7.7. The patch schema covers
trusted Slopslap agent profiles, prompt templates, concurrency, candidate,
delivery, and publication settings.

Literal credentials are always rejected. Authentication values are references
resolved by trusted Slopslap code at the final adapter boundary. Repository
configuration cannot call these admin tools or supply equivalent fields through
`plan_fix`.

Successful updates atomically persist before live reconfiguration. Reducing
concurrency does not cancel running work. Settings that cannot safely change
while jobs are active return `resource_busy` with the blocking setting names.

### 8.7 Delivery

Delivery is part of the immutable accepted plan and is not a separate MCP tool
in the first release. `plan_fix` accepts only the closed Git and publication
modes already supported by the domain. Branch, base, and remote names are
literal structured fields; raw Git arguments and remote URLs are prohibited.

The delivery object uses these fields:

```json
{
  "git": "uncommitted | current-branch | new-branch",
  "publish": "local | push | pull-request",
  "branch": "optional literal branch"
}
```

`uncommitted` is valid only with `local`. `push` and `pull-request` require a
commit mode. Fields that are irrelevant to the selected modes are rejected,
not ignored. Remote, base branch, draft state, publisher, and message templates
come from trusted Slopslap configuration and are returned in sanitized form by
`plan_fix`; an MCP request cannot replace them.

Immediately before each side effect, the server revalidates candidate
ownership, diff fingerprint, canonical remote identity, branch state, and
provider repository. Delivery authorization is checked when the plan is
created and again when execution reaches delivery. Losing authorization fails
closed rather than silently downgrading the requested result.

Slopslap never auto-merges and never force-updates an existing remote branch.
Create-only push mechanics used to establish a new branch remain permitted.

### 8.8 Scheduler and operational limits

No wall-clock timeout or attempt cap is required for a running local job.
Nevertheless, the MCP server MUST enforce trusted admission limits:

- maximum request and response sizes;
- maximum targets and allowed paths per job;
- maximum queued and running jobs;
- maximum concurrent provider and verifier operations;
- maximum persisted jobs and disk use;
- provider token/tool/output limits where configured; and
- per-principal rate and concurrency limits before HTTP is enabled.

Zero-valued provider budgets may continue to mean no product-imposed limit for
trusted local STDIO. HTTP mode MUST refuse startup if all provider cost/usage
budgets and admission quotas are unlimited.

## 9. Slopmochi integration

### 9.1 Child discovery

For a packaged installation, Slopmochi resolves children relative to its own
canonical executable:

```text
<install>/bin/slopmochi
<install>/bin/slopmark
<install>/bin/slopslap
```

Homebrew wrapper layouts MAY resolve into a private `libexec` tree, but the
resolved child MUST be canonical, regular, executable, installation-owned,
outside the selected repository, and not group/world writable.

Developer overrides are explicit absolute canonical paths in user-owned
configuration. Repository configuration cannot set or override child paths.
Ambient `PATH` is not consulted after composition.

### 9.2 Process lifecycle

Startup order:

1. resolve and validate packaged component manifest;
2. canonicalize the workspace and targets;
3. start Slopmark in admin mode with private pipes and bounded stderr;
4. connect and negotiate MCP, preferring `2026-07-28`;
5. start Slopslap in admin mode and negotiate MCP;
6. perform any idempotent existing-user configuration/state migration;
7. start initial analysis; and
8. render the UI once analysis state or a precise startup error is available.

Slopmark failure is fatal to the session. Slopslap failure degrades agent
features but does not close analysis views.

Shutdown order:

1. stop accepting UI commands;
2. cancel in-flight UI MCP requests;
3. close the Slopslap MCP session without interpreting shutdown as job cancel;
4. close the Slopmark MCP session;
5. close child stdin;
6. wait for children for a bounded teardown grace;
7. terminate and then kill the exact owned process group if necessary; and
8. join stdout/stderr readers before returning.

Unexpected EOF, non-protocol stdout, oversized frames, or a child exit is a
dependency failure. Slopmochi MUST NOT silently replace the child from `PATH`
or switch to an in-process implementation.

### 9.3 UI data model

The UI defines its own view DTOs. MCP responses are translated at the client
boundary; Bubble Tea state MUST NOT store Slopmark or Slopslap internal types.

File watching remains in Slopmochi. A debounced change triggers a new Slopmark
analysis. The first implementation may poll Slopslap job state at one-second
intervals while jobs are active and at a slower interval otherwise. Modern MCP
subscriptions are a later optimization.

## 10. Configuration and state ownership

Configuration splits into:

```text
Slopmark: analysis profiles, component weights, cache policy
Slopslap: agents, prompts, concurrency, candidates, delivery, publication
Slopmochi: appearance, table, interaction, child locations
```

User configuration lives under product-specific private directories. A
repository may contain analysis and safe remediation preferences, but
repository configuration MUST NOT introduce:

- credentials or literal secret values;
- secret references;
- executable paths;
- environment variables;
- provider endpoints;
- network destinations;
- broader filesystem roots;
- publication authority; or
- higher operational limits than trusted user/server policy.

Slopslap owns job and candidate state. Slopmochi owns no authoritative job
state. Slopmark owns analysis caches. Each state directory is created with
owner-only permissions and rejects symlinks where the existing implementation
does so.

### 10.1 Existing-user migration

The first split release performs an idempotent, non-destructive migration from
the paths returned by the current `preferences` and `userdata` packages:

1. Slopmochi reads the legacy combined preference document once.
2. It projects analysis fields into a proposed Slopmark settings document,
   agent/fix/delivery fields into a proposed Slopslap settings document, and UI
   fields into its own settings document.
3. It starts both children in admin mode and applies the proposed documents
   through revisioned settings tools.
4. It writes a private atomic migration receipt containing source fingerprint
   and destination revisions.
5. It never deletes or rewrites the legacy preferences automatically.

If destination settings already exist, migration fills only fields that still
equal product defaults. Conflicting user-owned destination values win and are
reported in a sanitized migration summary. A crash before the receipt causes a
safe replay through compare-and-swap revisions.

Slopmark MAY adopt the existing analysis cache directory in place after
validating ownership and schema. Slopslap MUST discover existing job and
candidate state through the current durable ownership records, acquire the
same repository leases, and rewrite records to its new private state root only
after successful validation. Legacy state is retained until the migrated copy
has survived one clean restart and reconciliation pass; automatic deletion is
deferred.

Migration tests MUST cover partial writes, stale revisions, conflicting new
settings, corrupted legacy files, symlinked state, active candidate recovery,
and a second identical startup.

### 10.2 Slopslap authority policy

Slopslap computes every plan against an immutable authority ceiling selected
at process launch. The ceiling is not inferred from MCP `clientInfo` or tool
annotations.

The default operational profile permits isolated worktrees, target-only scope,
and uncommitted local results. Trusted user configuration may enable broader
scope, current-workspace edits, commits, pushes, and pull requests. Admin mode
used by Slopmochi applies that trusted user policy but still requires each
requested operation to be explicit in the typed plan.

Repository configuration may narrow the ceiling but never widen it. A plan
records the effective ceiling revision. If trusted policy narrows before a
queued job reaches a privileged stage, Slopslap rechecks the current ceiling
and fails closed; policy expansion never retroactively widens an existing job.

## 11. Security requirements

### 11.1 Threat model

Treat all of the following as untrusted:

- MCP requests and client-supplied strings;
- source and repository configuration;
- model/provider output;
- analyzer subprocess output;
- Git remote metadata;
- saved state until ownership and schema validation pass; and
- child stderr and exit text.

Trusted inputs are limited to packaged binaries/assets, trusted user/server
configuration, compiled policy, and credentials resolved at their final use
boundary.

### 11.2 Slopmark invariants

- All source access is beneath the launch-bound canonical workspace or an
  exact canonical target root explicitly admitted at launch.
- MCP cannot change the workspace.
- Cache writes are beneath the private Slopmark state root.
- Analyzer executables come from the verified installation catalog.
- Analyzer children receive an allowlisted, credential-free environment.
- Analysis does not invoke repository-defined commands or package scripts.
- MCP outputs never include arbitrary source contents.

### 11.3 Slopslap invariants

- MCP cannot select executables, environment variables, credentials, provider
  endpoints, or arbitrary prompts.
- The agent never receives Git delivery credentials or inbound MCP tokens.
- Candidate allowed paths are frozen before execution.
- Provider completion never declares scoring or delivery success.
- Verification is a fresh Slopmark result for the exact candidate contents.
- Delivery revalidates the exact diff and remote identity.
- Provider, Git, GitHub, and analyzer subprocess environments are separately
  allowlisted.
- Incoming MCP authorization tokens are never forwarded to any upstream.
- Human-readable job IDs and opaque handles are not authorization decisions.

The current Codex adapter's use of `os.Environ()` MUST be replaced during the
Slopslap extraction. Each adapter declares the environment names it requires;
composition supplies only those names plus a minimal deterministic process
environment.

Repository text, source comments, model output, and provider events are data,
never policy. They cannot widen targets, select credentials or executables,
change delivery authority, or bypass fresh analysis and diff verification.

### 11.4 STDIO profile

STDIO is the secure default. It inherits authority from the launching process
and does not use MCP OAuth. File descriptors are close-on-exec except where
explicitly passed to the intended child. Unrelated grandchildren MUST NOT
inherit the MCP pipes.

The child is launched directly with an argument vector. Shells, command
substitution, shell configuration, and implicit executable lookup are banned.

### 11.5 Future HTTP profile

HTTP MUST NOT be added by merely exposing the STDIO server through a socket.
Before HTTP is enabled, implement:

- TLS except for explicit loopback development;
- MCP OAuth 2.1 and Protected Resource Metadata;
- token audience validation and resource indicators;
- exact issuer validation and PKCE where the product is an OAuth client;
- scopes separating analysis read, jobs read/start/cancel, candidate read, and
  delivery actions;
- principal-bound job and analysis handles;
- tenant-separated caches, persistence, and logs;
- request rate, concurrency, disk, and provider-spend quotas;
- security audit logging; and
- SSRF protections for all discovered authorization URLs.

An unauthenticated non-loopback listener is prohibited.

## 12. Bundle and release format

The Slopmochi release artifact is the consumer bundle:

```text
slopmochi-<bundle-version>-<os>-<arch>/
  bin/
    slopmochi
    slopmark
    slopslap
  analyzers/
    structural/
    typescript/
  component-catalog.json
  manifest.json
  LICENSE
```

`manifest.json` schema version 1:

```json
{
  "schema_version": 1,
  "bundle": "slopmochi",
  "bundle_version": "1.0.0",
  "components": {
    "slopmochi": {"version": "1.0.0", "path": "bin/slopmochi", "sha256": "..."},
    "slopmark": {"version": "1.2.0", "path": "bin/slopmark", "sha256": "..."},
    "slopslap": {"version": "1.1.0", "path": "bin/slopslap", "sha256": "..."}
  },
  "mcp": {
    "preferred": "2026-07-28",
    "supported": ["2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"]
  },
  "report_schemas": [3],
  "files": [
    {"path": "bin/slopmochi", "sha256": "...", "mode": "0755"},
    {"path": "bin/slopmark", "sha256": "...", "mode": "0755"},
    {"path": "bin/slopslap", "sha256": "...", "mode": "0755"},
    {"path": "component-catalog.json", "sha256": "...", "mode": "0644"}
  ]
}
```

The release workflow computes hashes after all files are staged and validates
the manifest before archiving. `files` contains every file in the bundle except
`manifest.json`, including every analyzer/runtime file. Undeclared files fail
validation. Slopmochi validates paths, modes, hashes, and component identities
at startup. Hash verification is REQUIRED for archives and SHOULD be enabled
for installed bundles where wrappers do not alter the binaries.

Separate release artifacts:

- Slopmark standalone contains Slopmark and all analyzer runtimes.
- Slopslap standalone contains Slopslap and the exact Slopmark/analyzer build
  tested with that Slopslap release.
- Slopmochi contains the complete tested stack.

The first migration release may version all artifacts together. Independent
component versioning starts only after compatibility tests and manifest checks
are operational.

## 13. Migration phases

### Phase 0: freeze behavior and add architecture guards

1. Add golden JSON fixtures for the current Slopmark report.
2. Add end-to-end fixtures for baseline, fix iteration, cancellation, and
   delivery reconciliation.
3. Add a dependency check that records current imports and later enforces the
   target dependency direction.
4. Add canonical `v1` JSON Schemas and success/error fixtures under
   `contracts/` for every tool defined in this document.
5. Rename internal analyzer executables from `slopslap-*` to `slopmark-*`, with
   packaging compatibility links for one release if already public.
6. Rename `cmd/slopslap-go` to `cmd/slopmark` before creating the real
   `cmd/slopslap`.

Exit: no user-visible behavior change; all existing tests and golden fixtures
pass.

### Phase 1: isolate Slopmark service and CLI

1. Move report/scoring DTOs into Slopmark ownership.
2. Move CLI parsing into the Slopmark module.
3. Move `runFollow` and fix composition out of Slopmark.
4. Build analyzer catalog paths relative to the Slopmark installation root.
5. Produce and smoke-test a standalone Slopmark archive.

Exit: `go list -deps` for Slopmark contains no Bubble Tea, agent, candidate,
delivery, publisher, or Slopmochi packages. Standalone multi-language analysis
passes from an unpacked archive.

### Phase 2: implement Slopmark MCP

1. Add the official MCP Go SDK.
2. Implement the service adapter and tools in section 7.
3. Add STDIO framing, stderr discipline, lifecycle, and limits.
4. Add product-local typed Slopmark MCP clients generated or validated from
   the canonical contracts.
5. Test every supported protocol version.

Exit: CLI and MCP golden results are equivalent after removal of transport-only
fields. Fuzz and escape tests pass.

### Phase 3: isolate Slopslap

1. Move the fix domain and manager into the Slopslap module.
2. Move provider, candidate, delivery, publisher, store, and recovery code.
3. Replace native analysis imports with Slopslap's typed Slopmark MCP client.
4. Split trusted Slopslap preferences and state roots.
5. Replace inherited child environments with allowlists.

Exit: Slopslap has no Slopmochi or Slopmark-internal imports. Its full manager
test suite passes using both a fake analysis port and a real packaged Slopmark
child.

### Phase 4: implement Slopslap MCP

1. Implement plan/start/read/cancel tools and immutable planned delivery.
2. Add opaque handles, ownership, durable idempotency, and limits.
3. Add MCP error translation and output sanitization.
4. Add protocol compatibility tests.

Exit: a generic MCP client can start, observe, cancel, restart/reconcile, and
deliver a job without Slopmochi. Cross-workspace and handle-hijacking tests
fail closed.

### Phase 5: convert Slopmochi

1. Make the existing `cmd/slopmochi` the real Bubble Tea application.
2. Replace direct analyzer calls with the Slopmark client.
3. Replace direct fix-manager calls with the Slopslap client.
4. Implement child discovery, manifest validation, process lifecycle, and
   degraded Slopslap behavior.
5. Split UI preferences from service preferences.

Exit: Slopmochi imports neither product's internals and passes current visual,
interaction, follow-mode, and agent UX tests through MCP fakes plus packaged
integration tests.

### Phase 6: release the complete bundle

1. Update Make targets to build modules independently.
2. Build all three executables and analyzer runtimes.
3. Generate and validate `manifest.json`.
4. Update archive, Homebrew, checksums, smoke tests, and release scripts.
5. Add standalone Slopmark and Slopslap release jobs.

Exit: clean-machine tests prove all three distributions, and the Slopmochi
bundle does not use ambient product executables.

### Phase 7: consider repository separation

Separate repositories only after two successful releases using the module and
MCP boundaries. Moving a directory to another repository must not require a
domain or protocol redesign.

## 14. Required test matrix

### Unit and fuzz

- path normalization, Unicode aliases, traversal, and symlink swaps;
- MCP schema rejection, unknown fields, size limits, NaN, and deep JSON;
- cursor integrity and expiry;
- handle generation and ownership;
- idempotency replay and conflicting reuse;
- admin tools absent from the operational profile;
- settings compare-and-swap and atomic persistence;
- error sanitization and secret redaction;
- report DTO round trips;
- environment allowlists; and
- manifest path and hash validation.

### Protocol compatibility

For both servers, exercise:

- `2026-07-28` discovery and per-request metadata;
- legacy initialization for every supported revision;
- highest-mutual-version selection;
- unsupported-version errors and supported-version reporting;
- malformed and duplicate JSON-RPC IDs;
- cancellation and process EOF;
- stdout contamination and oversized frames; and
- identical tool semantics across modern and legacy eras.

### Security integration

- absolute and relative workspace escape attempts;
- symlink and time-of-check/time-of-use swaps;
- source, environment, provider, Git, and OAuth secret leakage;
- agent writes outside allowed scope;
- forged candidate and saved job state;
- cross-principal and cross-workspace handle use;
- attempted admin-mode use on a network transport;
- duplicate mutation after reconnect/restart;
- remote identity changes between plan and delivery;
- cancellation while provider, verification, push, or publication is active;
- malicious provider/analyzer output; and
- process descendants surviving cancellation.

### Packaging

- Slopmark standalone analyzes Go, Java, Rust, and TypeScript;
- Slopslap standalone launches only its packaged/configured Slopmark;
- Slopmochi launches both packaged children with ambient `PATH` cleared;
- Slopmochi remains usable when Slopslap is deliberately absent;
- tampered manifest/component startup fails clearly;
- `--help` and `version --json` work for all executables; and
- archives contain no build caches, credentials, writable executable, or
  repository-only path.

### Quality gates

- `go test ./...` independently in each module;
- race tests for Slopslap coordinator, MCP sessions, and Slopmochi clients;
- all Java, Rust, TypeScript, and Go analyzer tests;
- protocol conformance tests for the official SDK version in use;
- `git diff --check`; and
- an architecture test enforcing permitted imports.

## 15. Observability and privacy

Each product logs structured diagnostics to stderr. Default logs contain:

- product/version;
- operation name;
- redacted handle prefix;
- duration and outcome;
- stable error code; and
- bounded resource counts.

Logs MUST NOT contain source contents, prompts, provider transcripts, MCP
authorization headers, environment values, API keys, Git credential URLs, or
full opaque handles. Debug protocol logging is explicit, off by default, and
must still redact reserved metadata and tool arguments that may contain source
paths or diff content.

## 16. Deliberately deferred work

- network/HTTP MCP transports;
- multi-tenant operation;
- MCP Tasks and modern subscriptions;
- a Slopslap human CLI;
- shared Slopmark process multiplexing;
- automatic component downloading or updating;
- separate source repositories; and
- additional agent or publication providers.

None of these deferrals may be approximated with an unauthenticated listener,
ambient executable lookup, shared writable state, or unbounded generic command
tool.

## 17. Definition of done

The split is complete when:

1. each product builds and tests as an independent Go module;
2. Slopmark works as a standalone CI CLI and MCP server;
3. Slopslap works as a standalone MCP server using Slopmark through MCP;
4. Slopmochi uses only MCP to consume the two services;
5. the Slopmochi release contains all three tested executables;
6. `2026-07-28` is negotiated by default with tested legacy fallback;
7. no product selects privileged executables from ambient `PATH`;
8. security integration tests in section 14 pass;
9. existing analysis, follow-mode, fix, cancellation, recovery, and delivery
   behavior remains covered; and
10. standalone and bundled artifacts pass clean-machine smoke tests.

## References

- MCP `2026-07-28` changelog:
  <https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/changelog.mdx>
- MCP authorization security considerations:
  <https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2026-07-28/basic/authorization/security-considerations.mdx>
- MCP security best practices:
  <https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/docs/2026-07-28/tutorials/security/security_best_practices.mdx>
- Official Go SDK compatibility:
  <https://github.com/modelcontextprotocol/go-sdk/blob/main/README.md>
- Official Go SDK protocol lifecycle:
  <https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/protocol.md>
