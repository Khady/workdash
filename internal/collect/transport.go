package collect

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/shellquote"
)

// sectionMarker prefixes each section's output in the remote stream. It is
// deliberately unlikely to occur in git or tmux output.
const sectionMarker = "___WD_SECTION___"

// Transport runs a whole plan and returns each section's output by key. The
// interface exists so local and remote collection differ only here: the parsers
// downstream never learn which machine the bytes came from.
type Transport interface {
	Run(plan Plan) (map[string]string, error)
}

// Local runs each section directly. Sections are independent, so a failing one
// yields empty output rather than aborting the host — one unreadable repo should
// not hide the other ten.
type Local struct{ Runner Runner }

func (l Local) Run(plan Plan) (map[string]string, error) {
	out := map[string]string{}
	for _, section := range plan.Sections {
		text, _ := l.Runner.Run(section.Cwd, section.Argv)
		out[section.Key] = text
	}
	return out, nil
}

// SSH runs an entire plan in one remote shell.
//
// This is the point of the whole design: measured against a host with eleven
// repos, one ssh exec per git command took 2.72s with a warm ControlMaster,
// while this took 0.086s. It needs nothing installed on the far side — the
// remote requirements are bash, git and tmux, which workdash already assumed.
type SSH struct {
	Runner Runner
	Target string
}

func (s SSH) Run(plan Plan) (map[string]string, error) {
	// The script is quoted for the *remote* shell, not the local exec. ssh joins
	// its command arguments with spaces and hands the result to the remote login
	// shell, so an unquoted script gets re-parsed there: `bash -c` would take
	// only the next word as its command and the rest as positional parameters,
	// silently losing the first section's marker.
	argv := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		s.Target, "bash", "-c", shellquote.Quote(sshScript(plan))}
	text, err := s.Runner.Run("", argv)
	if err != nil {
		return nil, err
	}
	return splitSections(text), nil
}

// sshScript emits a marker, then the section's command, for every section.
//
// Each command is isolated: a `cd` that fails skips only its own command, and
// stderr is dropped so a chatty git cannot be mistaken for output. The script
// never uses `set -e`, because one broken repo must not truncate the host.
func sshScript(plan Plan) string {
	var script strings.Builder
	for _, section := range plan.Sections {
		fmt.Fprintf(&script, "printf '%%s %%s\\n' %s %s\n",
			shellquote.Quote(sectionMarker), shellquote.Quote(section.Key))
		if section.Cwd != "" {
			fmt.Fprintf(&script, "cd -- %s 2>/dev/null && ", shellquote.Quote(section.Cwd))
		}
		fmt.Fprintf(&script, "%s 2>/dev/null\n", shellquote.Join(section.Argv))
	}
	// End deliberately. Without this the script's status is whichever section ran
	// last, which says nothing about the host: a stopped tmux server made the
	// final section exit 1 and the caller threw away eleven repos of git facts
	// that had been collected perfectly well. A genuine ssh failure still shows
	// up — connection problems exit 255, a missing shell 127 — so only the
	// meaningless status is suppressed.
	script.WriteString("exit 0\n")
	return script.String()
}

// splitSections files the stream under section keys, discarding anything before
// the first marker so a remote login banner cannot land in a section.
func splitSections(text string) map[string]string {
	out := map[string]string{}
	key := ""
	lines := []string{}
	flush := func() {
		if key != "" {
			out[key] = strings.Trim(strings.Join(lines, "\n"), "\n")
		}
		lines = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, sectionMarker+" ") {
			flush()
			key = strings.TrimPrefix(line, sectionMarker+" ")
			continue
		}
		if key != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return out
}
