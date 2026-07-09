package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/model"
)

func TestRefreshKeepsItemsForTUIAndCompactsBrowserResponse(t *testing.T) {
	tuiResponse, err := refreshResponseForTest("refresh")
	if err != nil {
		t.Fatal(err)
	}
	if len(tuiResponse.Items) != 1 {
		t.Fatalf("TUI refresh lost snapshot items: %#v", tuiResponse)
	}

	browserResponse, err := refreshResponseForTest("refresh_browser")
	if err != nil {
		t.Fatal(err)
	}
	if len(browserResponse.Items) != 0 {
		t.Fatalf("browser refresh should omit snapshot items: %#v", browserResponse)
	}
}

func refreshResponseForTest(requestType string) (Response, error) {
	done := make(chan struct{})
	close(done)
	state := NewState("")
	state.items = []model.WorkItem{{Kind: model.KindPR, PRURL: "https://github.com/example/repo/pull/1"}}
	state.loadedAt = time.Now()
	state.refreshing = true
	state.refreshDone = done
	server := &Server{state: state}
	return server.dispatch(context.Background(), Request{Type: requestType})
}
