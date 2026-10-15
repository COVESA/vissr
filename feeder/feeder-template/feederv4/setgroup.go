/**
* (C) 2026 MongoDB, Inc.
*
* All files and artifacts are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import "time"

/*
* setRequestGrouper assigns one ts to all data points of a set request.
* The server delivers a multi-set request to the feeder as one message per data point, and it timestamps
* each data point when writing it, so the data points of one request can differ by a millisecond.
* The messages of one request arrive back-to-back, so data points arriving within SET_GROUP_WINDOW of the
* previous one are treated as the same request and get the ts of its first data point.
**/

const SET_GROUP_WINDOW = 200 * time.Millisecond

type setRequestGrouper struct {
	lastArrival time.Time
	groupTs     string
}

var setGrouper setRequestGrouper

func (g *setRequestGrouper) GroupTs(ts string, arrival time.Time) string {
	if g.groupTs == "" || arrival.Sub(g.lastArrival) > SET_GROUP_WINDOW {
		g.groupTs = ts
	}
	g.lastArrival = arrival
	return g.groupTs
}
