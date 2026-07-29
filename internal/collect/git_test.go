package collect

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

type fakeRunner struct{ out map[string]string }

func (f fakeRunner) Run(cwd string, argv []string) (string, error) {
	return f.out[strings.Join(argv, " ")], nil
}

func TestGitCollectsWorktreesAndBranches(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
		"git worktree list --porcelain": "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
			"worktree /wt/topic\nHEAD def456\nbranch refs/heads/topic\n",
		"git for-each-ref --format=%(refname:short) refs/heads": "main\ntopic\nidle\n",
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
	runner := fakeRunner{out: map[string]string{
		"git worktree list --porcelain":                         "worktree /repo\nHEAD abc123\ndetached\n",
		"git for-each-ref --format=%(refname:short) refs/heads": "",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}
