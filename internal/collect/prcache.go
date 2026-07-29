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

// Get returns the cached records for an account, fetching when the entry is
// missing, stale or unreadable. A failed fetch is surfaced and not cached, so a
// transient outage does not pin an empty result for the whole TTL.
func (c PRCache) Get(account string, fetch func(string) ([]workspace.PRRecord, error)) ([]workspace.PRRecord, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	path := filepath.Join(c.Dir, cacheFileName(account))
	if entry, ok := readEntry(path); ok && now().Sub(entry.FetchedAt) < c.TTL {
		return entry.Records, nil
	}
	records, err := fetch(account)
	if err != nil {
		return nil, err
	}
	writeEntry(path, prCacheEntry{FetchedAt: now(), Records: records})
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
