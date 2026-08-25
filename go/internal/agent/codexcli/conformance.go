package codexcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/isolation"
)

// ExecutableChecker runs a Go sentinel helper under `codex sandbox` using the
// exact permission arguments that will protect a job. Crash containment is
// obtained only from the shared platform candidate-confinement boundary; the
// provider adapter never infers it from process groups or prompt flags.
type ExecutableChecker struct {
	Runner           isolation.Executor
	HelperExecutable string
	Confinement      isolation.CandidateConfinement
	Getenv           func(string) string
	Listen           func(string, string) (net.Listener, error)
	Network          string
}

func (checker ExecutableChecker) Check(ctx context.Context, request isolation.ConformanceRequest) isolation.Conformance {
	failed := isolation.Conformance{Diagnostic: "Codex confinement probe was not executed"}
	if checker.Runner == nil || request.Executable == "" || len(request.ProfileArguments) == 0 ||
		request.CandidateRoot == "" || request.GitCommonDir == "" || request.OutsideRoot == "" || len(request.SensitiveRoots) == 0 {
		return failed
	}
	if request.Limits.WallTime <= 0 || request.Limits.TerminateGrace <= 0 || request.Limits.MaxStdoutBytes <= 0 || request.Limits.MaxStderrBytes <= 0 {
		failed.Diagnostic = "Codex confinement probe limits are not configured"
		return failed
	}
	for _, argument := range request.ProfileArguments {
		if strings.Contains(argument, "danger-full-access") || strings.Contains(argument, "bypass-approvals") {
			failed.Diagnostic = "unsafe Codex sandbox argument was rejected"
			return failed
		}
	}
	helper := checker.HelperExecutable
	if helper == "" {
		var err error
		helper, err = os.Executable()
		if err != nil {
			failed.Diagnostic = err.Error()
			return failed
		}
	}
	helper, err := filepath.Abs(helper)
	if err != nil {
		failed.Diagnostic = err.Error()
		return failed
	}
	network := checker.Network
	if network == "" {
		network = "tcp"
	}
	address := "127.0.0.1:0"
	if network == "unix" {
		address = filepath.Join(request.OutsideRoot, "slopwatch-conformance.sock")
	}
	listen := checker.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen(network, address)
	if err != nil {
		failed.Diagnostic = fmt.Sprintf("start confinement network sentinel: %v", err)
		return failed
	}
	defer listener.Close()
	accepted := make(chan bool, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
			accepted <- true
			return
		}
		accepted <- false
	}()
	nonce, err := isolation.NewProbeNonce()
	if err != nil {
		failed.Diagnostic = err.Error()
		return failed
	}
	payload, err := json.Marshal(isolation.ProbeRequest{
		CandidateRoot: request.CandidateRoot, GitCommonDir: request.GitCommonDir,
		OutsideRoot: request.OutsideRoot, SensitiveRoots: request.SensitiveRoots,
		Network: network, NetworkAddress: listener.Addr().String(), Nonce: nonce,
	})
	if err != nil {
		failed.Diagnostic = err.Error()
		return failed
	}
	arguments := append([]string(nil), request.ProfileArguments...)
	arguments = append(arguments, "sandbox", "-P", permissionProfile, "-C", request.CandidateRoot, "--", helper, isolation.ProbeArgument)
	getenv := checker.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	run, err := checker.Runner.Run(ctx, isolation.Request{
		Executable: request.Executable, Arguments: arguments, Directory: request.CandidateRoot,
		Environment: transportEnvironment(getenv), Stdin: payload,
		Limits: request.Limits,
	})
	if err != nil || !run.Successful() || run.StdoutTruncated {
		failed.Diagnostic = probeDiagnostic("Codex confinement helper failed", run, err)
		return failed
	}
	var observed isolation.ProbeResult
	decoder := json.NewDecoder(bytes.NewReader(run.Stdout))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil {
		failed.Diagnostic = fmt.Sprintf("Codex confinement helper returned invalid data: %v", err)
		return failed
	}
	networkConnected := observed.NetworkConnected
	select {
	case connectionAccepted := <-accepted:
		networkConnected = networkConnected || connectionAccepted
	default:
	}
	crashContained := false
	confinementDiagnostic := ""
	if checker.Confinement != nil {
		capability := checker.Confinement.Capability(ctx)
		crashContained = capability.Available && capability.CrashContainment
		confinementDiagnostic = capability.Diagnostic
	}
	result := isolation.Conformance{
		CandidateWrite:       observed.CandidateWrite,
		OutsideWriteDenied:   !observed.OutsideWrite,
		GitMetadataDenied:    !observed.GitMetadataRead && !observed.GitMetadataWrite,
		SensitiveReadsDenied: !observed.SensitiveRead,
		ToolNetworkPolicy:    !networkConnected,
		TransportAuth:        request.TransportAuthVerified && !observed.SensitiveRead,
		CrashContainment:     crashContained,
	}
	if !result.MutationEligible() {
		result.Diagnostic = fmt.Sprintf("Codex confinement gates failed: %v", result.FailedGates())
		if !crashContained && confinementDiagnostic != "" {
			result.Diagnostic += ": " + confinementDiagnostic
		}
	}
	return result
}
