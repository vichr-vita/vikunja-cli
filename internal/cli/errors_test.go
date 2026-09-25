package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAuthenticationFailuresUseExitFive(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"title":"denied","detail":"invalid authentication"}`))
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
			result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			errObject := assertErrorEnvelope(t, result, "authentication", 5)
			if errObject["http_status"] != float64(status) {
				t.Fatalf("http_status = %#v, want %d", errObject["http_status"], status)
			}
			assertNoSecret(t, result)
		})
	}
}

func TestAPIErrorPreservesStatusCodeAndDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":"Unprocessable Entity","detail":"invalid title","code":1001,"errors":[{"message":"required","location":"body.title"}]}`))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"project", "create", "--data", `{}`}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	errObject := assertErrorEnvelope(t, result, "api", 6)
	if errObject["http_status"] != float64(422) || errObject["code"] != float64(1001) {
		t.Fatalf("API error metadata = %#v", errObject)
	}
	if details, ok := errObject["details"].([]any); !ok || len(details) != 1 {
		t.Fatalf("details = %#v", errObject["details"])
	}
}

func TestTransportFailureUsesExitFour(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}
	configPath := writeConfig(t, t.TempDir(), "https://vikunja.example", 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), client)
	assertErrorEnvelope(t, result, "transport", 4)
	assertNoSecret(t, result)
}

func TestMalformedSuccessResponseUsesExitSeven(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	assertErrorEnvelope(t, result, "protocol", 7)
}

func TestTypedResponsesRejectUnexpectedJSONShapes(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		response string
	}{
		{name: "single resource array", args: []string{"user", "get"}, response: `[]`},
		{name: "single resource null", args: []string{"project", "get", "7"}, response: `null`},
		{name: "list missing fields", args: []string{"project", "list"}, response: `{}`},
		{name: "list wrong item type", args: []string{"task", "list"}, response: `{"items":{},"total":0,"page":1,"per_page":50,"total_pages":0}`},
		{name: "list wrong count type", args: []string{"label", "list"}, response: `{"items":[],"total":"0","page":1,"per_page":50,"total_pages":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
			result := runCLI(t, tt.args, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			assertErrorEnvelope(t, result, "protocol", 7)
		})
	}
}

func TestRawResponseAcceptsAnySingleJSONValue(t *testing.T) {
	for _, response := range []string{`null`, `true`, `42`, `"ok"`, `[]`, `{}`} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(response))
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
			result := runCLI(t, []string{"api", "GET", "/user"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			assertExit(t, result, 0)
			assertJSONEqual(t, result.stdout, response)
		})
	}
}

func TestNoContentSuccessPrintsNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"task", "delete", "42", "--confirm", "task:42"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	assertExit(t, result, 0)
	if result.stdout != "null\n" {
		t.Fatalf("stdout = %q, want null newline", result.stdout)
	}
}

func TestServerCannotReflectTokenIntoErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":"bad token ` + testToken + `","detail":"` + testToken + `","code":1}`))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	assertErrorEnvelope(t, result, "api", 6)
	assertNoSecret(t, result)
}

func TestServerCannotReflectTokenIntoSuccess(t *testing.T) {
	for _, tt := range []struct {
		name     string
		args     []string
		response string
	}{
		{name: "typed", args: []string{"user", "get"}, response: `{"token":"` + testToken + `"}`},
		{name: "raw escaped", args: []string{"api", "GET", "/user"}, response: `{"token":"test-token-must-never-\u0061ppear"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
			result := runCLI(t, tt.args, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			assertErrorEnvelope(t, result, "protocol", 7)
			assertNoSecret(t, result)
		})
	}
}

func TestRedirectCannotReceiveTokenOutsideAPIRoot(t *testing.T) {
	var outsideRequests atomic.Int32
	var outsideAuthorization atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/user" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		outsideRequests.Add(1)
		outsideAuthorization.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	if result.exitCode == 0 {
		t.Fatalf("redirect unexpectedly succeeded: %q", result.stdout)
	}
	if outsideRequests.Load() != 0 {
		t.Fatalf("outside API requests = %d, authorization=%v", outsideRequests.Load(), outsideAuthorization.Load())
	}
	assertNoSecret(t, result)
}

func TestConfirmedDeleteDoesNotFollowRedirect(t *testing.T) {
	var redirected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/projects/7" {
			http.Redirect(w, r, "/api/v2/projects/8", http.StatusTemporaryRedirect)
			return
		}
		redirected.Add(1)
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"project", "delete", "7", "--confirm", "project:7"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	assertErrorEnvelope(t, result, "transport", 4)
	if redirected.Load() != 0 {
		t.Fatalf("redirected DELETE requests = %d, want 0", redirected.Load())
	}
	assertNoSecret(t, result)
}

func TestOutputFailureUsesInternalExitCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	var stderr bytes.Buffer
	result := runCLIWithWriters(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client(), failWriter{}, &stderr)
	result.stderr = stderr.String()
	assertErrorEnvelope(t, result, "internal", 1)
}

func TestResponseBodyReadFailureUsesProtocolExitCode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(errorReader{}),
		}, nil
	})}
	configPath := writeConfig(t, t.TempDir(), "https://vikunja.example", 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), client)
	assertErrorEnvelope(t, result, "protocol", 7)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestErrorOutputRemainsSingleJSONValue(t *testing.T) {
	result := runCLI(t, []string{"project", "get", "invalid"}, nil, "", http.DefaultClient)
	assertErrorEnvelope(t, result, "input", 2)
	if strings.Count(strings.TrimSpace(result.stderr), "\n") != 0 {
		t.Fatalf("stderr contains multiple lines: %q", result.stderr)
	}
}
