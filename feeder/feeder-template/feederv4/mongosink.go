/**
* (C) 2026 MongoDB, Inc.
*
* All files and artifacts are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/covesa/vissr/utils"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

/*
* mongoSink stores the set requests the feeder receives from the server in a MongoDB collection.
* The data points of one set request carry the same ts (see setRequestGrouper), so they are upserted into
* one document with _id = ts. The dot-separated VSS path becomes a nested document, e.g.
* CVED.Context.Occupant.Weight=85.5 is stored as {"CVED": {"Context": {"Occupant": {"Weight": 85.5}}}}.
* Writes are done asynchronously so that a slow or unreachable database does not block the feeder.
**/

const MONGO_QUEUE_SIZE = 100
const MONGO_WRITE_TIMEOUT = 5 * time.Second

type setItem struct {
	Path  string
	Value string
	Ts    string
}

// updater is the one operation of a MongoDB collection that the sink uses. *mongo.Collection
// implements it; tests supply a fake.
type updater interface {
	UpdateOne(ctx context.Context, filter any, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
}

type mongoSink struct {
	collection updater
	queue      chan setItem
}

var mongoSinkInst *mongoSink

func newMongoSink(uri string, dbName string, collName string) (*mongoSink, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetAppName("vissr-feederv4"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	sink := newMongoSinkFor(client.Database(dbName).Collection(collName), MONGO_QUEUE_SIZE)
	go sink.writer()
	return sink, nil
}

// newMongoSinkFor creates a sink that writes to collection. The caller starts the writer.
func newMongoSinkFor(collection updater, queueSize int) *mongoSink {
	return &mongoSink{
		collection: collection,
		queue:      make(chan setItem, queueSize),
	}
}

func (m *mongoSink) Write(path string, value string, ts string) {
	select {
	case m.queue <- setItem{Path: path, Value: value, Ts: ts}:
	default:
		utils.Error.Printf("mongoSink: queue full, dropping set for %q", path)
	}
}

// writer stores the queued items, one at a time and in order, until the queue is closed.
// A failed write is logged and the item is dropped; the writer carries on with the next one.
func (m *mongoSink) writer() {
	for item := range m.queue {
		m.store(item)
	}
}

// store upserts one item into the document _id = item.Ts.
func (m *mongoSink) store(item setItem) {
	ctx, cancel := context.WithTimeout(context.Background(), MONGO_WRITE_TIMEOUT)
	defer cancel()
	_, err := m.collection.UpdateOne(ctx, bson.M{"_id": item.Ts}, mongoUpdateDoc(item), options.UpdateOne().SetUpsert(true))
	if err != nil {
		utils.Error.Printf("mongoSink: write failed for %q, err=%s", item.Path, err)
		return
	}
	utils.Info.Printf("mongoSink: stored %s=%s (ts=%s)", item.Path, item.Value, item.Ts)
}

func mongoUpdateDoc(item setItem) bson.M {
	return bson.M{
		"$set":         bson.M{item.Path: mongoTypedValue(item.Value), "updatedAt": time.Now().UTC()},
		"$setOnInsert": bson.M{"ts": item.Ts},
	}
}

// VISS transports all values as strings, store numbers and booleans with their native type so they can be queried as such.
// An array value, e.g. ["118", "95", "75"], is stored as an array of typed values.
func mongoTypedValue(value string) interface{} {
	if strings.HasPrefix(value, "[") {
		var items []interface{}
		if err := json.Unmarshal([]byte(value), &items); err == nil {
			for i, item := range items {
				if str, ok := item.(string); ok {
					items[i] = mongoTypedValue(str)
				}
			}
			return items
		}
	}
	if value == "true" || value == "false" {
		return value == "true"
	}
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
	}
	return value
}
