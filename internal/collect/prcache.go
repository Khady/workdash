package collect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Khady/workdash/internal/workspace"
)

// PRCache is a short-lived on-disk cache for pull request fetches.
//
// It exists because GitHub is the only network source in the system and the CLI
// is a fresh process each time: without it every `wd ls` pays a few seconds of
// api.github.com latency, while the git and tmux facts take milliseconds.
//
// It is cache, not state. Every entry is reconstructible from GitHub, a corrupt
// or unreadable file counts as a miss, and deleting the directory is always safe
// — which is what keeps "workdash owns no authoritative state" true.
type PRCache struct {
	Dir string
	TTL time.Duration
	Now func() time.Time
}

type prCacheEntry struct {
	FetchedAt time.Time            `json:"fetched_at"`
	Records   []workspace.PRRecord `json:"records"`
}

func DefaultPRCacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(base, "workdash")
}

// Load returns a cached entry and its age, whatever that age is. Staleness is
// deliberately the caller's decision: an interactive frontend wants to render a
// day-old list instantly and refresh behind the user's back, which it cannot do
// if the cache hides old entries.
func (c PRCache) Load(account string) ([]workspace.PRRecord, time.Duration, bool) {
	entry, ok := readEntry(filepath.Join(c.Dir, cacheFileName(account)))
	if !ok {
		return nil, 0, false
	}
	return entry.Records, c.now().Sub(entry.FetchedAt), true
}

func (c PRCache) Store(account string, records []workspace.PRRecord) {
	writeEntry(filepath.Join(c.Dir, cacheFileName(account)), prCacheEntry{FetchedAt: c.now(), Records: records})
}

func (c PRCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Get returns the cached records for an account, fetching when the entry is
// missing, stale or unreadable. A failed fetch is surfaced and not cached, so a
// transient outage does not pin an empty result for the whole TTL.
func (c PRCache) Get(account string, fetch func(string) ([]workspace.PRRecord, error)) ([]workspace.PRRecord, error) {
	if records, age, ok := c.Load(account); ok && age < c.TTL {
		return records, nil
	}
	records, err := fetch(account)
	if err != nil {
		return nil, err
	}
	c.Store(account, records)
	return records, nil
}

// cacheFileName hashes the account so an account name with a slash or a space
// cannot escape the cache directory or collide with another.
func cacheFileName(account string) string {
	sum := sha256.Sum256([]byte(account))
	return "prs-" + hex.EncodeToString(sum[:8]) + ".json"
}

func readEntry(path string) (prCacheEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return prCacheEntry{}, false
	}
	var entry prCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return prCacheEntry{}, false
	}
	return entry, true
}

// writeEntry is best effort: an unwritable cache directory should slow workdash
// down, not break it.
func writeEntry(path string, entry prCacheEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = writeFile(path, string(data))
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
