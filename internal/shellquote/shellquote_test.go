package shellquote

import (
	"os/exec"
	"testing"
)

// The property that matters: whatever Quote produces, a real shell must split
// back into exactly the input. Golden strings only pin today's output format.
func TestQuoteRoundTripsThroughBash(t *testing.T) {
	inputs := []string{
		"plain", "two words", "it's", "", "$HOME", "${HOME}", "$(id -u)",
		"`date`", `back\slash`, `a"b`, "*", "a\tb", "a\nb", ";", "|&", "#hash",
		"a'b'c", "~user", "trailing\\", "!bang", "a!!b", "feature/v1.2:rc",
	}
	for _, in := range inputs {
		out, err := exec.Command("bash", "-c", "printf %s "+Quote(in)).Output()
		if err != nil {
			t.Fatalf("Quote(%q) = %q produced an unrunnable command: %v", in, Quote(in), err)
		}
		if string(out) != in {
			t.Fatalf("Quote(%q) = %q round-tripped to %q", in, Quote(in), string(out))
		}
	}
}

func TestJoinQuotesEachArgumentSeparately(t *testing.T) {
	got := Join([]string{"git", "-C", "/wt/my repo", "status"})
	if got != `git -C '/wt/my repo' status` {
		t.Fatalf("Join() = %q", got)
	}
}

func TestJoinRoundTripsThroughBash(t *testing.T) {
	argv := []string{"printf", "%s|", "one", "two words", "it's", "$HOME"}
	out, err := exec.Command("bash", "-c", Join(argv)).Output()
	if err != nil {
		t.Fatalf("Join(%q) produced an unrunnable command: %v", argv, err)
	}
	if string(out) != "one|two words|it's|$HOME|" {
		t.Fatalf("Join(%q) ran as %q", argv, string(out))
	}
}
