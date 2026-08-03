// Command wd-host is the Firefox native-messaging bridge over the workdash core.
//
// It replaces a daemon-backed host: there is no daemon and no
// socket, because there is nothing for one to own. A snapshot is a projection over
// git, tmux and GitHub, so every request rebuilds it from the same code `wd` and
// `wdt` run — which is what keeps the browser from being a frontend that quietly
// disagrees with the terminal.
//
// Firefox uses one-shot browser.runtime.sendNativeMessage, so this process is
// spawned per message, answers one message and exits. Nothing survives between
// messages, which is why action tokens are references revalidated on use rather
// than capabilities held in memory; see token.go.
//
// The framing is the browser's: a 4-byte length prefix in the platform's native
// byte order — little-endian everywhere workdash runs — followed by that many bytes
// of JSON, on stdin and stdout. Anything written to stdout that is not a framed
// message breaks the channel, so diagnostics go to stderr, which Firefox logs.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Khady/workdash/internal/dispatch"
	"github.com/Khady/workdash/internal/snapshot"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

const maxNativeRequestSize = 4 * 1024 * 1024

// The five request types the extension sends. Named constants because they are a
// wire contract with background.js, not internal spelling.
const (
	typeWarmup        = "warmup"
	typePRActions     = "get_pr_actions"
	typePRSummary     = "get_pr_summary"
	typeExecuteAction = "execute_action"
	typeRefreshAll    = "refresh_browser"
)

type request struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	// Token identifies an action to run; see token.go for why it is a reference.
	Token string `json:"token,omitempty"`
	// Confirmed is sent by the popup for actions that declared a confirmation. No
	// action does yet, so nothing reads it — the field stays because the extension
	// sends it and dropping it would make the popup's message fail to parse the day
	// confirmations arrive.
	Confirmed bool `json:"confirmed,omitempty"`
}

// response keeps the field names the daemon used, because popup.js and
// background.js read them and this host is meant to be a drop-in replacement.
// `ok` carries no omitempty: the extension treats a missing ok as a failure.
type response struct {
	ID    string `json:"id,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	// Status is a short human note about what happened, for a caller driving the
	// host by hand; the popup ignores it.
	Status string `json:"status,omitempty"`
	// LoadedAt is a pointer so an omitted timestamp is really omitted: a zero
	// time.Time serialises as year 1 whatever omitempty says, and the popup would
	// dutifully render "Loaded 00:00" for a response that never collected anything.
	LoadedAt *time.Time `json:"loaded_at,omitempty"`
	// Refreshing tells the popup its pull request data is being caught up behind it.
	Refreshing  bool     `json:"refreshing,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
	Groups      []group  `json:"groups,omitempty"`
	ActionCount int      `json:"action_count,omitempty"`
	// Summary is the one-line answer to get_pr_summary. It is additive: the badge
	// reads action_count, so no extension change was needed to introduce it.
	Summary string `json:"summary,omitempty"`
}

// collectedNow timestamps a response that really did rebuild the snapshot. There
// is no cache to be stale relative to: the answer was collected as this message
// was handled.
func collectedNow() *time.Time {
	at := time.Now()
	return &at
}

func failure(format string, args ...any) response {
	return response{OK: false, Error: fmt.Sprintf(format, args...)}
}

type hostMode int

const (
	modeServe hostMode = iota
	modeRefresh
)

// mode reads the invocation. Only an explicit "refresh" is a subcommand: Firefox
// passes the manifest path — and in some versions the extension id — as arguments,
// so rejecting unrecognised ones made the host exit without writing a frame, which
// the popup reports as Firefox's own "An unexpected error occurred".
func mode(argv []string) hostMode {
	if len(argv) > 1 && argv[1] == "refresh" {
		return modeRefresh
	}
	return modeServe
}

