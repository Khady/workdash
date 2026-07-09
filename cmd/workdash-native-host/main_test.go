package main

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/Khady/workdash/internal/daemon"
)

func TestNativeMessageRoundTrip(t *testing.T) {
	var encoded bytes.Buffer
	want := daemon.Response{OK: true, Status: "ready"}
	if err := writeNativeMessage(&encoded, want); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() <= 4 {
		t.Fatal("native response was not framed")
	}

	var request bytes.Buffer
	requestBody := []byte(`{"type":"snapshot","id":"one"}`)
	if err := binary.Write(&request, binary.NativeEndian, uint32(len(requestBody))); err != nil {
		t.Fatal(err)
	}
	request.Write(requestBody)

	got, err := readNativeMessage(&request)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "snapshot" || got.ID != "one" {
		t.Fatalf("unexpected request: %#v", got)
	}
}
