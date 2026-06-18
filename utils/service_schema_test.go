/**
* (C) 2026 Ford Motor Company
*
* Tests for VISSv3.2 service-schema routing in JsonSchemaValidate.
*
* Regression context: vissv3.2-service-schema.json was added to the repo
* but never wired into the validation pipeline — service requests
* (invoke/monitor/cancel/discover) were validated against the base
* vissv3.0-schema.json, which has no service-action branches. The result
* was that the service schema was never applied. These tests load both
* schemas the way JsonSchemaInit does and assert that service actions are
* routed to the service schema while data actions stay on the base schema.
*
* JsonSchemaInit uses sync.Once, so (as in schema_test.go) we read the
* schema files directly and assign the package globals to exercise the
* loaded branches deterministically.
**/
package utils

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/qri-io/jsonschema"
)

const serviceSchemaSourceRelPath = "../server/vissv2server/vissv3.2-service-schema.json"

// loadServiceSchema reads and compiles the production service schema,
// assigning it to the package global for the duration of t. Skips if the
// source file is unreadable.
func loadServiceSchema(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(serviceSchemaSourceRelPath)
	if err != nil {
		t.Skipf("service schema %s not readable (%v); skipping", serviceSchemaSourceRelPath, err)
	}
	prev := serviceSchema
	serviceSchema = jsonschema.Must(string(data))
	t.Cleanup(func() { serviceSchema = prev })
}

// loadBaseSchema reads and compiles the production base schema into the
// package global for the duration of t. Skips if unreadable.
func loadBaseSchema(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(schemaSourceRelPath)
	if err != nil {
		t.Skipf("base schema %s not readable (%v); skipping", schemaSourceRelPath, err)
	}
	prev := jsonSchema
	jsonSchema = jsonschema.Must(string(data))
	t.Cleanup(func() { jsonSchema = prev })
}

// TestReadServiceSchema_WithFile exercises readServiceSchema when the file
// is present (copied into a temp CWD).
func TestReadServiceSchema_WithFile(t *testing.T) {
	data, err := os.ReadFile(serviceSchemaSourceRelPath)
	if err != nil {
		t.Skipf("service schema source not readable (%v); skipping", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/vissv3.2-service-schema.json", data, 0644); err != nil {
		t.Fatalf("write service schema: %v", err)
	}
	chdirTemp(t, dir)

	if got := readServiceSchema(); got == "" {
		t.Error("readServiceSchema returned empty string with file present")
	}
}

// TestReadServiceSchema_WithoutFile exercises the missing-file branch.
func TestReadServiceSchema_WithoutFile(t *testing.T) {
	chdirTemp(t, t.TempDir())
	if got := readServiceSchema(); got != "" {
		t.Errorf("readServiceSchema with no file = %q; want \"\"", got)
	}
}

// TestSchemaActionField checks the action extraction used to pick a schema.
func TestSchemaActionField(t *testing.T) {
	cases := []struct {
		name, req, want string
	}{
		{"invoke", `{"action":"invoke","path":"X"}`, "invoke"},
		{"action not first", `{"requestId":"1","action":"get"}`, "get"},
		{"missing action", `{"path":"X"}`, ""},
		{"malformed json", `{not json`, ""},
		{"action not a string", `{"action":42}`, ""},
		{"empty", ``, ""},
	}
	for _, c := range cases {
		if got := schemaActionField(c.req); got != c.want {
			t.Errorf("%s: schemaActionField(%q) = %q; want %q", c.name, c.req, got, c.want)
		}
	}
}

// TestJsonSchemaValidate_ValidInvokeRoutedToServiceSchema is the core
// regression for the wiring bug: a well-formed invoke request is valid
// under the service schema but INVALID under the base schema (which has
// no invoke branch). If routing works, validation passes; if the base
// schema were still used, it would fail.
func TestJsonSchemaValidate_ValidInvokeRoutedToServiceSchema(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	invoke := `{"action":"invoke","path":"Vehicle.Cabin.SeatService.MoveSeat","filter":{"variant":"all"},"requestId":"1"}`

	if got := JsonSchemaValidate(invoke); got != "" {
		t.Errorf("valid invoke rejected = %q; want \"\" (should validate against service schema)", got)
	}

	// Prove the disagreement: the same request fails the base schema,
	// confirming routing — not a permissive base schema — is what passes it.
	if errs, err := jsonSchema.ValidateBytes(context.Background(), []byte(invoke)); err == nil && len(errs) == 0 {
		t.Error("base schema accepted an invoke request; routing test is not meaningful")
	}
}

// TestJsonSchemaValidate_InvalidInvokeRejected confirms the service schema
// is actually applied: an invoke missing the required "path" must be
// rejected. This is the behaviour whose absence the bug report described
// ("the server did not use the service json scheme").
func TestJsonSchemaValidate_InvalidInvokeRejected(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	missingPath := `{"action":"invoke","filter":{"variant":"all"},"requestId":"1"}`
	if got := JsonSchemaValidate(missingPath); got == "" {
		t.Error("invoke missing required 'path' was accepted; service schema not applied")
	}

	badFilter := `{"action":"invoke","path":"X","filter":{"variant":"bogus"},"requestId":"1"}`
	if got := JsonSchemaValidate(badFilter); got == "" {
		t.Error("invoke with out-of-enum filter variant was accepted; service schema not applied")
	}
}

// TestJsonSchemaValidate_DiscoverRoutedToServiceSchema covers a second
// service action to confirm the routing set, not just invoke.
func TestJsonSchemaValidate_DiscoverRoutedToServiceSchema(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	if got := JsonSchemaValidate(`{"action":"discover","path":"Vehicle","requestId":"2"}`); got != "" {
		t.Errorf("valid discover rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_DataActionUsesBaseSchema confirms non-service
// actions still validate against the base schema and are unaffected.
func TestJsonSchemaValidate_DataActionUsesBaseSchema(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	if got := JsonSchemaValidate(`{"action":"get","path":"Vehicle.Speed","requestId":"1"}`); got != "" {
		t.Errorf("valid get rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_ServiceSchemaNotLoaded confirms the graceful
// degradation message names the service schema when it is unavailable but
// a service action arrives.
func TestJsonSchemaValidate_ServiceSchemaNotLoaded(t *testing.T) {
	prevBase, prevSvc := jsonSchema, serviceSchema
	jsonSchema, serviceSchema = nil, nil
	defer func() { jsonSchema, serviceSchema = prevBase, prevSvc }()

	got := JsonSchemaValidate(`{"action":"invoke","path":"X"}`)
	if got == "" {
		t.Fatal("service action with nil service schema returned no error")
	}
	if !strings.Contains(got, "service JSON schema not loaded") {
		t.Errorf("got %q; want a 'service JSON schema not loaded' message", got)
	}
}
