/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
**/
package main

import (
	"os"
	"testing"
)

// readTransportSecConfig reads transport_sec/transportSec.json (relative to the
// overridable trSecConfigPath). prepareTransportSecConfig is integration-only:
// it loads real X.509 cert/key files and os.Exit(1)s on failure.
func TestReadTransportSecConfig_Coverage(t *testing.T) {
	dir := t.TempDir() + "/"
	old := trSecConfigPath
	defer func() { trSecConfigPath = old }()
	trSecConfigPath = dir

	// Missing file -> defaults to "no".
	secConfig = SecConfig{}
	readTransportSecConfig()
	if secConfig.TransportSec != "no" {
		t.Errorf("missing file: TransportSec=%q; want no", secConfig.TransportSec)
	}

	// Valid config.
	os.WriteFile(dir+"transportSec.json", []byte(`{"transportSec":"yes"}`), 0644)
	secConfig = SecConfig{}
	readTransportSecConfig()
	if secConfig.TransportSec != "yes" {
		t.Errorf("valid file: TransportSec=%q; want yes", secConfig.TransportSec)
	}

	// Malformed JSON -> defaults to "no".
	os.WriteFile(dir+"transportSec.json", []byte(`not json`), 0644)
	secConfig = SecConfig{}
	readTransportSecConfig()
	if secConfig.TransportSec != "no" {
		t.Errorf("bad json: TransportSec=%q; want no", secConfig.TransportSec)
	}
}
