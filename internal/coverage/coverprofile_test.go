package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// On a signal, the handler must run its onSignal callback (cancel + temp-profile
// cleanup) and then exit(1). exit is injected so no real OS signal is sent.
func TestOnSignalCleanupRunsCleanupAndExits(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	defer close(done)

	cleaned := make(chan struct{}, 1)
	exited := make(chan int, 1)
	onSignalCleanup(sigCh, done,
		func() { cleaned <- struct{}{} },
		func(code int) { exited <- code },
	)

	sigCh <- os.Interrupt
	select {
	case <-cleaned:
	case <-time.After(2 * time.Second):
		t.Fatal("onSignal callback was not invoked on signal")
	}
	select {
	case code := <-exited:
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exit was not called on signal")
	}
}

// When done is closed before any signal, the goroutine returns without running
// cleanup or exiting — this is Run's normal-exit path and must not leak/act.
func TestOnSignalCleanupStopsOnDone(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})

	acted := make(chan struct{}, 2)
	onSignalCleanup(sigCh, done,
		func() { acted <- struct{}{} },
		func(int) { acted <- struct{}{} },
	)

	close(done)
	select {
	case <-acted:
		t.Fatal("onSignal/exit must not run when done fires before a signal")
	case <-time.After(100 * time.Millisecond):
		// ok: the goroutine returned via done without acting.
	}
}

func TestResolveCoverProfileCreatesTempFile(t *testing.T) {
	path, cleanup, err := resolveCoverProfile("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	if !strings.HasPrefix(path, filepath.Clean(os.TempDir())) {
		t.Fatalf("temp profile %q not under temp dir %q", path, os.TempDir())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("temp profile not created: %v", err)
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove temp profile: stat err = %v", err)
	}
}

func TestResolveCoverProfileKeepsConfiguredPath(t *testing.T) {
	path, cleanup, err := resolveCoverProfile("coverage.out")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if path != "coverage.out" {
		t.Fatalf("path = %q, want coverage.out", path)
	}
	// cleanup must be a no-op for an explicitly configured path: it must not
	// create or attempt to delete coverage.out.
	cleanup()
	if _, err := os.Stat("coverage.out"); !os.IsNotExist(err) {
		t.Fatalf("configured path should not be touched; stat err = %v", err)
	}
}
