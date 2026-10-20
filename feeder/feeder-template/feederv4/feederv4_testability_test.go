/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests for the parts of feederv4 that used to be hard to test:
*   - the random simulation (simulateInput, selectRandomInput,
*     getRandomVssfMapIndex), now driven by a scripted random source
*   - udsWriter, driven over net.Pipe
**/
package main

import (
	"bufio"
	"math/rand"
	"net"
	"testing"
	"time"
)

// scriptedRand returns the given numbers in turn, each reduced modulo n like rand.Intn
// would bound it, and records the bounds it was asked for.
type scriptedRand struct {
	values []int
	bounds []int
}

func (s *scriptedRand) Intn(n int) int {
	s.bounds = append(s.bounds, n)
	if len(s.values) == 0 {
		return 0
	}
	v := s.values[0]
	s.values = s.values[1:]
	return v % n
}

// fmap entries: names without a dot are vehicle side leaves, the only ones that are picked.
func testFMap() []FeederMap {
	return []FeederMap{
		{Name: "Vehicle.Speed", Datatype: 9}, // VSS side name (has a dot), never picked
		{Name: "CanSpeed", Datatype: 9},      // double
		{Name: "CanDoor", Datatype: 10},      // bool
		{Name: "CanGear", Datatype: 0},       // uint8
		{Name: "CanOther", Datatype: 5},      // anything else
	}
}

func TestGetRandomVssfMapIndex_EmptyMap(t *testing.T) {
	if got := getRandomVssfMapIndex(nil, &scriptedRand{}); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
	if got := getRandomVssfMapIndex([]FeederMap{}, &scriptedRand{}); got != -1 {
		t.Errorf("got %d; want -1", got)
	}
}

func TestGetRandomVssfMapIndex_SkipsNamesWithDot(t *testing.T) {
	fm := testFMap()
	// Starting at the dotted entry (index 0) the search must move on to index 1.
	if got := getRandomVssfMapIndex(fm, &scriptedRand{values: []int{0}}); got != 1 {
		t.Errorf("start 0: got %d; want 1", got)
	}
	// Starting at a leaf entry that entry is used.
	if got := getRandomVssfMapIndex(fm, &scriptedRand{values: []int{3}}); got != 3 {
		t.Errorf("start 3: got %d; want 3", got)
	}
}

// The search wraps around the end of the map. Before the last entry was included in the
// range this skipped it, and with one entry it divided by zero.
func TestGetRandomVssfMapIndex_WrapsAroundAndCoversLastEntry(t *testing.T) {
	fm := []FeederMap{{Name: "A.b"}, {Name: "C.d"}, {Name: "Leaf"}}
	sr := &scriptedRand{values: []int{2}}
	if got := getRandomVssfMapIndex(fm, sr); got != 2 {
		t.Errorf("last entry: got %d; want 2", got)
	}
	if len(sr.bounds) != 1 || sr.bounds[0] != 3 {
		t.Errorf("random bound = %v; want [3] (the whole map, including the last entry)", sr.bounds)
	}
	fm2 := []FeederMap{{Name: "Leaf"}, {Name: "A.b"}, {Name: "C.d"}}
	if got := getRandomVssfMapIndex(fm2, &scriptedRand{values: []int{1}}); got != 0 {
		t.Errorf("wrap-around: got %d; want 0", got)
	}
}

func TestGetRandomVssfMapIndex_SingleEntry(t *testing.T) {
	if got := getRandomVssfMapIndex([]FeederMap{{Name: "Leaf"}}, &scriptedRand{}); got != 0 {
		t.Errorf("got %d; want 0", got)
	}
	if got := getRandomVssfMapIndex([]FeederMap{{Name: "A.b"}}, &scriptedRand{}); got != -1 {
		t.Errorf("single dotted entry: got %d; want -1", got)
	}
}

