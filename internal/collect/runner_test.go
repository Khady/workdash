package collect

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLocalRunnerErrorIncludesStderr(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// git localises its messages; pin the locale so the assertion is about
	// stderr being captured at all, not about which language it arrived in.
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANGUAGE", "C")
	dir := t.TempDir()
	out, err := LocalRunner{}.Run(dir, []string{"git", "rev-parse", "--git-dir"})
	if err == nil {
		t.Fatalf("Run() in %s returned no error, stdout = %q", dir, out)
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error = %q, want it to quote git's stderr", err)
	}
}

func TestLocalRunnerStderrIsReachableForClassification(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	out, err := LocalRunner{}.Run("", []string{"sh", "-c", "echo kept; echo boom >&2; exit 1"})
	if err == nil {
		t.Fatal("Run() returned no error, want exit status 1")
	}
	if out != "kept" {
		t.Fatalf("stdout = %q, want %q", out, "kept")
	}
	if got := Stderr(err); !strings.Contains(got, "boom") {
		t.Fatalf("Stderr(err) = %q, want it to contain %q", got, "boom")
	}
}
