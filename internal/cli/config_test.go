package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

func TestExplicitConfigTakesPrecedence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"username":"vit"}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeRawConfig(t, filepath.Join(home, ".config", "vikunja", "config.json"), `{}`, 0o600)
	explicit := writeConfig(t, t.TempDir(), server.URL, 0o600)
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": explicit}, home, server.Client())

	assertExit(t, result, 0)
	assertJSONEqual(t, result.stdout, `{"id":1,"username":"vit"}`)
	assertNoSecret(t, result)
}

func TestDefaultConfigUsesUserHome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":2,"username":"aninka"}`))
	}))
	defer server.Close()

	home := t.TempDir()
	configPath := filepath.Join(home, ".config", "vikunja", "config.json")
	writeRawConfig(t, configPath, `{"url":"`+server.URL+`","token":"`+testToken+`"}`, 0o600)
	result := runCLI(t, []string{"user", "get"}, nil, home, server.Client())

	assertExit(t, result, 0)
	assertJSONEqual(t, result.stdout, `{"id":2,"username":"aninka"}`)
	assertNoSecret(t, result)
}

func TestUnixSecureCredentialModesAreAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission contract")
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		t.Run(mode.String(), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":1}`))
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, mode)
			result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			assertExit(t, result, 0)
		})
	}
}

func TestUnixInsecureCredentialModesAreRejectedBeforeHTTP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission contract")
	}
	for _, mode := range []os.FileMode{0o700, 0o644, 0o620, 0o604, 0o100} {
		t.Run(mode.String(), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			configPath := writeConfig(t, t.TempDir(), server.URL, mode)
			result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), server.Client())
			assertErrorEnvelope(t, result, "configuration", 3)
			assertNoSecret(t, result)
			if requests.Load() != 0 {
				t.Fatalf("HTTP requests = %d, want 0", requests.Load())
			}
		})
	}
}

func TestNonRegularCredentialFileIsRejected(t *testing.T) {
	configPath := t.TempDir()
	result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": configPath}, t.TempDir(), http.DefaultClient)
	assertErrorEnvelope(t, result, "configuration", 3)
}

func TestInvalidConfigurationIsStructuredAndOffline(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"url":`},
		{name: "missing URL", body: `{"token":"` + testToken + `"}`},
		{name: "missing token", body: `{"url":"https://vikunja.example"}`},
		{name: "URL with path", body: `{"url":"https://vikunja.example/base","token":"x"}`},
		{name: "URL with empty query", body: `{"url":"https://vikunja.example?","token":"x"}`},
		{name: "URL with user info", body: `{"url":"https://user@vikunja.example","token":"x"}`},
		{name: "unsupported scheme", body: `{"url":"file:///tmp/socket","token":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeRawConfig(t, path, tt.body, 0o600)
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("HTTP must not be called")
				return nil, nil
			})}
			result := runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": path}, t.TempDir(), client)
			assertErrorEnvelope(t, result, "configuration", 3)
			assertNoSecret(t, result)
		})
	}
}
