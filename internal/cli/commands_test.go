package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

type requestExpectation struct {
	name        string
	args        []string
	method      string
	requestURI  string
	contentType string
	body        string
	response    string
	status      int
}

func runRequestExpectation(t *testing.T, tt requestExpectation) runResult {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != tt.method {
			t.Errorf("method = %s, want %s", r.Method, tt.method)
		}
		if r.RequestURI != tt.requestURI {
			t.Errorf("request URI = %q, want %q", r.RequestURI, tt.requestURI)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
			t.Errorf("authorization header = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != tt.contentType {
			t.Errorf("Content-Type = %q, want %q", got, tt.contentType)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if tt.body == "" {
			if len(body) != 0 {
				t.Errorf("body = %q, want empty", body)
			}
		} else {
			assertJSONEqual(t, string(body), tt.body)
		}
		status := tt.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(tt.response))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	return runCLI(t, tt.args, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
}

func TestUserGetUsesAuthenticatedV2Endpoint(t *testing.T) {
	result := runRequestExpectation(t, requestExpectation{
		args:       []string{"user", "get"},
		method:     http.MethodGet,
		requestURI: "/api/v2/user",
		response:   `{"id":1,"username":"vit"}`,
	})
	assertExit(t, result, 0)
	assertJSONEqual(t, result.stdout, `{"id":1,"username":"vit"}`)
}

func TestProjectCommandsUseExpectedRequests(t *testing.T) {
	tests := []requestExpectation{
		{name: "list", args: []string{"project", "list"}, method: http.MethodGet, requestURI: "/api/v2/projects?page=1&per_page=50", response: `{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`},
		{name: "get", args: []string{"project", "get", "7"}, method: http.MethodGet, requestURI: "/api/v2/projects/7", response: `{"id":7,"title":"Home"}`},
		{name: "create", args: []string{"project", "create", "--data", `{"title":"Home"}`}, method: http.MethodPost, requestURI: "/api/v2/projects", contentType: "application/json", body: `{"title":"Home"}`, response: `{"id":7,"title":"Home"}`, status: http.StatusCreated},
		{name: "update omits unspecified fields", args: []string{"project", "update", "7", "--data", `{"title":"Renamed"}`}, method: http.MethodPatch, requestURI: "/api/v2/projects/7", contentType: "application/merge-patch+json", body: `{"title":"Renamed"}`, response: `{"id":7,"title":"Renamed"}`},
		{name: "delete", args: []string{"project", "delete", "7", "--confirm", "project:7"}, method: http.MethodDelete, requestURI: "/api/v2/projects/7", response: "", status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runRequestExpectation(t, tt)
			assertExit(t, result, 0)
			if tt.status == http.StatusNoContent {
				assertJSONEqual(t, result.stdout, "null")
			}
		})
	}
}

func TestTaskCommandsUseExpectedRequests(t *testing.T) {
	tests := []requestExpectation{
		{name: "global list", args: []string{"task", "list", "--page", "3", "--per-page", "25", "--query", "milk & eggs"}, method: http.MethodGet, requestURI: "/api/v2/tasks?page=3&per_page=25&q=milk+%26+eggs", response: `{"items":[],"total":0,"page":3,"per_page":25,"total_pages":0}`},
		{name: "project list", args: []string{"task", "list", "--project", "7"}, method: http.MethodGet, requestURI: "/api/v2/projects/7/tasks?page=1&per_page=50", response: `{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`},
		{name: "get", args: []string{"task", "get", "42"}, method: http.MethodGet, requestURI: "/api/v2/tasks/42", response: `{"id":42,"title":"Buy milk"}`},
		{name: "create", args: []string{"task", "create", "7", "--data", `{"title":"Buy milk"}`}, method: http.MethodPost, requestURI: "/api/v2/projects/7/tasks", contentType: "application/json", body: `{"title":"Buy milk"}`, response: `{"id":42,"title":"Buy milk"}`, status: http.StatusCreated},
		{name: "update omits unspecified fields", args: []string{"task", "update", "42", "--data", `{"done":true}`}, method: http.MethodPatch, requestURI: "/api/v2/tasks/42", contentType: "application/merge-patch+json", body: `{"done":true}`, response: `{"id":42,"done":true}`},
		{name: "delete", args: []string{"task", "delete", "42", "--confirm", "task:42"}, method: http.MethodDelete, requestURI: "/api/v2/tasks/42", response: "", status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runRequestExpectation(t, tt)
			assertExit(t, result, 0)
		})
	}
}

