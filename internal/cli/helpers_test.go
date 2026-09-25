package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testToken = "test-token-must-never-appear"

type runResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func runCLI(t *testing.T, args []string, env map[string]string, home string, client *http.Client) runResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	result := runCLIWithWriters(t, args, env, home, client, &stdout, &stderr)
	result.stdout = stdout.String()
	result.stderr = stderr.String()
	return result
}

func runCLIWithWriters(t *testing.T, args []string, env map[string]string, home string, client *http.Client, stdout, stderr io.Writer) runResult {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	return runResult{exitCode: Run(context.Background(), Options{
		Args:       args,
		Stdout:     stdout,
		Stderr:     stderr,
		HTTPClient: client,
		Getenv: func(key string) string {
			return env[key]
		},
		UserHomeDir: func() (string, error) {
			if home == "" {
				return "", errors.New("home unavailable")
			}
			return home, nil
		},
	})}
}

func writeConfig(t *testing.T, dir, serverURL string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	body, err := json.Marshal(map[string]string{"url": serverURL, "token": testToken})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeRawConfig(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertExit(t *testing.T, result runResult, want int) {
	t.Helper()
	if result.exitCode != want {
		t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", result.exitCode, want, result.stdout, result.stderr)
	}
}

func decodeJSON(t *testing.T, raw string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var got any
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("decode JSON %q: %v", raw, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("output has more than one JSON value: %q", raw)
	}
	return got
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	gotJSON := decodeJSON(t, got)
	wantJSON := decodeJSON(t, want)
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("JSON = %#v, want %#v", gotJSON, wantJSON)
	}
}

func assertErrorEnvelope(t *testing.T, result runResult, kind string, exitCode int) map[string]any {
	t.Helper()
	assertExit(t, result, exitCode)
	if result.stdout != "" {
		t.Fatalf("stdout = %q, want empty", result.stdout)
	}
	outer, ok := decodeJSON(t, result.stderr).(map[string]any)
	if !ok || len(outer) != 1 {
		t.Fatalf("error envelope = %#v", outer)
	}
	errObject, ok := outer["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field = %#v", outer["error"])
	}
	if errObject["kind"] != kind || errObject["exit_code"] != float64(exitCode) {
		t.Fatalf("error = %#v, want kind=%q exit_code=%d", errObject, kind, exitCode)
	}
	if _, ok := errObject["message"].(string); !ok {
		t.Fatalf("error message missing: %#v", errObject)
	}
	if !strings.HasSuffix(result.stderr, "\n") {
		t.Fatalf("stderr lacks trailing newline: %q", result.stderr)
	}
	return errObject
}

func assertNoSecret(t *testing.T, result runResult) {
	t.Helper()
	if strings.Contains(result.stdout, testToken) || strings.Contains(result.stderr, testToken) {
		t.Fatalf("credential leaked: stdout=%q stderr=%q", result.stdout, result.stderr)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
