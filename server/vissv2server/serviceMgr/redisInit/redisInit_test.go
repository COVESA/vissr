/**
* (C) 2026 Matt Jones
*
* Unit tests for redisInit.
*
* The binary connects to a Redis Unix socket and optionally starts
* redis-server. There are no unit-testable pure functions — all logic
* is in main() which calls redis.NewClient, Ping, and exec.Command.
*
* Integration-only entry points — NOT unit-tested here:
*
*   main — requires Redis socket at utils.GetUdsPath("Vehicle","redis")
*          and redis-server binary.
**/
package main

import "testing"

func TestPackageCompiles(t *testing.T) {}