func TestTaskTimestampsNormalizeToUTCWithoutLosingPrecision(t *testing.T) {
	result := runRequestExpectation(t, requestExpectation{
		args:        []string{"task", "update", "42", "--data", `{"due_date":"2026-09-02T15:30:00.123456789+02:00","start_date":null}`},
		method:      http.MethodPatch,
		requestURI:  "/api/v2/tasks/42",
		contentType: "application/merge-patch+json",
		body:        `{"due_date":"2026-09-02T13:30:00.123456789Z","start_date":null}`,
		response:    `{"id":42}`,
	})
	assertExit(t, result, 0)
}

func TestLabelCommandsUseExpectedRequests(t *testing.T) {
	tests := []requestExpectation{
		{name: "global list", args: []string{"label", "list"}, method: http.MethodGet, requestURI: "/api/v2/labels?page=1&per_page=50", response: `{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`},
		{name: "task list", args: []string{"label", "list", "--task", "42"}, method: http.MethodGet, requestURI: "/api/v2/tasks/42/labels?page=1&per_page=50", response: `{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`},
		{name: "get", args: []string{"label", "get", "9"}, method: http.MethodGet, requestURI: "/api/v2/labels/9", response: `{"id":9,"title":"urgent"}`},
		{name: "create", args: []string{"label", "create", "--data", `{"title":"urgent"}`}, method: http.MethodPost, requestURI: "/api/v2/labels", contentType: "application/json", body: `{"title":"urgent"}`, response: `{"id":9,"title":"urgent"}`, status: http.StatusCreated},
		{name: "update", args: []string{"label", "update", "9", "--data", `{"hex_color":"ff0000"}`}, method: http.MethodPatch, requestURI: "/api/v2/labels/9", contentType: "application/merge-patch+json", body: `{"hex_color":"ff0000"}`, response: `{"id":9}`},
		{name: "delete", args: []string{"label", "delete", "9", "--confirm", "label:9"}, method: http.MethodDelete, requestURI: "/api/v2/labels/9", response: "", status: http.StatusNoContent},
		{name: "attach", args: []string{"label", "attach", "42", "9"}, method: http.MethodPost, requestURI: "/api/v2/tasks/42/labels", contentType: "application/json", body: `{"label_id":9}`, response: `{"label_id":9,"task_id":42}`, status: http.StatusCreated},
		{name: "detach", args: []string{"label", "detach", "42", "9", "--confirm", "task:42/label:9"}, method: http.MethodDelete, requestURI: "/api/v2/tasks/42/labels/9", response: "", status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runRequestExpectation(t, tt)
			assertExit(t, result, 0)
		})
	}
}

func TestCommentCommandsUseExpectedRequests(t *testing.T) {
	tests := []requestExpectation{
		{name: "list", args: []string{"comment", "list", "42"}, method: http.MethodGet, requestURI: "/api/v2/tasks/42/comments?page=1&per_page=50", response: `{"items":[],"total":0,"page":1,"per_page":50,"total_pages":0}`},
		{name: "get", args: []string{"comment", "get", "42", "8"}, method: http.MethodGet, requestURI: "/api/v2/tasks/42/comments/8", response: `{"id":8,"comment":"Hello"}`},
		{name: "create", args: []string{"comment", "create", "42", "--data", `{"comment":"Hello"}`}, method: http.MethodPost, requestURI: "/api/v2/tasks/42/comments", contentType: "application/json", body: `{"comment":"Hello"}`, response: `{"id":8,"comment":"Hello"}`, status: http.StatusCreated},
		{name: "update omits unspecified fields", args: []string{"comment", "update", "42", "8", "--data", `{"comment":"Edited"}`}, method: http.MethodPatch, requestURI: "/api/v2/tasks/42/comments/8", contentType: "application/merge-patch+json", body: `{"comment":"Edited"}`, response: `{"id":8,"comment":"Edited"}`},
		{name: "delete", args: []string{"comment", "delete", "42", "8", "--confirm", "task:42/comment:8"}, method: http.MethodDelete, requestURI: "/api/v2/tasks/42/comments/8", response: "", status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runRequestExpectation(t, tt)
			assertExit(t, result, 0)
		})
	}
}

