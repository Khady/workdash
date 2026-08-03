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
