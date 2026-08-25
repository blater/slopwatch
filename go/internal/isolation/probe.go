package isolation

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

const ProbeArgument = "__slopwatch_isolation_probe_v1"

type ProbeRequest struct {
	CandidateRoot  string   `json:"candidate_root"`
	GitCommonDir   string   `json:"git_common_dir"`
	OutsideRoot    string   `json:"outside_root"`
	SensitiveRoots []string `json:"sensitive_roots"`
	Network        string   `json:"network"`
	NetworkAddress string   `json:"network_address"`
	Nonce          string   `json:"nonce"`
}

type ProbeResult struct {
	CandidateWrite   bool `json:"candidate_write"`
	OutsideWrite     bool `json:"outside_write"`
	GitMetadataRead  bool `json:"git_metadata_read"`
	GitMetadataWrite bool `json:"git_metadata_write"`
	SensitiveRead    bool `json:"sensitive_read"`
	NetworkConnected bool `json:"network_connected"`
}

// ProbeMain runs inside the provider's tool sandbox and reports attempted
// operations. It is intentionally implemented without a shell so paths cannot
// alter probe behavior.
func ProbeMain(arguments []string) (handled bool, exitCode int) {
	if len(arguments) != 1 || arguments[0] != ProbeArgument {
		return false, 0
	}
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	var request ProbeRequest
	if err := decoder.Decode(&request); err != nil {
		fmt.Fprintln(os.Stderr, ErrSupervisorProtocol)
		return true, 125
	}
	if request.Nonce == "" || request.CandidateRoot == "" || request.GitCommonDir == "" || request.OutsideRoot == "" {
		fmt.Fprintln(os.Stderr, ErrInvalidRequest)
		return true, 125
	}
	result := ProbeResult{}
	result.CandidateWrite = canCreateSentinel(request.CandidateRoot, request.Nonce)
	result.OutsideWrite = canCreateSentinel(request.OutsideRoot, request.Nonce)
	result.GitMetadataRead = canOpen(filepath.Join(request.CandidateRoot, ".git")) || canOpen(request.GitCommonDir)
	result.GitMetadataWrite = canCreateSentinel(request.GitCommonDir, request.Nonce)
	for _, root := range request.SensitiveRoots {
		if canOpen(root) {
			result.SensitiveRead = true
			break
		}
	}
	if request.NetworkAddress != "" {
		network := request.Network
		if network == "" {
			network = "tcp"
		}
		connection, err := net.DialTimeout(network, request.NetworkAddress, 500*time.Millisecond)
		if err == nil {
			result.NetworkConnected = true
			_ = connection.Close()
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return true, 125
	}
	return true, 0
}

func NewProbeNonce() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func canCreateSentinel(root, nonce string) bool {
	path := filepath.Join(root, ".slopwatch-conformance-"+nonce)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	_ = file.Close()
	_ = os.Remove(path)
	return true
}

func canOpen(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}
