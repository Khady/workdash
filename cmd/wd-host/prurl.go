package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// canonicalPRURL reduces a browser URL to the one form a pull request is
// compared by: "https://github.com/owner/repo/pull/N", lowercased.
//
// It is a port of the legacy daemon's CanonicalPRURL rather than a call into it,
// because the daemon is on its way out and this is the one gate between whatever
// the extension sends and a workspace lookup. Everything it refuses is refused on
// purpose:
//
//   - anything but https, so a page served over http cannot name a workspace;
//   - any host other than github.com, matched on the parsed hostname rather than
//     a suffix, so `evil-github.com` and `gist.github.com` are both out;
//   - user info and an explicit port, which are two more ways to write a URL that
//     looks like github.com and is not;
//   - anything that is not /owner/repo/pull/<positive number>, which drops issue
//     pages, repository pages and the pull request list.
//
// Query, fragment and the trailing tab segment (/files, /commits) are dropped, so
// every view of one pull request canonicalizes to the same string.
func canonicalPRURL(raw string) (string, error) {
	owner, name, number, err := parsePRURL(raw)
	if err != nil {
		return "", err
	}
	return "https://github.com/" + owner + "/" + name + "/pull/" + strconv.Itoa(number), nil
}

// parsePRURL is canonicalPRURL's three parts, for the caller that has to ask
// GitHub about the pull request rather than only compare it. Owner and name come
// back lowercased, as the canonical form has them; GitHub resolves a repository
// case-insensitively, verified against the real API.
func parsePRURL(raw string) (owner, name string, number int, err error) {
	notAPR := fmt.Errorf("this page is not a github.com pull request")
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") ||
		parsed.User != nil || parsed.Port() != "" {
		return "", "", 0, notAPR
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 4 || parts[0] == "" || parts[1] == "" || parts[2] != "pull" || parts[3] == "" {
		return "", "", 0, notAPR
	}
	number, err = strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return "", "", 0, notAPR
	}
	return strings.ToLower(parts[0]), strings.ToLower(parts[1]), number, nil
}
