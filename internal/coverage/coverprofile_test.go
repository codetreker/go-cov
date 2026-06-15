package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
