//go:build unix

package codexcli

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blater/slopwatch/internal/agent"
	"github.com/blater/slopwatch/internal/fix"
	"github.com/blater/slopwatch/internal/isolation"
)

func TestCodexSupervisorHelper(t *testing.T) {
	arguments := argumentsFollowingMarker(os.Args, "--")
	if handled, code := isolation.ProbeMain(arguments); handled {
		os.Exit(code)
	}
	if handled, code := isolation.SupervisorMain(arguments); handled {
		os.Exit(code)
	}
}

func TestExecutableCheckerReportsObservedSandboxFailures(t *testing.T) {
	root := canonicalTestRoot(t)
	candidate := filepath.Join(root, "candidate")
	common := filepath.Join(root, "common")
	outside := filepath.Join(root, "outside")
	sensitive := filepath.Join(root, "sensitive")
	for _, directory := range []string{candidate, common, outside, sensitive} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(candidate, ".git"), []byte("gitdir: "+common+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeCodex := filepath.Join(root, "codex")
	if err := os.WriteFile(fakeCodex, []byte(`#!/bin/sh
while [ "$#" -gt 0 ] && [ "$1" != "--" ]; do shift; done
if [ "$#" -eq 0 ]; then exit 2; fi
shift
exec "$@"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "probe-helper")
	helperScript := "#!/bin/sh\nexec " + shellQuote(os.Args[0]) + " -test.run=TestCodexSupervisorHelper -- \"$@\"\n"
	if err := os.WriteFile(helper, []byte(helperScript), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := isolation.Runner{SupervisorExecutable: os.Args[0], PrefixArguments: []string{"-test.run=TestCodexSupervisorHelper", "--"}}
	checker := ExecutableChecker{
		Runner: runner, HelperExecutable: helper, Network: "unix",
		Confinement: staticConfinement{capability: isolation.ConfinementCapability{Available: true, CrashContainment: true, Backend: "test"}},
		Listen: func(network, _ string) (net.Listener, error) {
			return &inertListener{closed: make(chan struct{})}, nil
		},
	}
	result := checker.Check(t.Context(), isolation.ConformanceRequest{
		Executable:       fakeCodex,
		ProfileArguments: []string{"-c", `default_permissions="slopwatch"`, "-c", `permissions.slopwatch={}`},
		CandidateRoot:    candidate, GitCommonDir: common, OutsideRoot: outside,
		SensitiveRoots: []string{sensitive}, TransportAuthVerified: true,
		Limits: isolation.Limits{WallTime: 5 * time.Second, TerminateGrace: time.Second, MaxStdoutBytes: 1 << 20, MaxStderrBytes: 1 << 20},
	})
	if !result.CandidateWrite {
		t.Fatalf("unconfined fake did not demonstrate the candidate-write control: %#v", result)
	}
	if result.OutsideWriteDenied || result.GitMetadataDenied || result.SensitiveReadsDenied || !result.CrashContainment || result.MutationEligible() {
		t.Fatalf("unconfined fake was incorrectly eligible: %#v", result)
	}
}

type staticConfinement struct {
	capability isolation.ConfinementCapability
}

func (value staticConfinement) Capability(context.Context) isolation.ConfinementCapability {
	return value.capability
}
func (staticConfinement) RunCandidate(context.Context, isolation.CandidatePolicy, isolation.Request) (isolation.Result, isolation.Conformance, error) {
	return isolation.Result{}, isolation.Conformance{}, nil
}

type inertListener struct {
	closed chan struct{}
}

func (listener *inertListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, os.ErrClosed
}

func (listener *inertListener) Close() error {
	select {
	case <-listener.closed:
	default:
		close(listener.closed)
	}
	return nil
}

func (listener *inertListener) Addr() net.Addr { return inertAddress("/private/tmp/no-such-socket") }

type inertAddress string

func (address inertAddress) Network() string { return "unix" }
func (address inertAddress) String() string  { return string(address) }

func TestStrategyContractThroughSupervisorAndFakeExecutable(t *testing.T) {
	root := canonicalTestRoot(t)
	executable := filepath.Join(root, "codex")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'codex-cli 0.149.1\n'
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  printf 'Logged in using ChatGPT\n'
  exit 0
fi
if [ "$1" = "debug" ] && [ "$2" = "models" ]; then
  printf '%s\n' '{"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6-Sol","default_reasoning_level":"low","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}]}]}'
  exit 0
fi
prompt=$(sed -n '1,2p')
if [ "$prompt" != "trusted envelope" ]; then
  printf 'prompt was not provided on stdin\n' >&2
  exit 9
fi
printf '%s\n' '{"type":"thread.started","thread_id":"fake-thread"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"fake complete"}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}'
`
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "candidate")
	common := filepath.Join(root, "common")
	if err := os.Mkdir(candidate, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(common, 0o700); err != nil {
		t.Fatal(err)
	}
	runner := isolation.Runner{
		SupervisorExecutable: os.Args[0],
		PrefixArguments:      []string{"-test.run=TestCodexSupervisorHelper", "--"},
	}
	strategy := New(runner, isolation.CheckerFunc(func(context.Context, isolation.ConformanceRequest) isolation.Conformance {
		return passingConformance()
	}))
	strategy.workingDir = func() (string, error) { return root, nil }
	request := agent.Request{
		JobID: "job", AttemptID: "attempt", Model: "gpt-5.6-sol", Effort: "high", Delegation: agent.DelegationSingle,
		Workspace: fix.CandidateIdentity{RepositoryRoot: candidate, GitCommonDir: common},
		Task:      agent.RemediationTask{Instructions: agent.InstructionDocument{Envelope: "trusted envelope", Objective: "fix it"}},
		Limits:    agent.Limits{MaxOutputBytes: 1 << 20, MaxEvents: 20},
	}
	result := strategy.Execute(t.Context(), agent.Profile{Executable: executable}, request, nil)
	if result.Status != agent.ResultCompleted || result.Summary != "fake complete" || result.SessionReference != "fake-thread" {
		t.Fatalf("Execute() = %#v", result)
	}
}

func argumentsFollowingMarker(arguments []string, marker string) []string {
	for index, value := range arguments {
		if value == marker && index+1 < len(arguments) {
			return arguments[index+1:]
		}
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
