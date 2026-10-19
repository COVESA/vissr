/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* Tests for the state storage writers of feederv4 (statestorageSet and the per
* backend sqliteSet / redisSet / memcacheSet):
*   - sqlite: a real database in a temp dir
*   - redis, memcache: minimal fake servers that speak just enough of the wire
*     protocol (RESP, memcache text) to record what the client sends
**/
package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/go-redis/redis"
)

// ---------------------------------------------------------------------------
// sqlite
// ---------------------------------------------------------------------------

func newTestSqliteDb(t *testing.T, paths ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "statestorage.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("CREATE TABLE VSS_MAP (`path` TEXT, c_value TEXT, c_ts TEXT, d_value TEXT, d_ts TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	for _, p := range paths {
		if _, err := db.Exec("INSERT INTO VSS_MAP (`path`, c_value, c_ts, d_value, d_ts) VALUES (?, 'old', 'old-ts', 'def', 'def-ts')", p); err != nil {
			t.Fatalf("insert %q: %v", p, err)
		}
	}
	return db
}

func readRow(t *testing.T, db *sql.DB, path string) (value, ts, defValue string, found bool) {
	t.Helper()
	err := db.QueryRow("SELECT c_value, c_ts, d_value FROM VSS_MAP WHERE `path`=?", path).Scan(&value, &ts, &defValue)
	if err == sql.ErrNoRows {
		return "", "", "", false
	}
	if err != nil {
		t.Fatalf("select %q: %v", path, err)
	}
	return value, ts, defValue, true
}

func TestSqliteSet_UpdatesCurrentValueAndTimestamp(t *testing.T) {
	db := newTestSqliteDb(t, "Vehicle.Speed", "Vehicle.Other")
	if err := sqliteSet(db, "Vehicle.Speed", "42", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("sqliteSet: %v", err)
	}
	v, ts, def, _ := readRow(t, db, "Vehicle.Speed")
	if v != "42" || ts != "2026-01-01T00:00:00Z" {
		t.Errorf("row = (%q, %q); want (42, 2026-01-01T00:00:00Z)", v, ts)
	}
	if def != "def" {
		t.Errorf("default value changed to %q; only the current value may be written", def)
	}
	if v, ts, _, _ := readRow(t, db, "Vehicle.Other"); v != "old" || ts != "old-ts" {
		t.Errorf("another signal's row changed to (%q, %q)", v, ts)
	}
}

// A path without a row updates nothing; it is not an error and creates no row.
func TestSqliteSet_UnknownPathStoresNothing(t *testing.T) {
	db := newTestSqliteDb(t, "Vehicle.Speed")
	if err := sqliteSet(db, "Vehicle.NotThere", "1", "t"); err != nil {
		t.Fatalf("sqliteSet: %v", err)
	}
	if _, _, _, found := readRow(t, db, "Vehicle.NotThere"); found {
		t.Error("a row was created for an unknown path")
	}
	if v, _, _, _ := readRow(t, db, "Vehicle.Speed"); v != "old" {
		t.Errorf("existing row changed to %q", v)
	}
}

// Values and paths are bound parameters: quotes and SQL syntax are data, not code.
func TestSqliteSet_ValuesAndPathsAreNotInterpretedAsSql(t *testing.T) {
	evilPath := `Vehicle.X'; DROP TABLE VSS_MAP; --`
	db := newTestSqliteDb(t, evilPath, "Vehicle.Speed")
	evilValue := `a"b'c\d'); DROP TABLE VSS_MAP; --`
	if err := sqliteSet(db, evilPath, evilValue, "t"); err != nil {
		t.Fatalf("sqliteSet: %v", err)
	}
	v, _, _, found := readRow(t, db, evilPath)
	if !found || v != evilValue {
		t.Errorf("stored value = %q (found=%v); want %q", v, found, evilValue)
	}
	if _, _, _, found := readRow(t, db, "Vehicle.Speed"); !found {
		t.Error("table was damaged")
	}
}

func TestSqliteSet_NilHandleIsAnErrorNotAPanic(t *testing.T) {
	if err := sqliteSet(nil, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error for a nil database handle")
	}
}

func TestSqliteSet_MissingTableIsAnError(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := sqliteSet(db, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error when the VSS_MAP table does not exist")
	}
}

func TestSqliteSet_ClosedDatabaseIsAnError(t *testing.T) {
	db := newTestSqliteDb(t, "Vehicle.Speed")
	db.Close()
	if err := sqliteSet(db, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error for a closed database")
	}
}

// ---------------------------------------------------------------------------
// fake servers
// ---------------------------------------------------------------------------

// fakeServer accepts connections and runs handle on each one.
type fakeServer struct {
	ln    net.Listener
	wg    sync.WaitGroup
	mu    sync.Mutex
	conns []net.Conn
}

func startFakeServer(t *testing.T, handle func(net.Conn)) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{ln: ln}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()
			s.wg.Add(1)
			go func() { defer s.wg.Done(); defer conn.Close(); handle(conn) }()
		}
	}()
	// Clients keep pooled connections open, so the handlers only end when the server
	// closes the connections: close them before waiting.
	t.Cleanup(func() {
		ln.Close()
		s.mu.Lock()
		for _, c := range s.conns {
			c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s
}

func (s *fakeServer) addr() string { return s.ln.Addr().String() }

// closedAddr returns an address that nothing listens on.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := ln.Addr().String()
	ln.Close()
	return a
}

type recorder struct {
	mu   sync.Mutex
	data [][]string
}

func (r *recorder) add(cmd []string) { r.mu.Lock(); r.data = append(r.data, cmd); r.mu.Unlock() }
func (r *recorder) all() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.data...)
}