func main() {
	// `wd-host refresh` is not a browser request: it is how the detached background
	// refresh re-execs this binary. snapshot.SpawnBackgroundRefresh runs
	// os.Executable() with "refresh", and under Firefox that executable is this one,
	// so serving the subcommand is what makes "render now, catch up behind you" work
	// for the browser exactly as it does for the CLI.
	if mode(os.Args) == modeRefresh {
		if _, warnings := refreshPRs(); len(warnings) > 0 {
			for _, warning := range warnings {
				fmt.Fprintln(os.Stderr, "wd-host: "+warning)
			}
		}
		return
	}

	req, err := readNativeMessage(os.Stdin)
	if err != nil {
		// A malformed frame still gets a framed answer where one can be written, so
		// the popup shows a reason instead of "did not return a response".
		if err != io.EOF {
			_ = writeNativeMessage(os.Stdout, failure("workdash could not read the request: %v", err))
		}
		return
	}
	resp := handle(req)
	resp.ID = req.ID
	if err := writeNativeMessage(os.Stdout, resp); err != nil {
		fmt.Fprintln(os.Stderr, "wd-host: "+err.Error())
	}
}

func handle(req request) response {
	switch req.Type {
	case typeWarmup:
		return handleWarmup()
	case typePRActions:
		return handlePRActions(req.URL)
	case typePRSummary:
		return handlePRSummary(req.URL)
	case typeExecuteAction:
		return handleExecute(req.Token)
	case typeRefreshAll:
		return handleRefresh()
	case "":
		return failure("workdash received a request with no type")
	}
	return failure("workdash does not understand the request type %q", req.Type)
}

// handleWarmup must stay cheap: the extension calls it before every other
// request and on browser startup. It reads the config, which is the one thing
// every other handler needs and the one thing that can be misconfigured, and it
// touches no git, no tmux and no network. If the pull request cache is past its
// soft limit it kicks off the same detached refresh the CLI does rather than
// making the browser wait for api.github.com.
func handleWarmup() response {
	cfg, err := wdconf.Load(wdconf.DefaultPath())
	if err != nil {
		return failure("%v", configError(err))
	}
	refreshing := false
	if prCacheIsStale(cfg) {
		snapshot.SpawnBackgroundRefresh()
		refreshing = true
	}
	return response{OK: true, Status: "ready", Refreshing: refreshing}
}

// prCacheIsStale reuses the CLI's freshness policy rather than inventing a second
// one: anything but "cached and inside the soft limit" is worth refreshing behind
// the user, including a cache that is missing entirely — warmup must never block.
func prCacheIsStale(cfg wdconf.Config) bool {
	cache := snapshot.CacheFor(cfg)
	for _, account := range snapshot.AccountsToFetch(cfg) {
		_, age, cached := cache.Load(account)
		decision := snapshot.Policy(cached, age, cfg.PRCacheTTL, cfg.PRRefreshAfter)
		if !decision.UseCache || decision.RefreshInBackground {
			return true
		}
	}
	return false
}

func handlePRActions(rawURL string) response {
	canonical, err := canonicalPRURL(rawURL)
	if err != nil {
		return failure("%v", err)
	}
	cfg, snap, warnings, err := load()
	if err != nil {
		return failure("%v", err)
	}
	groups := groupsForPR(snap, cfg.Actions, canonical)
	if len(groups) == 0 {
		// An empty success would render as a blank popup. Which of the two reasons it
		// is matters: no workspace at all, or a workspace with nothing to offer.
		if len(matchingWorkspaces(snap, canonical)) == 0 {
			return failure("no workdash workspace has this pull request's branch checked out or fetched")
		}
		return failure("workdash knows this branch but no configured action applies to it")
	}
	return response{
		OK: true, Status: "ready", LoadedAt: collectedNow(), Warnings: warnings,
		Groups: groups, ActionCount: countActions(groups),
	}
}