func TestRawRequestPreservesJSONAndStaysUnderV2(t *testing.T) {
	result := runRequestExpectation(t, requestExpectation{
		args:        []string{"api", "POST", "/projects?expand=permissions", "--data", `{"title":"Raw"}`},
		method:      http.MethodPost,
		requestURI:  "/api/v2/projects?expand=permissions",
		contentType: "application/json",
		body:        `{"title":"Raw"}`,
		response:    `[1,{"unknown":true}]`,
		status:      http.StatusCreated,
	})
	assertExit(t, result, 0)
	assertJSONEqual(t, result.stdout, `[1,{"unknown":true}]`)
}

func TestInvalidInputFailsBeforeConfigurationOrHTTP(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "unknown command", args: []string{"unknown"}},
		{name: "user mutation", args: []string{"user", "delete"}},
		{name: "zero ID", args: []string{"project", "get", "0"}},
		{name: "negative ID", args: []string{"task", "get", "-1"}},
		{name: "non-object resource payload", args: []string{"project", "create", "--data", `[]`}},
		{name: "trailing JSON", args: []string{"label", "create", "--data", `{} {}`}},
		{name: "invalid timestamp", args: []string{"task", "update", "42", "--data", `{"due_date":"tomorrow"}`}},
		{name: "page too low", args: []string{"task", "list", "--page", "0"}},
		{name: "per page too high", args: []string{"project", "list", "--per-page", "1001"}},
		{name: "missing data", args: []string{"comment", "create", "42"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("HTTP must not be called")
				return nil, nil
			})}
			result := runCLI(t, tt.args, nil, "", client)
			assertErrorEnvelope(t, result, "input", 2)
		})
	}
}

func TestUnsafeRawTargetsFailWithoutSendingCredentials(t *testing.T) {
	tests := []string{
		"https://attacker.example/steal",
		"//attacker.example/steal",
		"/../user",
		"/%2e%2e/user",
		"/projects/%2e%2e/%2e%2e/login",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("HTTP must not be called")
				return nil, nil
			})}
			result := runCLI(t, []string{"api", "GET", target}, nil, "", client)
			assertErrorEnvelope(t, result, "input", 2)
			assertNoSecret(t, result)
		})
	}
}

func TestDestructiveCommandsRequireExactConfirmationBeforeHTTP(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "project missing", args: []string{"project", "delete", "7"}},
		{name: "task mismatch", args: []string{"task", "delete", "42", "--confirm", "task:41"}},
		{name: "label mismatch", args: []string{"label", "delete", "9", "--confirm", "label:8"}},
		{name: "detach mismatch", args: []string{"label", "detach", "42", "9", "--confirm", "task:42/label:8"}},
		{name: "comment mismatch", args: []string{"comment", "delete", "42", "8", "--confirm", "task:42/comment:7"}},
		{name: "raw delete mismatch", args: []string{"api", "DELETE", "/projects/7", "--confirm", "DELETE:/projects/8"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
			result := runCLI(t, tt.args, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			assertErrorEnvelope(t, result, "input", 2)
			if requests.Load() != 0 {
				t.Fatalf("HTTP requests = %d, want 0", requests.Load())
			}
		})
	}
}

func TestListOutputRetainsPaginationEnvelope(t *testing.T) {
	want := `{"items":[{"id":1}],"total":51,"page":2,"per_page":50,"total_pages":2}`
	result := runRequestExpectation(t, requestExpectation{
		args:       []string{"project", "list", "--page", "2"},
		method:     http.MethodGet,
		requestURI: "/api/v2/projects?page=2&per_page=50",
		response:   want,
	})
	assertExit(t, result, 0)
	assertJSONEqual(t, result.stdout, want)
}

func TestUpdatePayloadContainsOnlySuppliedFields(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"done":true}`))
	}))
	defer server.Close()
	configPath := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"task", "update", "42", "--data", `{"done":true}`}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
	assertExit(t, result, 0)
	if !reflect.DeepEqual(got, map[string]any{"done": true}) {
		t.Fatalf("payload = %#v, want only done", got)
	}
}

func TestSuccessfulOutputIsOneNewlineTerminatedJSONValue(t *testing.T) {
	result := runRequestExpectation(t, requestExpectation{
		args:       []string{"user", "get"},
		method:     http.MethodGet,
		requestURI: "/api/v2/user",
		response:   `{"id":1}`,
	})
	assertExit(t, result, 0)
	if result.stderr != "" {
		t.Fatalf("stderr = %q, want empty", result.stderr)
	}
	if !strings.HasSuffix(result.stdout, "\n") {
		t.Fatalf("stdout lacks trailing newline: %q", result.stdout)
	}
	decodeJSON(t, result.stdout)
}
