package collect

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/workspace"
)

// fakeRunner answers only the commands a test declares. An unmapped argv is a
// test failure, not an empty string: a collector that changes the command it
// runs must not keep passing tests written against the old one.
type fakeRunner struct {
	t   *testing.T
	cwd string // the directory every command is expected to run in
	out map[string]string
}

func (f fakeRunner) Run(cwd string, argv []string) (string, error) {
	f.t.Helper()
	key := strings.Join(argv, " ")
	out, ok := f.out[key]
	if !ok {
		f.t.Fatalf("unexpected command %q; mapped commands: %v", key, f.keys())
	}
	if cwd != f.cwd {
		f.t.Fatalf("command %q ran in cwd %q, want %q", key, cwd, f.cwd)
	}
	return out, nil
}

func (f fakeRunner) keys() []string {
	keys := make([]string, 0, len(f.out))
	for key := range f.out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestGitCollectsWorktreesAndBranches(t *testing.T) {
	runner := fakeRunner{t: t, cwd: "/repo", out: map[string]string{
		strings.Join(worktreeArgv, " "): "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
			"worktree /wt/topic\nHEAD def456\nbranch refs/heads/topic\n",
		strings.Join(branchArgv, " "): "main\ntopic\nidle\n",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	snap := workspace.Index(records, nil)
	if len(snap.Workspaces) != 3 {
		t.Fatalf("got %d workspaces, want 3", len(snap.Workspaces))
	}
	byBranch := map[string]workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		byBranch[ws.ID.Branch] = ws
	}
	if wt := byBranch["main"].Worktree; wt == nil || !wt.IsMain || wt.Path != "/repo" {
		t.Fatalf("main worktree = %+v", wt)
	}
	if wt := byBranch["topic"].Worktree; wt == nil || wt.IsMain || wt.Head != "def456" {
		t.Fatalf("topic worktree = %+v", wt)
	}
	if byBranch["idle"].Has(workspace.FacetWorktree) {
		t.Fatal("branch without a worktree should have no worktree facet")
	}
}

func TestGitSkipsDetachedWorktrees(t *testing.T) {
	runner := fakeRunner{t: t, cwd: "/repo", out: map[string]string{
		strings.Join(worktreeArgv, " "): "worktree /repo\nHEAD abc123\ndetached\n",
		strings.Join(branchArgv, " "):   "",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}

// A branch sharing its name with a tag is ambiguous, and %(refname:short) would
// answer "heads/v1.0" for it — a name that matches nothing in the worktree
// output, so the two records never merge and the real workspace is shadowed by a
// phantom one.
func TestGitNamesBranchesThatCollideWithTags(t *testing.T) {
	runner := fakeRunner{t: t, cwd: "/repo", out: map[string]string{
		strings.Join(worktreeArgv, " "): "worktree /repo\nHEAD abc123\nbranch refs/heads/v1.0\n",
		strings.Join(branchArgv, " "):   "v1.0\n",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	snap := workspace.Index(records, nil)
	if len(snap.Workspaces) != 1 {
		branches := []string{}
		for _, ws := range snap.Workspaces {
			branches = append(branches, ws.ID.Branch)
		}
		t.Fatalf("got workspaces %v, want just [v1.0]", branches)
	}
	ws := snap.Workspaces[0]
	if ws.ID.Branch != "v1.0" {
		t.Fatalf("branch = %q, want %q", ws.ID.Branch, "v1.0")
	}
	if ws.Worktree == nil || ws.Worktree.Path != "/repo" {
		t.Fatalf("worktree = %+v, want the branch and worktree records merged", ws.Worktree)
	}
}

// In a bare layout git lists the repository itself first, as a `bare` block with
// no checkout. It is not a worktree, so it must not consume the main slot and
// leave the first real checkout looking like a secondary one.
func TestGitBareBlockDoesNotConsumeMainSlot(t *testing.T) {
	runner := fakeRunner{t: t, cwd: "/repo/bare.git", out: map[string]string{
		strings.Join(worktreeArgv, " "): "worktree /repo/bare.git\nbare\n\n" +
			"worktree /repo/wt-main\nHEAD abc123\nbranch refs/heads/main\n\n" +
			"worktree /repo/wt-topic\nHEAD def456\nbranch refs/heads/topic\n",
		strings.Join(branchArgv, " "): "main\ntopic\n",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo/bare.git", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	snap := workspace.Index(records, nil)
	byBranch := map[string]workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		byBranch[ws.ID.Branch] = ws
	}
	if len(snap.Workspaces) != 2 {
		t.Fatalf("got %d workspaces, want 2 (the bare repo is not a workspace)", len(snap.Workspaces))
	}
	if wt := byBranch["main"].Worktree; wt == nil || !wt.IsMain || wt.Path != "/repo/wt-main" {
		t.Fatalf("main worktree = %+v, want IsMain at /repo/wt-main", wt)
	}
	if wt := byBranch["topic"].Worktree; wt == nil || wt.IsMain {
		t.Fatalf("topic worktree = %+v, want a non-main worktree", wt)
	}
}

// The forms below are all real: scp-style ssh with and without the .git suffix,
// https with and without it, and the ssh:// URL form ahrefs hosts use. Anything
// unrecognised must come back empty rather than half-parsed — a repo with no
// remote at all prints nothing and exits non-zero, which the transport files as
// an empty section.
func TestParseFullName(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{"scp style", "git@github.com:Khady/workdash.git", "Khady/workdash"},
		{"scp style without suffix", "git@github.com:Khady/workdash", "Khady/workdash"},
		{"https", "https://github.com/ocaml/dune.git", "ocaml/dune"},
		{"https without suffix", "https://github.com/ocaml/dune", "ocaml/dune"},
		{"ssh url", "ssh://git@github.com/ahrefs/monorepo.git", "ahrefs/monorepo"},
		{"trailing newline", "git@github.com:Khady/workdash.git\n", "Khady/workdash"},
		{"no remote", "", ""},
		{"not a url", "origin", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseFullName(test.out); got != test.want {
				t.Fatalf("parseFullName(%q) = %q, want %q", test.out, got, test.want)
			}
		})
	}
}

// The commit time comes from the branch listing itself, so recency costs no extra
// command and no extra ssh round trip.
func TestParseBranchesReadsCommitTime(t *testing.T) {
	records := parseBranches("main\t1700000000\ntopic\t1800000000\n", "local", Repo{Root: "/repo", Label: "repo"})
	if len(records) != 2 {
		t.Fatalf("records = %+v, want 2", records)
	}
	byBranch := map[string]*time.Time{}
	for _, record := range records {
		byBranch[record.ID.Branch] = record.CommittedAt
	}
	if got := byBranch["main"]; got == nil || got.Unix() != 1700000000 {
		t.Fatalf("main committed at = %v", got)
	}
	if got := byBranch["topic"]; got == nil || got.Unix() != 1800000000 {
		t.Fatalf("topic committed at = %v", got)
	}
}

// An older git, or a ref with no date, must still yield the branch.
func TestParseBranchesToleratesAMissingCommitTime(t *testing.T) {
	records := parseBranches("main\ntopic\t\n", "local", Repo{Root: "/repo", Label: "repo"})
	if len(records) != 2 {
		t.Fatalf("records = %+v, want both branches", records)
	}
	for _, record := range records {
		if record.CommittedAt != nil {
			t.Fatalf("record %+v should have no date", record)
		}
	}
}

func TestBranchArgvAsksForTheCommitDate(t *testing.T) {
	joined := strings.Join(branchArgv, " ")
	if !strings.Contains(joined, "committerdate:unix") {
		t.Fatalf("branch argv = %q, want the commit date in the format", joined)
	}
}

// The upstream and the ahead count ride along in the same for-each-ref, exactly
// as the old dashboard got them: "+1 unpushed" is a thing you want to see before
// deleting a worktree, and it costs no extra command.
func TestParseBranchesReadsUpstreamAndAhead(t *testing.T) {
	out := "main\t1700000000\torigin/main\t\n" +
		"topic\t1700000001\torigin/topic\t[ahead 3]\n" +
		"mixed\t1700000002\torigin/mixed\t[ahead 2, behind 5]\n" +
		"local-only\t1700000003\t\t\n"
	records := parseBranches(out, "local", Repo{Root: "/repo", Label: "repo"})
	if len(records) != 4 {
		t.Fatalf("records = %d, want 4", len(records))
	}
	byBranch := map[string]workspace.GitRecord{}
	for _, r := range records {
		byBranch[r.ID.Branch] = r
	}
	if got := byBranch["main"]; got.Upstream != "origin/main" || got.Ahead != 0 {
		t.Fatalf("main = upstream %q ahead %d, want origin/main and 0", got.Upstream, got.Ahead)
	}
	if got := byBranch["topic"]; got.Ahead != 3 {
		t.Fatalf("topic ahead = %d, want 3", got.Ahead)
	}
	if got := byBranch["mixed"]; got.Ahead != 2 {
		t.Fatalf("mixed ahead = %d, want 2 (behind is not unpushed)", got.Ahead)
	}
	if got := byBranch["local-only"]; got.Upstream != "" || got.Ahead != 0 {
		t.Fatalf("local-only = upstream %q ahead %d, want empty and 0", got.Upstream, got.Ahead)
	}
}

func TestBranchArgvAsksForUpstreamAndTrack(t *testing.T) {
	joined := strings.Join(branchArgv, " ")
	for _, want := range []string{"upstream:short", "upstream:track"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("branch argv = %q, want %s in the format", joined, want)
		}
	}
}
