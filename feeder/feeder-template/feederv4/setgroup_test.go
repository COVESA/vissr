package main

import (
	"testing"
	"time"
)

func TestSetRequestGrouperGroupsBackToBackDataPoints(t *testing.T) {
	var g setRequestGrouper
	start := time.Now()
	// one multi-set request, the server timestamped the data points 1 ms apart
	if got := g.GroupTs("ts.630", start); got != "ts.630" {
		t.Fatalf("first data point: got %q", got)
	}
	if got := g.GroupTs("ts.631", start.Add(time.Millisecond)); got != "ts.630" {
		t.Errorf("same request: got %q, want ts.630", got)
	}
	if got := g.GroupTs("ts.631", start.Add(150*time.Millisecond)); got != "ts.630" {
		t.Errorf("within window of the previous data point: got %q, want ts.630", got)
	}
	// the next request arrives after a pause
	if got := g.GroupTs("ts.900", start.Add(500*time.Millisecond)); got != "ts.900" {
		t.Errorf("next request: got %q, want ts.900", got)
	}
}
