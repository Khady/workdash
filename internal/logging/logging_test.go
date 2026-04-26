package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultLogPathUsesXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	want := filepath.Join(state, "workdash", "workdash.log")
	if got := DefaultLogPath(); got != want {
		t.Fatalf("DefaultLogPath() = %q, want %q", got, want)
	}
}

func TestPrintfCreatesDefaultLogFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	Printf("hello %s", "workdash")

	content, err := os.ReadFile(DefaultLogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "hello workdash") {
		t.Fatalf("unexpected log content: %q", string(content))
	}
}

func TestRedactGHToken(t *testing.T) {
	token := "gho_" + "123456789012345678901234567890123456"
	got := Redact("'env' 'GH_TOKEN=sample-token' GH_TOKEN=second-sample-token " + token + " 'gh'")
	if strings.Contains(got, "sample-token") {
		t.Fatalf("token was not redacted: %q", got)
	}
	if strings.Contains(got, "second-sample-token") {
		t.Fatalf("unquoted token was not redacted: %q", got)
	}
	if strings.Contains(got, token) {
		t.Fatalf("github token was not redacted: %q", got)
	}
	if !strings.Contains(got, "GH_TOKEN=<redacted>") {
		t.Fatalf("unexpected redaction: %q", got)
	}
}

func TestPrintfRotatesLogFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	path := DefaultLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", int(maxLogSize))), 0o600); err != nil {
		t.Fatal(err)
	}

	Printf("new log line")

	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Size() != maxLogSize {
		t.Fatalf("rotated size = %d, want %d", rotated.Size(), maxLogSize)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "new log line") {
		t.Fatalf("unexpected active log content: %q", string(content))
	}
}