// readRESPCommand reads one command, an array of bulk strings, as sent by a redis client.
func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("unexpected line %q", line)
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(hdr[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func fakeRedis(t *testing.T, rec *recorder, reply string) *fakeServer {
	return startFakeServer(t, func(c net.Conn) {
		r := bufio.NewReader(c)
		for {
			cmd, err := readRESPCommand(r)
			if err != nil {
				return
			}
			rec.add(cmd)
			io.WriteString(c, reply)
		}
	})
}

func newTestRedisClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Network: "tcp", Addr: addr, MaxRetries: 0})
}

// ---------------------------------------------------------------------------
// redis
// ---------------------------------------------------------------------------

func TestRedisSet_StoresDatapointJsonUnderThePath(t *testing.T) {
	rec := &recorder{}
	srv := fakeRedis(t, rec, "+OK\r\n")
	client := newTestRedisClient(srv.addr())
	defer client.Close()

	if err := redisSet(client, "Vehicle.Speed", "42", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("redisSet: %v", err)
	}
	cmds := rec.all()
	if len(cmds) != 1 || len(cmds[0]) != 3 || !strings.EqualFold(cmds[0][0], "set") || cmds[0][1] != "Vehicle.Speed" {
		t.Fatalf("commands = %v; want one SET Vehicle.Speed <json>", cmds)
	}
	var dp map[string]string
	if err := json.Unmarshal([]byte(cmds[0][2]), &dp); err != nil {
		t.Fatalf("stored value %q is not JSON: %v", cmds[0][2], err)
	}
	if dp["value"] != "42" || dp["ts"] != "2026-01-01T00:00:00Z" || len(dp) != 2 {
		t.Errorf("stored datapoint = %v; want value=42 ts=2026-01-01T00:00:00Z", dp)
	}
}

// A value with quotes and backslashes must still be a valid JSON document that decodes
// back to the same value (the former string concatenation corrupted it).
func TestRedisSet_ValueWithQuotesIsValidJson(t *testing.T) {
	rec := &recorder{}
	srv := fakeRedis(t, rec, "+OK\r\n")
	client := newTestRedisClient(srv.addr())
	defer client.Close()

	val := "a\"b\\c\nd"
	if err := redisSet(client, "Vehicle.Name", val, "t"); err != nil {
		t.Fatalf("redisSet: %v", err)
	}
	var dp map[string]string
	if err := json.Unmarshal([]byte(rec.all()[0][2]), &dp); err != nil {
		t.Fatalf("stored value is not JSON: %v", err)
	}
	if dp["value"] != val {
		t.Errorf("round trip value = %q; want %q", dp["value"], val)
	}
}

func TestRedisSet_ServerErrorIsReturned(t *testing.T) {
	srv := fakeRedis(t, &recorder{}, "-ERR out of memory\r\n")
	client := newTestRedisClient(srv.addr())
	defer client.Close()
	err := redisSet(client, "Vehicle.Speed", "1", "t")
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Errorf("err = %v; want the server's error", err)
	}
}

