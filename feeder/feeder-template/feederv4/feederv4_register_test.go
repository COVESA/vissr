/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests for feederRegister, which registers the feeder with the server over a unix
* domain socket and returns the name of the socket file the server wants to use for
* the data channel. A fake server listens on a socket in a temp dir.
**/
package main

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startRegServer listens on a unix socket in a short temp dir (socket paths are limited
// to about 100 characters), reads one registration request, answers with reply (unless
// it is empty) and closes the connection. The request received is sent on the returned
// channel. It skips the test if a unix socket cannot be created.
func startRegServer(t *testing.T, reply string) (sockFile string, requests <-chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "reg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sockFile = filepath.Join(dir, "r.sock")
	ln, err := net.Listen("unix", sockFile)
	if err != nil {
		t.Skipf("cannot listen on a unix socket: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		got <- string(buf[:n])
		if reply != "" {
			conn.Write([]byte(reply))
		}
	}()
	return sockFile, got
}

func TestFeederRegister_SendsNameAndReturnsTheSockfile(t *testing.T) {
	sock, requests := startRegServer(t, `{"action": "reg", "sockfile": "/var/tmp/vissv2/server-feeder-1.sock"}`)
	got := feederRegister(sock, ConfigData{Name: "feeder1"})
	if got != "/var/tmp/vissv2/server-feeder-1.sock" {
		t.Errorf("sockfile = %q; want /var/tmp/vissv2/server-feeder-1.sock", got)
	}
	select {
	case req := <-requests:
		if req != `{"action": "reg", "name": "feeder1"}` {
			t.Errorf("request = %q; want the reg request for feeder1", req)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the server did not receive a request")
	}
}

func TestFeederRegister_ServerErrorGivesEmptyResult(t *testing.T) {
	sock, _ := startRegServer(t, `{"action": "error"}`)
	if got := feederRegister(sock, ConfigData{Name: "f"}); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

func TestFeederRegister_MalformedResponsesGiveEmptyResult(t *testing.T) {
	cases := map[string]string{
		"not json":            `this is not json`,
		"no action":           `{"sockfile": "/x.sock"}`,
		"non-string action":   `{"action": 7, "sockfile": "/x.sock"}`,
		"no sockfile":         `{"action": "reg"}`,
		"non-string sockfile": `{"action": "reg", "sockfile": 5}`,
		"null sockfile":       `{"action": "reg", "sockfile": null}`,
		"empty object":        `{}`,
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			sock, _ := startRegServer(t, reply)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			if got := feederRegister(sock, ConfigData{Name: "f"}); got != "" {
				t.Errorf("got %q; want empty", got)
			}
		})
	}
}

// The server closes the connection without answering.
func TestFeederRegister_NoResponseGivesEmptyResult(t *testing.T) {
	sock, _ := startRegServer(t, "")
	if got := feederRegister(sock, ConfigData{Name: "f"}); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

func TestFeederRegister_NoServerGivesEmptyResult(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nothing-listens-here.sock")
	if got := feederRegister(missing, ConfigData{Name: "f"}); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}
