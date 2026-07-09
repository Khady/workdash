package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/model"
)

const NativeHostName = "io.github.khady.workdash"

type Request struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type"`
	URL       string `json:"url,omitempty"`
	Token     string `json:"token,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
}

type Response struct {
	ID          string           `json:"id,omitempty"`
	OK          bool             `json:"ok"`
	Error       string           `json:"error,omitempty"`
	Status      string           `json:"status,omitempty"`
	LoadedAt    time.Time        `json:"loaded_at,omitempty"`
	Refreshing  bool             `json:"refreshing,omitempty"`
	Items       []model.WorkItem `json:"items,omitempty"`
	Warnings    []string         `json:"warnings,omitempty"`
	Groups      []ActionGroup    `json:"groups,omitempty"`
	ActionCount int              `json:"action_count,omitempty"`
}

type ActionGroup struct {
	ItemKey  string          `json:"item_key"`
	Kind     model.ItemKind  `json:"kind"`
	Title    string          `json:"title"`
	Subtitle string          `json:"subtitle"`
	Host     string          `json:"host"`
	Path     string          `json:"path,omitempty"`
	Actions  []BrowserAction `json:"actions"`
}

type BrowserAction struct {
	Token              string `json:"token,omitempty"`
	Label              string `json:"label"`
	Detail             string `json:"detail,omitempty"`
	Enabled            bool   `json:"enabled"`
	UnavailableReason  string `json:"unavailable_reason,omitempty"`
	ConfirmationTitle  string `json:"confirmation_title,omitempty"`
	ConfirmationDetail string `json:"confirmation_detail,omitempty"`
	ConfirmLabel       string `json:"confirm_label,omitempty"`
}

func SocketPath() string {
	if path := os.Getenv("WORKDASH_SOCKET"); path != "" {
		return path
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		return filepath.Join(runtimeDir, "workdash.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("workdash-%d.sock", os.Getuid()))
}

func Call(req Request) (Response, error) {
	conn, err := net.DialTimeout("unix", SocketPath(), 500*time.Millisecond)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, err
	}
	if !response.OK {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

func RestoreActions(items []model.WorkItem, insideTmux bool) []model.WorkItem {
	for i := range items {
		item := &items[i]
		switch item.Kind {
		case model.KindWorktree:
			item.Action = actions.CdAction{Path: item.Path}
			if item.SSHTarget != "" {
				item.Action = actions.RemoteShellAction{Path: item.Path, SSHTarget: item.SSHTarget}
			}
		case model.KindBranch:
			item.Action = actions.CheckoutAction{RepoRoot: item.RepoRoot, Branch: item.Branch, SSHTarget: item.SSHTarget}
			if item.Path != "" {
				item.Action = actions.CdAction{Path: item.Path}
				if item.SSHTarget != "" {
					item.Action = actions.RemoteShellAction{Path: item.Path, SSHTarget: item.SSHTarget}
				}
			}
		case model.KindTmux:
			item.Action = actions.TmuxAction{Session: item.Session, InsideTmux: insideTmux, SSHTarget: item.SSHTarget}
		case model.KindPR:
			item.Action = actions.OpenInBrowserAction{URL: item.PRURL}
		}
	}
	return items
}
