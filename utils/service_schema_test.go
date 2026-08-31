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

	if got := JsonSchemaValidate(`{"action":"discover","path":"Vehicle","depth":"0","requestId":"2"}`); got != "" {
		t.Errorf("valid discover rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_DiscoverMissingDepth_Rejected confirms "depth" is
// now a required discoveryRequest field (per the Discover spec revision:
// depth replaces the resource filter, and metadata for all instances of a
// multiplexed service is always returned).
func TestJsonSchemaValidate_DiscoverMissingDepth_Rejected(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	if got := JsonSchemaValidate(`{"action":"discover","path":"Vehicle","requestId":"2"}`); got == "" {
		t.Error("discover without depth was accepted; want rejection")
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

// ---- "resource" filter variant (§2/§7 item 4) -------------------------------
//
// These validate the updated vissv3.2-service-schema.json's filter $defs
// against the exact request shapes used by
// client/client-1.0/Javascript/appclient_service_commands.txt, confirming
// the schema update (draft-07/2019-09 tuple 'items', not 2020-12
// 'prefixItems' — qri-io/jsonschema does not implement 'prefixItems') is
// actually effective against the real Go-side validator, not just
// well-formed JSON Schema in isolation.

// TestJsonSchemaValidate_ResourceFilterArray_ResourceFirst is the exact
// MoveSeat invoke request shape from appclient_service_commands.txt
// (requestId 8756): filter is an array of [resource, timebased].
func TestJsonSchemaValidate_ResourceFilterArray_ResourceFirst(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "invoke",
		"path": "VehicleService.Seating.MoveSeat",
		"input": {"MovementType": "longitudinal", "Position": "40"},
		"filter": [{"variant": "resource", "parameter": ["Row1.DriverSide"]}, {"variant": "timebased", "parameter": {"period": "250"}}],
		"requestId": "8756"
	}`
	if got := JsonSchemaValidate(req); got != "" {
		t.Errorf("valid resource+timebased invoke rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_ResourceFilterArray_MonitorAll is
// appclient_service_commands.txt's monitor request: filter [resource, all].
func TestJsonSchemaValidate_ResourceFilterArray_MonitorAll(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "monitor",
		"path": "VehicleService.Seating.MoveSeat",
		"filter": [{"variant": "resource", "parameter": ["Row1.DriverSide"]}, {"variant": "all"}],
		"requestId": "8757"
	}`
	if got := JsonSchemaValidate(req); got != "" {
		t.Errorf("valid resource+all monitor rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_ResourceFilterArray_MonitorFirst confirms the
// combination validates with the monitoring variant listed first (order
// should not matter — the schema's two-branch oneOf covers both orderings).
func TestJsonSchemaValidate_ResourceFilterArray_MonitorFirst(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "monitor",
		"path": "VehicleService.Seating.MoveSeat",
		"filter": [{"variant": "status"}, {"variant": "resource", "parameter": ["Row1.DriverSide"]}],
		"requestId": "8757b"
	}`
	if got := JsonSchemaValidate(req); got != "" {
		t.Errorf("valid status+resource (reversed order) monitor rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_DiscoverIgnoresLeftoverFilterProperty confirms a
// discover request carrying a leftover "filter" property (from the older,
// now-removed resource-filter-based narrowing) still validates as long as
// "depth" is present — discover-message doesn't reference "filter" in its
// $defs at all any more, and additionalProperties is unset (defaults to
// allowed) throughout this schema, so an extra unrecognised property is not
// itself a validation error.
func TestJsonSchemaValidate_DiscoverIgnoresLeftoverFilterProperty(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "discover",
		"path": "VehicleService.Seating.MoveSeat",
		"depth": "0",
		"filter": {"variant": "resource", "parameter": ["Row1.DriverSide"]},
		"requestId": "d1"
	}`
	if got := JsonSchemaValidate(req); got != "" {
		t.Errorf("discover with depth and a leftover filter property rejected = %q; want \"\"", got)
	}
}

// TestJsonSchemaValidate_FilterArrayTwoResourceFiltersRejected confirms an
// array combining two "resource" filters (invalid — the array form requires
// exactly one resource filter and one monitoring filter) is rejected.
func TestJsonSchemaValidate_FilterArrayTwoResourceFiltersRejected(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "invoke",
		"path": "VehicleService.Seating.MoveSeat",
		"input": {"MovementType": "longitudinal", "Position": "40"},
		"filter": [{"variant": "resource", "parameter": ["Row1"]}, {"variant": "resource", "parameter": ["Row2"]}],
		"requestId": "bad1"
	}`
	if got := JsonSchemaValidate(req); got == "" {
		t.Error("invoke with two 'resource' filters in the array was accepted; schema not enforcing the combination rule")
	}
}

// TestJsonSchemaValidate_FilterArrayTwoMonitoringFiltersRejected confirms an
// array combining two non-resource monitoring filters (invalid) is rejected.
func TestJsonSchemaValidate_FilterArrayTwoMonitoringFiltersRejected(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "invoke",
		"path": "VehicleService.Seating.MoveSeat",
		"input": {"MovementType": "longitudinal", "Position": "40"},
		"filter": [{"variant": "status"}, {"variant": "all"}],
		"requestId": "bad2"
	}`
	if got := JsonSchemaValidate(req); got == "" {
		t.Error("invoke with two monitoring filters in the array was accepted; schema not enforcing the combination rule")
	}
}

// TestJsonSchemaValidate_ResourceFilterMissingParameterRejected confirms a
// "resource" filter with no "parameter" array is rejected (required field).
func TestJsonSchemaValidate_ResourceFilterMissingParameterRejected(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "invoke",
		"path": "VehicleService.Seating.MoveSeat",
		"input": {"MovementType": "longitudinal", "Position": "40"},
		"filter": {"variant": "resource"},
		"requestId": "bad3"
	}`
	if got := JsonSchemaValidate(req); got == "" {
		t.Error("'resource' filter missing 'parameter' was accepted; schema not enforcing required field")
	}
}

// TestJsonSchemaValidate_GetCapabilitiesNoneFilter is
// appclient_service_commands.txt's GetCapabilities invoke: a plain
// {"variant":"none"} filter (unaffected by the resource-filter additions).
func TestJsonSchemaValidate_GetCapabilitiesNoneFilter(t *testing.T) {
	loadBaseSchema(t)
	loadServiceSchema(t)

	req := `{
		"action": "invoke",
		"path": "VehicleService.Seating.GetCapabilities",
		"filter": {"variant": "none"},
		"requestId": "8760"
	}`
	if got := JsonSchemaValidate(req); got != "" {
		t.Errorf("valid GetCapabilities invoke (none filter) rejected = %q; want \"\"", got)
	}
}