func TestRedisSet_UnreachableServerIsAnError(t *testing.T) {
	client := newTestRedisClient(closedAddr(t))
	defer client.Close()
	if err := redisSet(client, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error when the server cannot be reached")
	}
}

func TestRedisSet_NilClientIsAnErrorNotAPanic(t *testing.T) {
	if err := redisSet(nil, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error for a nil client")
	}
}

// ---------------------------------------------------------------------------
// memcache
// ---------------------------------------------------------------------------

type memcacheSetCmd struct {
	key  string
	data string
}

// fakeMemcache implements the "set" command of the memcache text protocol.
func fakeMemcache(t *testing.T, got chan<- memcacheSetCmd, reply string) *fakeServer {
	return startFakeServer(t, func(c net.Conn) {
		r := bufio.NewReader(c)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			f := strings.Fields(line) // set <key> <flags> <exptime> <bytes>
			if len(f) < 5 || f[0] != "set" {
				io.WriteString(c, "ERROR\r\n")
				return
			}
			size, _ := strconv.Atoi(f[4])
			buf := make([]byte, size+2)
			if _, err := io.ReadFull(r, buf); err != nil {
				return
			}
			got <- memcacheSetCmd{key: f[1], data: string(buf[:size])}
			io.WriteString(c, reply)
		}
	})
}

func TestMemcacheSet_StoresDatapointJsonUnderThePath(t *testing.T) {
	got := make(chan memcacheSetCmd, 4)
	srv := fakeMemcache(t, got, "STORED\r\n")
	client := memcache.New(srv.addr())

	if err := memcacheSet(client, "Vehicle.Speed", "42", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("memcacheSet: %v", err)
	}
	cmd := <-got
	if cmd.key != "Vehicle.Speed" {
		t.Errorf("key = %q; want Vehicle.Speed", cmd.key)
	}
	var dp map[string]string
	if err := json.Unmarshal([]byte(cmd.data), &dp); err != nil {
		t.Fatalf("stored value %q is not JSON: %v", cmd.data, err)
	}
	if dp["value"] != "42" || dp["ts"] != "2026-01-01T00:00:00Z" || len(dp) != 2 {
		t.Errorf("stored datapoint = %v; want value=42 ts=2026-01-01T00:00:00Z", dp)
	}
}

func TestMemcacheSet_ValueWithQuotesIsValidJson(t *testing.T) {
	got := make(chan memcacheSetCmd, 4)
	srv := fakeMemcache(t, got, "STORED\r\n")
	client := memcache.New(srv.addr())
	val := "a\"b\\c d"
	if err := memcacheSet(client, "Vehicle.Name", val, "t"); err != nil {
		t.Fatalf("memcacheSet: %v", err)
	}
	var dp map[string]string
	if err := json.Unmarshal([]byte((<-got).data), &dp); err != nil || dp["value"] != val {
		t.Errorf("round trip failed: dp=%v err=%v; want value %q", dp, err, val)
	}
}

func TestMemcacheSet_ServerErrorIsReturned(t *testing.T) {
	got := make(chan memcacheSetCmd, 4)
	srv := fakeMemcache(t, got, "SERVER_ERROR out of memory\r\n")
	client := memcache.New(srv.addr())
	if err := memcacheSet(client, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error when the server reports one")
	}
}

func TestMemcacheSet_UnreachableServerIsAnError(t *testing.T) {
	client := memcache.New(closedAddr(t))
	if err := memcacheSet(client, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error when the server cannot be reached")
	}
}

func TestMemcacheSet_NilClientIsAnErrorNotAPanic(t *testing.T) {
	if err := memcacheSet(nil, "Vehicle.Speed", "1", "t"); err == nil {
		t.Error("want an error for a nil client")
	}
}

// ---------------------------------------------------------------------------
// statestorageSet: backend selection and the 0 / -1 result
// ---------------------------------------------------------------------------

// withStorage sets the package level storage selection for one test and restores it.
func withStorage(t *testing.T, dbType string, db *sql.DB, rc *redis.Client, mc *memcache.Client) {
	t.Helper()
	oldType, oldDb, oldRc, oldMc := stateDbType, dbHandle, redisClient, memcacheClient
	stateDbType, dbHandle, redisClient, memcacheClient = dbType, db, rc, mc
	t.Cleanup(func() { stateDbType, dbHandle, redisClient, memcacheClient = oldType, oldDb, oldRc, oldMc })
}

