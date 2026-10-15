package main

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
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
