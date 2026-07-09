package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Khady/workdash/internal/daemon"
)

const maxNativeRequestSize = 4 * 1024 * 1024

func main() {
	request, err := readNativeMessage(os.Stdin)
	if err != nil {
		_ = writeNativeMessage(os.Stdout, daemon.Response{OK: false, Error: err.Error()})
		return
	}
	response, err := daemon.Call(request)
	if err != nil {
		response.OK = false
		if response.Error == "" {
			response.Error = "Cannot reach the Workdash daemon: " + err.Error()
		}
	}
	if err := writeNativeMessage(os.Stdout, response); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func readNativeMessage(r io.Reader) (daemon.Request, error) {
	var size uint32
	if err := binary.Read(r, binary.NativeEndian, &size); err != nil {
		return daemon.Request{}, err
	}
	if size == 0 || size > maxNativeRequestSize {
		return daemon.Request{}, fmt.Errorf("invalid native message size %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return daemon.Request{}, err
	}
	var request daemon.Request
	if err := json.Unmarshal(data, &request); err != nil {
		return daemon.Request{}, err
	}
	return request, nil
}

func writeNativeMessage(w io.Writer, response daemon.Response) error {
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.NativeEndian, uint32(len(data))); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
