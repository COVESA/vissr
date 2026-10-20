package main

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestMongoTypedValue(t *testing.T) {
	cases := []struct {
		in   string
		want interface{}
	}{
		{"85.5", 85.5},
		{"1", 1.0},
		{"-12.6036", -12.6036},
		{"true", true},
		{"false", false},
		{"TRUE", "TRUE"},
		{"viss-inline:Data-not-available", "viss-inline:Data-not-available"},
		{"", ""},
	}
	for _, c := range cases {
		if got := mongoTypedValue(c.in); got != c.want {
			t.Errorf("mongoTypedValue(%q) = %v (%T), want %v (%T)", c.in, got, got, c.want, c.want)
		}
	}
}

func TestMongoTypedValueArray(t *testing.T) {
	cases := []struct {
		in   string
		want interface{}
	}{
		{`["118", "95", "75"]`, []interface{}{118.0, 95.0, 75.0}},
		{`["-1.5", "true", "abc"]`, []interface{}{-1.5, true, "abc"}},
		{`[1, 2]`, []interface{}{1.0, 2.0}},
		{`[]`, []interface{}{}},
		{`[not json`, `[not json`},
	}
	for _, c := range cases {
		if got := mongoTypedValue(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("mongoTypedValue(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

func TestMongoUpdateDoc(t *testing.T) {
	doc := mongoUpdateDoc(setItem{Path: "CVED.Context.Occupant.Weight", Value: "85.5", Ts: "2026-10-05T23:02:44.611Z"})
	set, ok := doc["$set"].(bson.M)
	if !ok {
		t.Fatalf("missing $set: %v", doc)
	}
	if set["CVED.Context.Occupant.Weight"] != 85.5 {
		t.Errorf("$set path value = %v, want 85.5", set["CVED.Context.Occupant.Weight"])
	}
	if _, ok := set["updatedAt"]; !ok {
		t.Errorf("$set missing updatedAt")
	}
	onInsert, ok := doc["$setOnInsert"].(bson.M)
	if !ok || onInsert["ts"] != "2026-10-05T23:02:44.611Z" {
		t.Errorf("$setOnInsert = %v, want ts", doc["$setOnInsert"])
	}
}

// ---------------------------------------------------------------------------
// the sink itself, against a fake collection
// ---------------------------------------------------------------------------

type updateCall struct {
	filter bson.M
	update bson.M
	upsert bool
}

// fakeCollection records the UpdateOne calls. fail makes the call with that (1 based) number
// return an error; block, if set, is waited on before each call returns.
type fakeCollection struct {
	mu      sync.Mutex
	calls   []updateCall
	fail    map[int]bool
	block   chan struct{}
	entered chan struct{} // receives one value when a call starts, if not nil
	done    chan struct{} // receives one value per finished call, if not nil
}

func (f *fakeCollection) UpdateOne(ctx context.Context, filter any, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	var o options.UpdateOneOptions
	for _, l := range opts {
		for _, set := range l.List() {
			if err := set(&o); err != nil {
				return nil, err
			}
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, updateCall{filter: filter.(bson.M), update: update.(bson.M), upsert: o.Upsert != nil && *o.Upsert})
	n := len(f.calls)
	failed := f.fail[n]
	f.mu.Unlock()
	if f.done != nil {
		defer func() { f.done <- struct{}{} }()
	}
	if failed {
		return nil, errors.New("simulated write failure")
	}
	return &mongo.UpdateResult{}, nil
}

func (f *fakeCollection) snapshot() []updateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]updateCall(nil), f.calls...)
}

func TestMongoSinkStore_UpsertsByTimestamp(t *testing.T) {
	fc := &fakeCollection{}
	sink := newMongoSinkFor(fc, 4)
	sink.store(setItem{Path: "CVED.Context.Occupant.Weight", Value: "85.5", Ts: "2026-10-05T23:02:44.611Z"})
	calls := fc.snapshot()
	if len(calls) != 1 {
		t.Fatalf("calls = %d; want 1", len(calls))
	}
	if calls[0].filter["_id"] != "2026-10-05T23:02:44.611Z" {
		t.Errorf("filter = %v; want _id = the ts", calls[0].filter)
	}
	if !calls[0].upsert {
		t.Error("the write is not an upsert; the first data point of a request would be lost")
	}
	if set, _ := calls[0].update["$set"].(bson.M); set["CVED.Context.Occupant.Weight"] != 85.5 {
		t.Errorf("update = %v; want the typed value under the path", calls[0].update)
	}
}

// Data points of one set request share a ts, so they all address one document.
func TestMongoSinkStore_DataPointsOfOneRequestAddressOneDocument(t *testing.T) {
	fc := &fakeCollection{}
	sink := newMongoSinkFor(fc, 4)
	sink.store(setItem{Path: "A.x", Value: "1", Ts: "T1"})
	sink.store(setItem{Path: "B.y", Value: "true", Ts: "T1"})
	sink.store(setItem{Path: "A.x", Value: "2", Ts: "T2"})
	calls := fc.snapshot()
	if len(calls) != 3 {
		t.Fatalf("calls = %d; want 3", len(calls))
	}
	if calls[0].filter["_id"] != calls[1].filter["_id"] || calls[0].filter["_id"] == calls[2].filter["_id"] {
		t.Errorf("filters = %v %v %v; the first two must match and the third differ", calls[0].filter, calls[1].filter, calls[2].filter)
	}
}

func TestMongoSinkWriter_StoresInOrderAndStopsWhenTheQueueIsClosed(t *testing.T) {
	fc := &fakeCollection{}
	sink := newMongoSinkFor(fc, 8)
	for i := 0; i < 5; i++ {
		sink.Write("P.v", strconv.Itoa(i), "T")
	}
	close(sink.queue)
	done := make(chan struct{})
	go func() { sink.writer(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("writer did not return after the queue was closed")
	}
	calls := fc.snapshot()
	if len(calls) != 5 {
		t.Fatalf("calls = %d; want 5", len(calls))
	}
	for i, c := range calls {
		if got := c.update["$set"].(bson.M)["P.v"]; got != float64(i) {
			t.Errorf("call %d stored %v; want %d (order not kept)", i, got, i)
		}
	}
}

// A failed write only drops that item; the writer goes on with the next ones.
func TestMongoSinkWriter_FailedWriteDoesNotStopTheWriter(t *testing.T) {
	fc := &fakeCollection{fail: map[int]bool{2: true}}
	sink := newMongoSinkFor(fc, 8)
	for i := 1; i <= 4; i++ {
		sink.Write("P.v", strconv.Itoa(i), "T")
	}
	close(sink.queue)
	done := make(chan struct{})
	go func() { sink.writer(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("writer did not return")
	}
	if n := len(fc.snapshot()); n != 4 {
		t.Errorf("calls = %d; want 4 (the writer stopped after the failed write)", n)
	}
}

// Write must never block the feeder: with a full queue (a slow or unreachable database) the
// item is dropped. The first item is taken by the writer and is stuck in the database call;
// queueSize more fit in the queue; the rest are dropped.
//
// The test waits until the writer is inside the database call before it sends more. Without
// that the outcome depends on when the writer goroutine first gets to run: if the flood comes
// first, the queue fills up, the writer then takes one of the queued items, and only 2 items
// are kept, not 3 (this failed on a loaded CI machine under the race detector).
func TestMongoSinkWrite_NeverBlocksWhenTheQueueIsFull(t *testing.T) {
	fc := &fakeCollection{block: make(chan struct{}), entered: make(chan struct{}, 16), done: make(chan struct{}, 16)}
	sink := newMongoSinkFor(fc, 2)
	go sink.writer()
	defer close(sink.queue)

	sink.Write("P.v", "first", "T")
	select {
	case <-fc.entered: // the writer has taken the item and is blocked in the database call
	case <-time.After(5 * time.Second):
		t.Fatal("the writer did not start storing the first item")
	}

	finished := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			sink.Write("P.v", strconv.Itoa(i), "T")
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Write blocked on a full queue")
	}
	close(fc.block) // let the database answer
	// at most 1 (in the call) + 2 (queued) items can have been kept
	deadline := time.After(10 * time.Second)
	for got := 0; got < 3; got++ {
		select {
		case <-fc.done:
		case <-deadline:
			t.Fatalf("only %d items were stored; want 3 (1 in flight + 2 queued)", got)
		}
	}
	select {
	case <-fc.done:
		t.Error("more than 3 items were stored; the queue limit is not applied")
	case <-time.After(200 * time.Millisecond):
	}
}
