//go:build unix

package cli

import (
	"net/http"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFIFOConfigIsRejectedWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()

	done := make(chan runResult, 1)
	go func() {
		done <- runCLI(t, []string{"user", "get"}, map[string]string{"VIKUNJA_CONFIG": path}, home, http.DefaultClient)
	}()

	select {
	case result := <-done:
		assertErrorEnvelope(t, result, "configuration", 3)
	case <-time.After(time.Second):
		t.Fatal("config load blocked on FIFO")
	}
}
