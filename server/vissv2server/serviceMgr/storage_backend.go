/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* StorageBackend interface + per-backend adapters used by serviceMgr.
*
* This file is the third follow-up to PR #129 and the deferred work
* noted in #131's description. Previously, getVehicleData and
* setVehicleData each carried a long switch statement on the
* stateDbType string, with the database client (dbHandle, redisClient,
* memcacheClient, IoTDBsession) held in package globals. The interface
* makes each backend a self-contained unit, the adapters own their
* dependencies, and tests can substitute a mock implementation without
* poking package globals.
**/
package serviceMgr

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/apache/iotdb-client-go/client"
	"github.com/bradfitz/gomemcache/memcache"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	"strconv"
)

// StorageBackend abstracts the vehicle-state storage system used by
// serviceMgr. Implementations exist for sqlite, redis, memcache,
// apache-iotdb, and a "none" backend that emits the dummyValue
// counter on Get and always fails Set.
type StorageBackend interface {
	// Get returns the canonical {"value":"...", "ts":"..."} JSON
	// string for the data point at path. On error or missing data,
	// returns the same sentinel-value JSON the original inline
	// switches emitted (e.g. {"value":"visserr:Data-not-available",
	// "ts":"..."}). Callers (getDataPackMap and subscribe LatestDataPoint
	// capture) pass the result through to clients as-is.
	Get(path string) string

	// Set writes value at path. Returns the timestamp used for the
	// write, or "" on failure -- handleServiceSet relies on the
	// empty return to emit the service_unavailable response.
	Set(path, value string) string
}

// sqliteBackend writes/reads vehicle state through a *sql.DB handle
// against the VSS_MAP table. The original schema was
//
//	CREATE TABLE VSS_MAP (path TEXT, c_value TEXT, c_ts TEXT, d_value TEXT, d_ts TEXT)
//
// (c_* are the current value/ts, d_* are the desired). Get reads
// c_value+c_ts, Set updates d_value+d_ts -- preserving the original
// inline arms exactly.
type sqliteBackend struct {
	db *sql.DB
}

func newSqliteBackend(db *sql.DB) *sqliteBackend {
	return &sqliteBackend{db: db}
}

func (s *sqliteBackend) Get(path string) string {
	rows, err := s.db.Query("SELECT `c_value`, `c_ts` FROM VSS_MAP WHERE `path`=?", path)
	if err != nil {
		return `{"value":"Data-error", "ts":"` + utils.GetRfcTime() + `"}`
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			utils.Warning.Printf("sqliteBackend.Get: rows.Err for path=%s err=%v", path, err)
		}
		return `{"value":"visserr:Data-not-available", "ts":"` + utils.GetRfcTime() + `"}`
	}
	value := ""
	timestamp := ""
	if err := rows.Scan(&value, &timestamp); err != nil {
		utils.Warning.Printf("Data not found: %s for path=%s", err, path)
		return `{"value":"visserr:Data-not-available", "ts":"` + utils.GetRfcTime() + `"}`
	}
	return `{"value":"` + value + `", "ts":"` + timestamp + `"}`
}

func (s *sqliteBackend) Set(path, value string) string {
	ts := utils.GetRfcTime()
	stmt, err := s.db.Prepare("UPDATE VSS_MAP SET d_value=?, d_ts=? WHERE `path`=?")
	if err != nil {
		utils.Error.Printf("Could not prepare for statestorage updating, err = %s", err)
		return ""
	}
	defer stmt.Close()
	trimmedPath := path
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		trimmedPath = path[1 : len(path)-1]
	}
	_, err = stmt.Exec(value, ts, trimmedPath)
	if err != nil {
		utils.Error.Printf("Could not update statestorage, err = %s", err)
		return ""
	}
	return ts
}

// redisBackend reads from a redis client directly and writes by
// pushing a JSON set message to the feeder via toFeeder (the original
// arm did the same -- redis writes go through the feeder so it can
// fan them out to subscribers).
type redisBackend struct {
	client   *redis.Client
	toFeeder chan string
}

func newRedisBackend(c *redis.Client, toFeeder chan string) *redisBackend {
	return &redisBackend{client: c, toFeeder: toFeeder}
}

func (r *redisBackend) Get(path string) string {
	dp, err := r.client.Get(path).Result()
	if err != nil {
		if err.Error() != "redis: nil" {
			utils.Error.Printf("Job failed. Error()=%s", err.Error())
			return `{"value":"Database-error", "ts":"` + utils.GetRfcTime() + `"}`
		}
		return `{"value":"visserr:Data-not-available", "ts":"` + utils.GetRfcTime() + `"}`
	}
	return dp
}