// handlePRSummary answers the badge, which wants a count, and a human, who wants
// to know where workdash has this branch. A pull request nobody has checked out is
// a legitimate empty answer here rather than an error: the badge simply clears.
func handlePRSummary(rawURL string) response {
	canonical, err := canonicalPRURL(rawURL)
	if err != nil {
		return failure("%v", err)
	}
	cfg, snap, warnings, err := load()
	if err != nil {
		return failure("%v", err)
	}
	return response{
		OK: true, Status: "ready", LoadedAt: collectedNow(), Warnings: warnings,
		ActionCount: countActions(groupsForPR(snap, cfg.Actions, canonical)),
		Summary:     summaryForPR(snap, canonical),
	}
}

// handleExecute re-derives everything from the token and runs the action through
// the same dispatch the CLI and the TUI use, so the browser cannot acquire a
// launch path of its own.
func handleExecute(token string) response {
	ref, err := decodeToken(token)
	if err != nil {
		return failure("%v", err)
	}
	cfg, snap, warnings, err := load()
	if err != nil {
		return failure("%v", err)
	}
	resolved, err := resolveRef(snap, cfg.Actions, ref)
	if err != nil {
		return failure("%v", err)
	}
	executor, err := dispatch.ExecutorFor(cfg, dispatch.Host(resolved))
	if err != nil {
		return failure("%v", err)
	}
	out, err := dispatch.Execute(executor, resolved)
	if err != nil {
		return failure("%s failed: %v", resolved.ID, err)
	}
	if out == "" {
		out = "completed"
	}
	return response{OK: true, Status: out, Warnings: warnings}
}

// handleRefresh is the popup's refresh button: refetch pull requests now, with the
// caller waiting, and store them. It shares the CLI's `wd refresh` body so the two
// cannot cache different things.
func handleRefresh() response {
	stored, warnings := refreshPRs()
	if stored == 0 && len(warnings) > 0 {
		return failure("refreshing pull requests failed: %s", strings.Join(warnings, "; "))
	}
	return response{
		OK: true, Status: fmt.Sprintf("refreshed %d pull requests", stored),
		LoadedAt: collectedNow(), Warnings: warnings,
	}
}

func refreshPRs() (int, []string) {
	cfg, err := wdconf.Load(wdconf.DefaultPath())
	if err != nil {
		return 0, []string{configError(err).Error()}
	}
	return snapshot.RefreshPRs(cfg, snapshot.CacheFor(cfg), snapshot.FetchPRs)
}

// load is the whole I/O path: read the config, then collect every host. Warnings
// are per source, so one unreachable machine degrades its own section and the rest
// of the answer still stands. Only a snapshot where everything failed is an error,
// which is the same distinction `wd ls` makes.
func load() (wdconf.Config, workspace.Snapshot, []string, error) {
	cfg, err := wdconf.Load(wdconf.DefaultPath())
	if err != nil {
		return wdconf.Config{}, workspace.Snapshot{}, nil, configError(err)
	}
	snap, warnings := snapshot.Load(cfg)
	if len(snap.Workspaces) == 0 && len(warnings) > 0 {
		return cfg, snap, warnings, fmt.Errorf("workdash collected nothing; every source failed: %s",
			strings.Join(warnings, "; "))
	}
	return cfg, snap, warnings, nil
}

// configError names the file, because the popup is the one frontend with no
// terminal to show a path in and "no such file or directory" alone is unactionable.
func configError(err error) error {
	return fmt.Errorf("workdash cannot read its config at %s: %v", wdconf.DefaultPath(), err)
}

func readNativeMessage(r io.Reader) (request, error) {
	var size uint32
	if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
		return request{}, err
	}
	// Checked before allocating, so a bad length prefix cannot ask for a gigabyte.
	if size == 0 || size > maxNativeRequestSize {
		return request{}, fmt.Errorf("invalid native message size %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return request{}, err
	}
	var req request
	if err := json.Unmarshal(data, &req); err != nil {
		return request{}, err
	}
	return req, nil
}

func writeNativeMessage(w io.Writer, resp response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.NativeEndian, uint32(len(data))); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