func TestGetRandomVssfMapIndex_NoQualifyingEntryTerminates(t *testing.T) {
	fm := []FeederMap{{Name: "A.b"}, {Name: "C.d"}, {Name: "E.f"}}
	done := make(chan int, 1)
	go func() { done <- getRandomVssfMapIndex(fm, &scriptedRand{values: []int{1}}) }()
	select {
	case got := <-done:
		if got != -1 {
			t.Errorf("got %d; want -1", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("getRandomVssfMapIndex did not terminate")
	}
}

func TestSelectRandomInput_ValueRangeFollowsDatatype(t *testing.T) {
	fm := testFMap()
	cases := []struct {
		name      string
		index     int // the first scripted number selects the signal, the second the value
		wantName  string
		wantBound int
	}{
		{"double", 1, "CanSpeed", 90},
		{"bool", 2, "CanDoor", 2},
		{"uint8", 3, "CanGear", 10},
		{"other", 4, "CanOther", 1000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sr := &scriptedRand{values: []int{c.index, 7}}
			got := selectRandomInput(fm, sr)
			if got.Name != c.wantName {
				t.Errorf("name = %q; want %q", got.Name, c.wantName)
			}
			if len(sr.bounds) != 2 || sr.bounds[1] != c.wantBound {
				t.Errorf("random bounds = %v; want the value drawn below %d", sr.bounds, c.wantBound)
			}
			want := map[int]string{90: "7", 2: "1", 10: "7", 1000: "7"}[c.wantBound]
			if got.Value != want {
				t.Errorf("value = %q; want %q", got.Value, want)
			}
		})
	}
}

func TestSelectRandomInput_NoSuitableSignalGivesEmptyData(t *testing.T) {
	if got := selectRandomInput(nil, &scriptedRand{}); got != (DomainData{}) {
		t.Errorf("empty map: got %+v; want empty", got)
	}
	if got := selectRandomInput([]FeederMap{{Name: "A.b"}}, &scriptedRand{}); got != (DomainData{}) {
		t.Errorf("only dotted names: got %+v; want empty", got)
	}
}

// With a seeded *rand.Rand the whole sequence is reproducible.
func TestSelectRandomInput_SeededRandIsDeterministic(t *testing.T) {
	fm := testFMap()
	run := func() []DomainData {
		r := rand.New(rand.NewSource(42))
		var out []DomainData
		for i := 0; i < 20; i++ {
			out = append(out, selectRandomInput(fm, r))
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("run differs at %d: %+v vs %+v", i, a[i], b[i])
		}
		if a[i].Name == "" || a[i].Name == "Vehicle.Speed" {
			t.Errorf("entry %d picked %q", i, a[i].Name)
		}
	}
}

// nil means the package level math/rand: it must work and stay inside the ranges.
func TestSelectRandomInput_NilUsesGlobalRand(t *testing.T) {
	fm := testFMap()
	for i := 0; i < 200; i++ {
		got := selectRandomInput(fm, nil)
		if got.Name == "" || got.Name == "Vehicle.Speed" || got.Value == "" {
			t.Fatalf("iteration %d: got %+v", i, got)
		}
	}
}

func TestSimulateInput_RandomModeUsesTheContextsRand(t *testing.T) {
	sr := &scriptedRand{values: []int{2, 1}}
	ctx := &simulateDataCtx{RandomSim: true, Fmap: testFMap(), Rand: sr}
	got := simulateInput(ctx)
	if got.Name != "CanDoor" || got.Value != "1" {
		t.Errorf("got %+v; want CanDoor=1", got)
	}
}

// Stepwise mode counts up from setVal-10 and switches to random after 10 steps.
func TestSimulateInput_StepwiseThenRandom(t *testing.T) {
	sr := &scriptedRand{values: []int{1, 5}}
	ctx := &simulateDataCtx{Fmap: testFMap(), Path: "CanSpeed", SetVal: "50", Rand: sr}
	for i := 0; i < 10; i++ {
		got := simulateInput(ctx)
		want := []string{"40", "41", "42", "43", "44", "45", "46", "47", "48", "49"}[i]
		if got.Name != "CanSpeed" || got.Value != want {
			t.Fatalf("step %d: got %+v; want CanSpeed=%s", i, got, want)
		}
		if ctx.RandomSim {
			t.Fatalf("switched to random mode too early, after step %d", i)
		}
	}
	if len(sr.bounds) != 0 {
		t.Fatalf("random source used during stepwise mode: %v", sr.bounds)
	}
	// The 11th call: the iteration counter is 10, so the mode flips, and this call is stepwise.
	got := simulateInput(ctx)
	if got.Name != "CanSpeed" || got.Value != "50" || !ctx.RandomSim {
		t.Fatalf("11th call: got %+v randomSim=%v; want CanSpeed=50 and randomSim=true", got, ctx.RandomSim)
	}
	got = simulateInput(ctx)
	if got.Name != "CanSpeed" || got.Value != "5" {
		t.Errorf("12th call (random): got %+v; want CanSpeed=5", got)
	}
}

func TestSimulateInput_NonNumericSetValueIsPassedThrough(t *testing.T) {
	ctx := &simulateDataCtx{Path: "CanMode", SetVal: "eco"}
	for i := 0; i < 3; i++ {
		if got := simulateInput(ctx); got.Name != "CanMode" || got.Value != "eco" {
			t.Fatalf("step %d: got %+v; want CanMode=eco", i, got)
		}
	}
}

// ---------------------------------------------------------------------------
// udsWriter
// ---------------------------------------------------------------------------

func TestUdsWriter_WritesEachMessageNewlineTerminated(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	udsChan := make(chan string)
	done := make(chan struct{})
	go func() { udsWriter(server, udsChan); close(done) }()

	reader := bufio.NewReader(client)
	msgs := []string{`{"action": "subscribe", "status": "ok"}`, `{"action": "subscription", "path":"Vehicle.Speed"}`}
	for _, m := range msgs {
		udsChan <- m
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read failed: %v", err)
		}
		if line != m+"\n" {
			t.Errorf("got %q; want %q", line, m+"\n")
		}
	}
	close(udsChan)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("udsWriter did not return after udsChan was closed")
	}
}

// Closing the channel ends the writer and closes the connection; it must not write anything
// after the last message (a closed channel used to yield "" forever, i.e. endless newlines).
func TestUdsWriter_ClosedChannelStopsWriterAndClosesConn(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	udsChan := make(chan string)
	done := make(chan struct{})
	go func() { udsWriter(server, udsChan); close(done) }()

	close(udsChan)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("udsWriter did not return after udsChan was closed")
	}
	client.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 16)
	n, err := client.Read(buf)
	if n != 0 {
		t.Errorf("read %q after close; want no data", buf[:n])
	}
	if err == nil {
		t.Error("read after close returned no error; want EOF (connection closed)")
	}
}

// A write that fails must not stop the writer: the senders block on the unbuffered channel
// until their message is taken, so a stopped writer would wedge them.
func TestUdsWriter_WriteErrorDoesNotStopTheWriter(t *testing.T) {
	server, client := net.Pipe()
	client.Close() // every write on server now fails
	udsChan := make(chan string)
	done := make(chan struct{})
	go func() { udsWriter(server, udsChan); close(done) }()

	for i := 0; i < 3; i++ {
		select {
		case udsChan <- "msg":
		case <-time.After(10 * time.Second):
			t.Fatalf("send %d blocked: the writer stopped after a failed write", i)
		}
	}
	close(udsChan)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("udsWriter did not return after udsChan was closed")
	}
}