func (r *redisBackend) Set(path, value string) string {
	ts := utils.GetRfcTime()
	message := `{"action": "set", "data": {"path":"` + path + `", "dp":{"value":"` + value + `", "ts":"` + ts + `"}}}`
	r.toFeeder <- message
	return ts
}

// memcacheBackend has the same shape as redisBackend -- reads
// directly through the client, writes through the feeder. The
// original arms shared a fallthrough; we make the duplication
// explicit by giving memcache its own struct.
type memcacheBackend struct {
	client   *memcache.Client
	toFeeder chan string
}

func newMemcacheBackend(c *memcache.Client, toFeeder chan string) *memcacheBackend {
	return &memcacheBackend{client: c, toFeeder: toFeeder}
}

func (m *memcacheBackend) Get(path string) string {
	mcItem, err := m.client.Get(path)
	if err != nil {
		if err.Error() != "memcache: cache miss" {
			utils.Error.Printf("Job failed. Error()=%s", err.Error())
			return `{"value":"Database-error", "ts":"` + utils.GetRfcTime() + `"}`
		}
		utils.Warning.Printf("Data not found.")
		return `{"value":"visserr:Data-not-available", "ts":"` + utils.GetRfcTime() + `"}`
	}
	return string(mcItem.Value)
}

func (m *memcacheBackend) Set(path, value string) string {
	ts := utils.GetRfcTime()
	message := `{"action": "set", "data": {"path":"` + path + `", "dp":{"value":"` + value + `", "ts":"` + ts + `"}}}`
	m.toFeeder <- message
	return ts
}

// iotdbBackend talks to an Apache IoTDB session. The config struct
// (host/port/prefix/timeout) is held alongside the session so the
// PrefixPath and Timeout are available for query construction.
type iotdbBackend struct {
	session client.Session
	config  IoTDBConfiguration
}

func newIoTDBBackend(session client.Session, config IoTDBConfiguration) *iotdbBackend {
	return &iotdbBackend{session: session, config: config}
}

func (i *iotdbBackend) Get(path string) string {
	// Back-quote the VSS node for the DB query, e.g. `Vehicle.CurrentLocation.Longitude`
	selectLastSQL := fmt.Sprintf("select last `%v` from %v", path, i.config.PrefixPath)
	value := ""
	ts := ""
	sessionDataSet, err := i.session.ExecuteQueryStatement(selectLastSQL, &i.config.Timeout)
	if err != nil {
		utils.Error.Printf("IoTDB: Query failed with error=%s", err)
		return `{"value":"visserr:Data-not-available", "ts":"` + utils.GetRfcTime() + `"}`
	}
	success, nextErr := sessionDataSet.Next()
	if nextErr == nil && success {
		value = sessionDataSet.GetText("Value")
		ts = sessionDataSet.GetText(client.TimestampColumnName)
	}
	sessionDataSet.Close()
	return `{"value":"` + value + `", "ts":"` + ts + `"}`
}

func (i *iotdbBackend) Set(path, value string) string {
	ts := utils.GetRfcTime()
	vssKey := []string{"`" + path + "`"} // Back-quote the VSS node for the DB insert
	vssValue := []string{value}
	IoTDBts := time.Now().UTC().UnixNano() / 1000000

	status, err := i.session.InsertStringRecord(i.config.PrefixPath, vssKey, vssValue, IoTDBts)
	if err != nil {
		utils.Error.Printf("IoTDB: DB insert using InsertStringRecord failed with: %v", err)
		return ""
	}
	if status != nil {
		if err = client.VerifySuccess(status); err != nil {
			utils.Error.Printf("IoTDB: DB insert Verify failed with: %v", err)
			return ""
		}
	}
	return ts
}

// noneBackend is the zero-dependency backend used when serviceMgr is
// run without real state storage. Get returns the dummyValue counter
// the package maintains as a sanity heartbeat; Set always returns ""
// (failure) -- matching the original setVehicleData which had no
// "none" case and fell through to the empty-string return.
//
// noneBackend is also the package-level default for stateBackend, so
// getVehicleData / setVehicleData never nil-deref before
// ServiceMgrInit runs (e.g. in tests).
type noneBackend struct{}

func newNoneBackend() *noneBackend {
	return &noneBackend{}
}

func (n *noneBackend) Get(path string) string {
	return `{"value":"` + strconv.Itoa(dummyValue) + `", "ts":"` + utils.GetRfcTime() + `"}`
}

func (n *noneBackend) Set(path, value string) string {
	return "" // signals service_unavailable to handleServiceSet
}
