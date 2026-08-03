package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// The framing is the browser's, not ours: a 4-byte length prefix in the
// platform's native byte order (little-endian everywhere workdash runs) followed
// by that many bytes of JSON.
func TestNativeMessageRoundTrip(t *testing.T) {
	var framed bytes.Buffer
	if err := writeNativeMessage(&framed, response{OK: true, Summary: "hello"}); err != nil {
		t.Fatal(err)
	}
	var size uint32
	if err := binary.Read(bytes.NewReader(framed.Bytes()[:4]), binary.NativeEndian, &size); err != nil {
		t.Fatal(err)
	}
	if int(size) != framed.Len()-4 {
		t.Fatalf("prefix = %d, body = %d", size, framed.Len()-4)
	}

	var request bytes.Buffer
	body := []byte(`{"type":"get_pr_actions","url":"https://github.com/o/r/pull/1"}`)
	if err := binary.Write(&request, binary.NativeEndian, uint32(len(body))); err != nil {
		t.Fatal(err)
	}
	request.Write(body)
	got, err := readNativeMessage(&request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "get_pr_actions" || got.URL != "https://github.com/o/r/pull/1" {
		t.Fatalf("request = %+v", got)
	}
}

func TestReadNativeMessageRejectsAnAbsurdSize(t *testing.T) {
	var request bytes.Buffer
	if err := binary.Write(&request, binary.NativeEndian, uint32(maxNativeRequestSize+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeMessage(&request); err == nil {
		t.Fatal("readNativeMessage() error = nil, want a rejection before allocating")
	}
}

// The popup shows response.error and nothing else, so a failure must always
// carry ok:false and a message.
func TestFailureResponseIsAlwaysReadable(t *testing.T) {
	data, err := json.Marshal(failure("this page is not a github.com pull request"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"ok":false`) {
		t.Fatalf("response = %s, want ok:false", text)
	}
	if !strings.Contains(text, `"error":"this page is not a github.com pull request"`) {
		t.Fatalf("response = %s, want the message in error", text)
	}
}

// A success must serialise ok:true even when there is nothing else to say; the
// extension treats a missing ok as a failure.
func TestSuccessResponseAlwaysCarriesOK(t *testing.T) {
	data, err := json.Marshal(response{OK: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("response = %s, want a bare ok:true", data)
	}
}

// Firefox invokes a native host with the manifest path — and in some versions the
// extension id — as arguments. Treating any argument as a subcommand made the host
// exit 2 without writing a frame, which surfaces in the popup as Firefox's own "An
// unexpected error occurred" rather than anything workdash said.
func TestModeIgnoresTheArgumentsFirefoxPasses(t *testing.T) {
	manifest := "/home/me/.mozilla/native-messaging-hosts/io.github.khady.workdash.json"
	for _, argv := range [][]string{
		{"wd-host", manifest},
		{"wd-host", manifest, "workdash@khady.github.io"},
		{"wd-host"},
	} {
		if got := mode(argv); got != modeServe {
			t.Fatalf("mode(%q) = %v, want serve: Firefox passes the manifest path", argv, got)
		}
	}
}

func TestModeStillRecognisesTheRefreshSubcommand(t *testing.T) {
	if got := mode([]string{"wd-host", "refresh"}); got != modeRefresh {
		t.Fatalf("mode(refresh) = %v, want refresh", got)
	}
}
