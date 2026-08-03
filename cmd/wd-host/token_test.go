package main

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

func sampleRef() actionRef {
	return actionRef{
		ID:     workspace.ID{Host: "asia", Repo: "/home/me/code/monorepo", Branch: "louis/topic"},
		Action: "shell",
		PRURL:  "https://github.com/khady/workdash/pull/12",
	}
}

func TestTokenRoundTrip(t *testing.T) {
	got, err := decodeToken(encodeToken(sampleRef()))
	if err != nil {
		t.Fatalf("decodeToken() error = %v", err)
	}
	if got != sampleRef() {
		t.Fatalf("decoded = %+v, want %+v", got, sampleRef())
	}
}

// Branches carry slashes and repo roots carry them too, so the encoding has to
// survive both without a separator collision.
func TestTokenRoundTripsAwkwardNames(t *testing.T) {
	ref := actionRef{
		ID:     workspace.ID{Host: "local", Repo: "/home/me/a b/c:d", Branch: "feature/a-b.c"},
		Action: "gwt-delete",
		PRURL:  "https://github.com/o/r/pull/1",
	}
	got, err := decodeToken(encodeToken(ref))
	if err != nil {
		t.Fatalf("decodeToken() error = %v", err)
	}
	if got != ref {
		t.Fatalf("decoded = %+v, want %+v", got, ref)
	}
}

// A token is URL-safe text with no padding so it survives JSON and any logging
// on the way through the extension.
func TestTokenIsOpaqueURLSafeText(t *testing.T) {
	token := encodeToken(sampleRef())
	if !strings.HasPrefix(token, tokenVersion+".") {
		t.Fatalf("token = %q, want a version prefix", token)
	}
	if strings.ContainsAny(token, "=+/ \t\n\"") {
		t.Fatalf("token = %q, want URL-safe unpadded text", token)
	}
}

func TestDecodeTokenRejectsGarbage(t *testing.T) {
	for _, token := range []string{
		"",
		"not-a-token",
		"wd1.",
		"wd1.!!!not-base64!!!",
		"wd2." + strings.TrimPrefix(encodeToken(sampleRef()), tokenVersion+"."),
		strings.TrimPrefix(encodeToken(sampleRef()), tokenVersion+"."),
	} {
		if ref, err := decodeToken(token); err == nil {
			t.Fatalf("decodeToken(%q) = %+v, want a rejection", token, ref)
		}
	}
}

func TestDecodeTokenRejectsTheWrongNumberOfFields(t *testing.T) {
	if _, err := decodeToken(encodeFields("local", "/repo")); err == nil {
		t.Fatal("decodeToken() error = nil, want a rejection for a truncated token")
	}
	if _, err := decodeToken(encodeFields("local", "/repo", "b", "a", "url", "extra")); err == nil {
		t.Fatal("decodeToken() error = nil, want a rejection for an over-long token")
	}
}

func TestDecodeTokenRejectsEmptyRequiredFields(t *testing.T) {
	if _, err := decodeToken(encodeFields("local", "/repo", "", "shell", "https://github.com/o/r/pull/1")); err == nil {
		t.Fatal("decodeToken() error = nil, want a rejection for an empty branch")
	}
	if _, err := decodeToken(encodeFields("local", "/repo", "b", "", "https://github.com/o/r/pull/1")); err == nil {
		t.Fatal("decodeToken() error = nil, want a rejection for an empty action id")
	}
}
