package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConflictingPathOwnedIdentifiersFailBeforeConfiguration(t *testing.T) {
	tests := [][]string{
		{"project", "update", "7", "--data", `{"id":8}`},
		{"task", "create", "7", "--data", `{"project_id":8}`},
		{"comment", "update", "42", "8", "--data", `{"task_id":41}`},
	}
	for _, args := range tests {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("HTTP must not be called")
				return nil, nil
			})}
			result := runCLI(t, args, nil, "", client)
			assertErrorEnvelope(t, result, "input", 2)
		})
	}
}

func TestCrossOriginRedirectCannotReceiveToken(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("redirected authorization = %q, want empty", got)
		}
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v2/user", http.StatusFound)
	}))
	defer source.Close()

	configPath := writeConfig(t, t.TempDir(), source.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), source.Client())
	assertErrorEnvelope(t, result, "transport", 4)
	if redirected.Load() != 0 {
		t.Fatalf("redirected requests = %d, want 0", redirected.Load())
	}
	assertNoSecret(t, result)
}

func TestConstrainedClientUsesThirtySecondTimeout(t *testing.T) {
	base := &http.Client{Timeout: time.Second}
	client := constrainedClient(base, &url.URL{Scheme: "https", Host: "vikunja.example"})
	if client.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", client.Timeout)
	}
	if base.Timeout != time.Second {
		t.Fatalf("base client timeout changed to %s", base.Timeout)
	}
}

func TestHelpDoesNotLoadCredentials(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP must not be called")
		return nil, nil
	})}
	result := runCLI(t, []string{"project", "--help"}, nil, "", client)
	assertExit(t, result, 0)
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
	decodeJSON(t, result.stdout)
}

func TestDestructiveHelpDisplaysExactConfirmation(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"project", "delete", "7", "--help"}, want: "project:7"},
		{args: []string{"label", "detach", "42", "9", "--help"}, want: "task:42/label:9"},
		{args: []string{"api", "DELETE", "/projects/7", "--help"}, want: "DELETE:/projects/7"},
	}
	for _, tt := range tests {
		result := runCLI(t, tt.args, nil, "", http.DefaultClient)
		assertExit(t, result, 0)
		if !strings.Contains(result.stdout, tt.want) {
			t.Fatalf("help = %q, want confirmation %q", result.stdout, tt.want)
		}
	}
}

func TestDestructiveHelpHandlesExistingFlags(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"project", "delete", "7", "--confirm", "wrong", "--help"}, want: "project:7"},
		{args: []string{"api", "DELETE", "--data", `{}`, "/projects/7", "--confirm", "wrong", "--help"}, want: "DELETE:/projects/7"},
	}
	for _, tt := range tests {
		result := runCLI(t, tt.args, nil, "", http.DefaultClient)
		assertExit(t, result, 0)
		if !strings.Contains(result.stdout, tt.want) || strings.Count(result.stdout, "--confirm") != 1 {
			t.Fatalf("help = %q, want one confirmation for %q", result.stdout, tt.want)
		}
	}
}

func TestHostlessOriginIsRejectedBeforeHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, t.TempDir(), "http://:"+parsed.Port(), 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": path}, t.TempDir(), server.Client())
	assertErrorEnvelope(t, result, "configuration", 3)
	if requests.Load() != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests.Load())
	}
}

func TestTokenIsRedactedFromErrorDetailKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":{"` + testToken + `":"bad"}}`))
	}))
	defer server.Close()
	path := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": path}, t.TempDir(), server.Client())
	assertErrorEnvelope(t, result, "api", 6)
	assertNoSecret(t, result)
}

func TestRFC9457DetailIsReportedAndSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":"invalid request","detail":"title is required; token ` + testToken + `"}`))
	}))
	defer server.Close()
	path := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"project", "create", "--data", `{}`}, map[string]string{"VIKUNJA_CONFIG": path}, t.TempDir(), server.Client())
	errorObject := assertErrorEnvelope(t, result, "api", 6)
	message, _ := errorObject["message"].(string)
	if !strings.Contains(message, "title is required") {
		t.Fatalf("message = %q, want RFC9457 detail", message)
	}
	assertNoSecret(t, result)
}

func TestNonStrictRFC3339TimestampsAreRejectedOffline(t *testing.T) {
	for _, timestamp := range []string{
		"2026-09-02T15:30:00,123Z",
		"2026-09-02T15:30:00+24:00",
		"2026-09-02T15:30:00+01:60",
	} {
		result := runCLI(t, []string{"task", "update", "42", "--data", `{"due_date":"` + timestamp + `"}`}, nil, "", http.DefaultClient)
		assertErrorEnvelope(t, result, "input", 2)
	}
}

func TestRFC3339ArbitraryFractionalPrecisionIsPreserved(t *testing.T) {
	result := runRequestExpectation(t, requestExpectation{
		args:        []string{"task", "update", "42", "--data", `{"due_date":"2026-09-02T00:30:00.123456789012+02:00"}`},
		method:      http.MethodPatch,
		requestURI:  "/api/v2/tasks/42",
		contentType: "application/merge-patch+json",
		body:        `{"due_date":"2026-09-01T22:30:00.123456789012Z"}`,
		response:    `{"id":42}`,
	})
	assertExit(t, result, 0)
}

func TestDeeplyEncodedTraversalIsRejectedOffline(t *testing.T) {
	target := "/%2e%2e/user"
	for range 8 {
		target = strings.ReplaceAll(target, "%", "%25")
	}
	result := runCLI(t, []string{"api", "GET", target}, nil, "", http.DefaultClient)
	assertErrorEnvelope(t, result, "input", 2)
}

func TestDeeplyEncodedRedirectIsRejectedBeforeFollowup(t *testing.T) {
	target := "/api/v2/%2e%2e/%2e%2e/login"
	for range 8 {
		target = strings.ReplaceAll(target, "%", "%25")
	}
	var followups atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/user" {
			w.Header().Set("Location", target)
			w.WriteHeader(http.StatusFound)
			return
		}
		followups.Add(1)
	}))
	defer server.Close()
	path := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": path}, t.TempDir(), server.Client())
	assertErrorEnvelope(t, result, "transport", 4)
	if followups.Load() != 0 {
		t.Fatalf("redirect followups = %d, want 0", followups.Load())
	}
}

func TestOversizedErrorsRetainHTTPExitClassification(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     make(http.Header),
					Body:       io.NopCloser(io.LimitReader(repeatReader('x'), maxResponseSize+1)),
				}, nil
			})}
			path := writeConfig(t, t.TempDir(), "https://vikunja.example", 0o600)
			result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": path}, t.TempDir(), client)
			wantKind, wantExit := "api", 6
			if status == http.StatusUnauthorized || status == http.StatusForbidden {
				wantKind, wantExit = "authentication", 5
			}
			assertErrorEnvelope(t, result, wantKind, wantExit)
		})
	}
}

type repeatReader byte

func (r repeatReader) Read(buffer []byte) (int, error) {
	for i := range buffer {
		buffer[i] = byte(r)
	}
	return len(buffer), nil
}
