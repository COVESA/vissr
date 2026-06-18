/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
**/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBytes_EvicSim(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.bin")
	if err := os.WriteFile(p, []byte{0x01, 0x02, 0x03, 0x04}, 0644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got := readBytes(2, f)
	if len(got) != 2 || got[0] != 0x01 || got[1] != 0x02 {
		t.Errorf("readBytes(2) = %v; want [1 2]", got)
	}
	if readBytes(0, f) != nil {
		t.Error("readBytes(0) should return nil")
	}
}
