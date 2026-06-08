/**
* (C) 2026 Matt Jones
*
* Unit tests for vin_feeder (redisVinFeeder).
*
* redisSet calls redis.Client.Set which requires a live Redis connection.
* main dials a Unix Redis socket and calls os.Exit.
* There are no unit-testable pure functions.
*
* Integration-only entry points — NOT unit-tested here:
*
*   redisSet — requires a live redis.Client connected to
*              utils.GetUdsPath("Vehicle","redis")
*   main     — requires Redis socket + VIN command-line argument
**/
package main

import "testing"

func TestPackageCompiles(t *testing.T) {}
