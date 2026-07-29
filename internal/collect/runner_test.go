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

// RunEnv adds to the ambient environment rather than replacing it: gh needs PATH
// and HOME to find its config at all, so a bare environment would break it.
func TestLocalRunnerRunEnvAddsToTheAmbientEnvironment(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	t.Setenv("WD_AMBIENT", "ambient")
	out, err := LocalRunner{}.RunEnv("", []string{"WD_EXTRA=extra"},
		[]string{"sh", "-c", `printf '%s %s\n' "$WD_AMBIENT" "$WD_EXTRA"`})
	if err != nil {
		t.Fatalf("RunEnv() error = %v", err)
	}
	if out != "ambient extra" {
		t.Fatalf("out = %q, want %q", out, "ambient extra")
	}
}

// RunEnv must classify failures exactly like Run, or a caller that reads stderr
// stops being able to tell a missing tool from a rejected credential.
func TestLocalRunnerRunEnvReportsStderrOnFailure(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	_, err := LocalRunner{}.RunEnv("", []string{"WD_EXTRA=extra"},
		[]string{"sh", "-c", "echo boom >&2; exit 1"})
	if err == nil {
		t.Fatal("RunEnv() error = nil, want exit status 1")
	}
	if got := Stderr(err); !strings.Contains(got, "boom") {
		t.Fatalf("Stderr(err) = %q, want it to contain %q", got, "boom")
	}
}

// The interface is what PRs is written against; asserting it here means a
// LocalRunner that drifts out of shape fails at build time.
func TestLocalRunnerIsAnEnvRunner(t *testing.T) {
	var _ EnvRunner = LocalRunner{}
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