func TestStatestorageSet_SqliteSuccessIsZero(t *testing.T) {
	db := newTestSqliteDb(t, "Vehicle.Speed")
	withStorage(t, "sqlite", db, nil, nil)
	if got := statestorageSet("Vehicle.Speed", "7", "ts"); got != 0 {
		t.Fatalf("got %d; want 0", got)
	}
	if v, _, _, _ := readRow(t, db, "Vehicle.Speed"); v != "7" {
		t.Errorf("stored value = %q; want 7", v)
	}
}

func TestStatestorageSet_RedisSuccessIsZero(t *testing.T) {
	rec := &recorder{}
	srv := fakeRedis(t, rec, "+OK\r\n")
	client := newTestRedisClient(srv.addr())
	defer client.Close()
	withStorage(t, "redis", nil, client, nil)
	if got := statestorageSet("Vehicle.Speed", "7", "ts"); got != 0 {
		t.Fatalf("got %d; want 0", got)
	}
	if len(rec.all()) != 1 {
		t.Errorf("commands = %v; want one", rec.all())
	}
}

func TestStatestorageSet_MemcacheSuccessIsZero(t *testing.T) {
	got := make(chan memcacheSetCmd, 4)
	srv := fakeMemcache(t, got, "STORED\r\n")
	withStorage(t, "memcache", nil, nil, memcache.New(srv.addr()))
	if r := statestorageSet("Vehicle.Speed", "7", "ts"); r != 0 {
		t.Fatalf("got %d; want 0", r)
	}
	if c := <-got; c.key != "Vehicle.Speed" {
		t.Errorf("key = %q", c.key)
	}
}

// Only the selected backend is written to.
func TestStatestorageSet_OnlyTheSelectedBackendIsUsed(t *testing.T) {
	rec := &recorder{}
	srv := fakeRedis(t, rec, "+OK\r\n")
	client := newTestRedisClient(srv.addr())
	defer client.Close()
	db := newTestSqliteDb(t, "Vehicle.Speed")
	withStorage(t, "sqlite", db, client, nil)
	if got := statestorageSet("Vehicle.Speed", "7", "ts"); got != 0 {
		t.Fatalf("got %d; want 0", got)
	}
	if n := len(rec.all()); n != 0 {
		t.Errorf("redis received %d commands although sqlite is selected", n)
	}
}

func TestStatestorageSet_FailureIsMinusOne(t *testing.T) {
	t.Run("sqlite table missing", func(t *testing.T) {
		db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "e.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		withStorage(t, "sqlite", db, nil, nil)
		if got := statestorageSet("p", "v", "t"); got != -1 {
			t.Errorf("got %d; want -1", got)
		}
	})
	t.Run("redis unreachable", func(t *testing.T) {
		client := newTestRedisClient(closedAddr(t))
		defer client.Close()
		withStorage(t, "redis", nil, client, nil)
		if got := statestorageSet("p", "v", "t"); got != -1 {
			t.Errorf("got %d; want -1", got)
		}
	})
	t.Run("memcache unreachable", func(t *testing.T) {
		withStorage(t, "memcache", nil, nil, memcache.New(closedAddr(t)))
		if got := statestorageSet("p", "v", "t"); got != -1 {
			t.Errorf("got %d; want -1", got)
		}
	})
}

// main only logs an error, and carries on, if the sqlite database file is missing, so the
// handle can be nil when the first write arrives. That used to panic.
func TestStatestorageSet_UninitialisedBackendIsMinusOneNotAPanic(t *testing.T) {
	for _, dbType := range []string{"sqlite", "redis", "memcache"} {
		t.Run(dbType, func(t *testing.T) {
			withStorage(t, dbType, nil, nil, nil)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			if got := statestorageSet("p", "v", "t"); got != -1 {
				t.Errorf("got %d; want -1", got)
			}
		})
	}
}

func TestStatestorageSet_NoOrUnknownBackendIsMinusOne(t *testing.T) {
	for _, dbType := range []string{"", "none", "bogus"} {
		withStorage(t, dbType, nil, nil, nil)
		if got := statestorageSet("p", "v", "t"); got != -1 {
			t.Errorf("stateDbType %q: got %d; want -1", dbType, got)
		}
	}
}
