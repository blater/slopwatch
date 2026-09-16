package openairesponses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const testSecret = "sk-test-auth-material-must-never-leak"
const testEndpoint = "http://127.0.0.1/v1/responses"

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type scriptedProvider struct {
	t           *testing.T
	mu          sync.Mutex
	responses   []string
	requests    [][]byte
	authHeaders []string
}

func (provider *scriptedProvider) RoundTrip(request *http.Request) (*http.Response, error) {
	provider.t.Helper()
	if request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/models") {
		provider.authHeaders = append(provider.authHeaders, request.Header.Get("Authorization"))
		return testHTTPResponse(request, http.StatusOK, `{"object":"list","data":[{"id":"gpt-test"}]}`), nil
	}
	if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/responses") {
		return testHTTPResponse(request, http.StatusNotFound, `{"error":"unexpected request"}`), nil
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || !json.Valid(body) {
		provider.t.Errorf("decode request: %v", err)
		return testHTTPResponse(request, http.StatusBadRequest, `{"error":"bad request"}`), nil
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.requests = append(provider.requests, append([]byte(nil), body...))
	provider.authHeaders = append(provider.authHeaders, request.Header.Get("Authorization"))
	if len(provider.responses) == 0 {
		provider.t.Error("unexpected provider turn")
		return testHTTPResponse(request, http.StatusInternalServerError, `{"error":"missing script"}`), nil
	}
	response := provider.responses[0]
	provider.responses = provider.responses[1:]
	return testHTTPResponse(request, http.StatusOK, response), nil
}

func testHTTPResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: fmt.Sprintf("%d test", status), Request: request,
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(body)),
	}
}
