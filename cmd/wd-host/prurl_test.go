package main

import "testing"

// The extension canonicalizes the same way in background.js, but the host must
// never trust it: this is the only gate between a page URL and a workspace
// lookup, so it is table-driven and the rejections matter as much as the
// acceptances.
func TestCanonicalPRURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "https://github.com/Khady/workdash/pull/12", "https://github.com/khady/workdash/pull/12"},
		{"owner and repo are lowercased", "https://github.com/OCaml/Dune/pull/7", "https://github.com/ocaml/dune/pull/7"},
		{"a tab suffix is dropped", "https://github.com/khady/workdash/pull/12/files", "https://github.com/khady/workdash/pull/12"},
		{"a fragment and query are dropped", "https://github.com/khady/workdash/pull/12?w=1#issuecomment-1", "https://github.com/khady/workdash/pull/12"},
		{"surrounding whitespace is trimmed", "  https://github.com/khady/workdash/pull/12\n", "https://github.com/khady/workdash/pull/12"},
		{"a trailing slash is fine", "https://github.com/khady/workdash/pull/12/", "https://github.com/khady/workdash/pull/12"},
		{"host case is irrelevant", "https://GitHub.com/khady/workdash/pull/12", "https://github.com/khady/workdash/pull/12"},

		{"empty", "", ""},
		{"not a url", "not a url at all", ""},
		{"http is not https", "http://github.com/khady/workdash/pull/12", ""},
		{"another host", "https://gitlab.com/khady/workdash/pull/12", ""},
		{"a host that merely ends in github.com", "https://evil-github.com/khady/workdash/pull/12", ""},
		{"a subdomain is not github.com", "https://gist.github.com/khady/workdash/pull/12", ""},
		{"user info is refused", "https://user@github.com/khady/workdash/pull/12", ""},
		{"a port is refused", "https://github.com:8443/khady/workdash/pull/12", ""},
		{"an issue is not a pull request", "https://github.com/khady/workdash/issues/12", ""},
		{"a repo page is not a pull request", "https://github.com/khady/workdash", ""},
		{"a pull list is not a pull request", "https://github.com/khady/workdash/pull", ""},
		{"a non-numeric number", "https://github.com/khady/workdash/pull/abc", ""},
		{"zero is not a pull request number", "https://github.com/khady/workdash/pull/0", ""},
		{"a negative number", "https://github.com/khady/workdash/pull/-3", ""},
		{"an empty owner", "https://github.com//workdash/pull/12", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := canonicalPRURL(test.raw)
			if test.want == "" {
				if err == nil {
					t.Fatalf("canonicalPRURL(%q) = %q, want a rejection", test.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("canonicalPRURL(%q) error = %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("canonicalPRURL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

// The rejection is shown verbatim in the popup, so it has to read as a sentence
// about the page rather than as a parser complaint.
func TestCanonicalPRURLErrorIsReadable(t *testing.T) {
	_, err := canonicalPRURL("https://github.com/khady/workdash")
	if err == nil {
		t.Fatal("canonicalPRURL() error = nil")
	}
	if got := err.Error(); got != "this page is not a github.com pull request" {
		t.Fatalf("error = %q, want a sentence the popup can show", got)
	}
}
