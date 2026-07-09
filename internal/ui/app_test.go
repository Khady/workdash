package ui

import (
	"testing"
	"time"
)

func TestFormatRefreshStatus(t *testing.T) {
	loadedAt := time.Date(2026, 7, 9, 15, 4, 0, 0, time.Local)

	tests := []struct {
		name     string
		loadedAt time.Time
		loading  bool
		want     string
	}{
		{name: "empty before first load", want: ""},
		{name: "initial load", loading: true, want: "Loading"},
		{name: "loaded", loadedAt: loadedAt, want: "Refreshed 15:04"},
		{name: "reloading", loadedAt: loadedAt, loading: true, want: "Refreshing 15:04"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRefreshStatus(tt.loadedAt, tt.loading)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
