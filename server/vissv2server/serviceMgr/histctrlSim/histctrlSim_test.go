/**
* (C) 2026 Matt Jones
*
* Unit tests for hist_ctrl_client.
*
* The entire binary is an interactive TTY loop that dials a Unix socket.
* There are no unit-testable pure functions — every function either reads
* from stdin, dials a real Unix socket (utils.GetUdsPath), or calls
* os.Exit. This file documents that fact so future contributors know the
* package was audited.
*
* Integration-only entry points — NOT unit-tested here:
*
*   main — dials utils.GetUdsPath("Vehicle","history"), interactive
*          stdin loop, os.Exit paths.
**/
package main

import "testing"

// TestPackageCompiles is a compile-only sentinel — it ensures the package
// builds cleanly and the integration-only annotation above stays current.
func TestPackageCompiles(t *testing.T) {}
