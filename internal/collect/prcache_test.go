package collect

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/workspace"
)

func TestPRCacheReturnsFreshEntryWithoutFetching(t *testing.T) {
	dir := t.TempDir()
	cache := PRCache{Dir: dir, TTL: time.Minute, Now: func() time.Time { return time.Unix(1000, 0) }}
	want := []workspace.PRRecord{{FullName: "o/r", Branch: "main", PR: workspace.PR{Number: 1}}}
	fetches := 0
	fetch := func(string) ([]workspace.PRRecord, error) {
		fetches++
		return want, nil
	}
	if _, err := cache.Get("acct", fetch); err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	got, err := cache.Get("acct", fetch)
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if fetches != 1 {
		t.Fatalf("fetches = %d, want the second Get served from cache", fetches)
	}
	if len(got) != 1 || got[0].PR.Number != 1 {
		t.Fatalf("records = %+v, want the cached record", got)
	}
}

func TestPRCacheRefetchesAfterTTL(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1000, 0)
	cache := PRCache{Dir: dir, TTL: time.Minute, Now: func() time.Time { return now }}
	fetches := 0
	fetch := func(string) ([]workspace.PRRecord, error) {
		fetches++
		return []workspace.PRRecord{{FullName: "o/r", Branch: "main"}}, nil
	}
	if _, err := cache.Get("acct", fetch); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := cache.Get("acct", fetch); err != nil {
		t.Fatal(err)
	}
	if fetches != 2 {
		t.Fatalf("fetches = %d, want a refetch once the entry went stale", fetches)
	}
}

func TestPRCacheKeepsAccountsSeparate(t *testing.T) {
	dir := t.TempDir()
	cache := PRCache{Dir: dir, TTL: time.Minute, Now: time.Now}
	for _, account := range []string{"a", "b"} {
		name := account
		if _, err := cache.Get(account, func(string) ([]workspace.PRRecord, error) {
			return []workspace.PRRecord{{FullName: "o/" + name, Branch: "main"}}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := cache.Get("a", func(string) ([]workspace.PRRecord, error) {
		t.Fatal("account a should still be cached")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FullName != "o/a" {
		t.Fatalf("records = %+v, want account a's own entry", got)
	}
}

// A cache is disposable state: unreadable or corrupt content must cost a refetch,
// never an error, or a bad file would wedge the tool until someone deleted it.
func TestPRCacheTreatsCorruptEntryAsAMiss(t *testing.T) {
	dir := t.TempDir()
	cache := PRCache{Dir: dir, TTL: time.Minute, Now: time.Now}
	if err := writeFile(filepath.Join(dir, cacheFileName("acct")), "not json at all"); err != nil {
		t.Fatal(err)
	}
	fetched := false
	got, err := cache.Get("acct", func(string) ([]workspace.PRRecord, error) {
		fetched = true
		return []workspace.PRRecord{{FullName: "o/r", Branch: "main"}}, nil
	})
	if err != nil {
		t.Fatalf("Get() error = %v, want a silent refetch", err)
	}
	if !fetched || len(got) != 1 {
		t.Fatalf("fetched = %v records = %+v, want a refetch", fetched, got)
	}
}

// A failed fetch must not poison the cache with an empty result.
func TestPRCacheDoesNotStoreFailures(t *testing.T) {
	dir := t.TempDir()
	cache := PRCache{Dir: dir, TTL: time.Minute, Now: time.Now}
	if _, err := cache.Get("acct", func(string) ([]workspace.PRRecord, error) {
		return nil, errTest
	}); err == nil {
		t.Fatal("Get() error = nil, want the fetch failure surfaced")
	}
	fetched := false
	if _, err := cache.Get("acct", func(string) ([]workspace.PRRecord, error) {
		fetched = true
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !fetched {
		t.Fatal("a failed fetch must not have been cached")
	}
}

var errTest = errors.New("fetch failed")
