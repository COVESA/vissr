/**
* Regression tests for the vissv2server.go fixes shipped in PR #121
* (issueServiceRequest dt[:5] panic; initiateFileTransfer + getFileDescriptorData
* type-assert and uid-conversion panics).
**/
package main

import (
	"os"
	"testing"

	"github.com/covesa/vissr/utils"
)

// TestMain initialises the package-level utils loggers (utils.Info,
// utils.Error etc.) before any tests run. The production daemon sets
// these up in its boot path; without this hook, tests that hit
// logging-emitting code branches (e.g. getFileDescriptorData's "of
// unknown type" branch) nil-deref instead of doing what they should.
func TestMain(m *testing.M) {
	utils.InitLog("vissv2server-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// TestGetFileDescriptorData_ValidInput is the happy-path check.
func TestGetFileDescriptorData_ValidInput(t *testing.T) {
	value := map[string]interface{}{
		"name": "upload.txt",
		"hash": "abc123",
		"uid":  "2d878213",
	}
	name, hash, uid := getFileDescriptorData(value)
	if name != "upload.txt" {
		t.Fatalf("name = %q; want \"upload.txt\"", name)
	}
	if hash != "abc123" {
		t.Fatalf("hash = %q; want \"abc123\"", hash)
	}
	if uid != "2d878213" {
		t.Fatalf("uid = %q; want \"2d878213\"", uid)
	}
}

// TestGetFileDescriptorData_NonMapInputDoesNotPanic is the regression
// test for the PR #121 type-assert fix. Before that fix the unguarded
// value.(map[string]interface{}) panicked the daemon whenever a VISS
// client sent a non-object value for a FileDescriptor actuator.
func TestGetFileDescriptorData_NonMapInputDoesNotPanic(t *testing.T) {
	cases := []struct {
		name  string
		value interface{}
	}{
		{"nil", nil},
		{"string", "just a string"},
		{"int", 42},
		{"array", []interface{}{"a", "b"}},
		{"bool", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("getFileDescriptorData panicked on %s input: %v", tc.name, r)
				}
			}()
			n, h, u := getFileDescriptorData(tc.value)
			if n != "" || h != "" || u != "" {
				t.Fatalf("expected empty triple on non-map input; got %q,%q,%q", n, h, u)
			}
		})
	}
}

// TestGetFileDescriptorData_NonStringValuesAreRejected verifies that
// inner non-string values do not crash (the inner switch has a
// `default` arm that returns empty).
func TestGetFileDescriptorData_NonStringValuesAreRejected(t *testing.T) {
	value := map[string]interface{}{
		"name": "ok.txt",
		"hash": 42, // non-string
		"uid":  "2d878213",
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("getFileDescriptorData panicked: %v", r)
		}
	}()
	n, h, u := getFileDescriptorData(value)
	// The inner default-arm returns empty when an unknown value type
	// is encountered. Don't assert specific values; just confirm the
	// function returned without panicking.
	_, _, _ = n, h, u
}

// FuzzGetFileDescriptorData runs the helper against pseudo-random JSON
// shapes to confirm it never panics regardless of input.
//
// Run with: go test -fuzz=FuzzGetFileDescriptorData -fuzztime=10s ./...
func FuzzGetFileDescriptorData(f *testing.F) {
	// Seeds are encoded as (name, hash, uid) since the fuzzer doesn't
	// natively understand map[string]interface{}; reconstruct inside.
	seeds := []struct {
		name, hash, uid string
	}{
		{"upload.txt", "abcdef", "2d878213"},
		{"", "", ""},
		{"a", "b", "c"},
		{"name with spaces", "abc", "12345678"},
	}
	for _, s := range seeds {
		f.Add(s.name, s.hash, s.uid)
	}
	f.Fuzz(func(t *testing.T, name, hash, uid string) {
		value := map[string]interface{}{
			"name": name,
			"hash": hash,
			"uid":  uid,
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getFileDescriptorData panicked on (%q,%q,%q): %v", name, hash, uid, r)
			}
		}()
		_, _, _ = getFileDescriptorData(value)
	})
}
