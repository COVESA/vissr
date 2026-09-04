/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
 * Tests for the VISSv3.2 gRPC JSON<->protobuf conversion functions added
 * alongside the pre-existing Get / Set / Subscribe / Unsubscribe functions
 * in grcputils.go:
*
*   - MultiGetRequestJsonToPb / MultiGetRequestPbToJson /
*     MultiGetResponseJsonToPb / MultiGetResponsePbToJson
*   - MultiSetRequestJsonToPb / MultiSetRequestPbToJson
*   - DiscoverRequestJsonToPb / DiscoverRequestPbToJson /
*     DiscoverResponseJsonToPb / DiscoverResponsePbToJson
*
* Same shape/coverage goals as grcputils_test.go: happy path, malformed
* input does not panic, and (where relevant) a round trip preserves the
* interesting fields.
**/
package utils

import (
	"encoding/json"
	"testing"

	pb "github.com/covesa/vissr/grpc_pb"
)

// --------------------------------------------------------------------------
// MultiGetRequest
// --------------------------------------------------------------------------

func TestMultiGetRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"get","data":["Vehicle.Speed","Trailer1.Cargo.Hold1.Temperature"],"requestId":"8756"}`
	got := MultiGetRequestJsonToPb(in)
	if got == nil {
		t.Fatalf("got nil")
	}
	if len(got.GetData()) != 2 || got.GetData()[0] != "Vehicle.Speed" || got.GetData()[1] != "Trailer1.Cargo.Hold1.Temperature" {
		t.Errorf("Data = %v", got.GetData())
	}
	if got.GetRequestId() != "8756" {
		t.Errorf("RequestId = %q; want 8756", got.GetRequestId())
	}
}

func TestMultiGetRequestJsonToPb_WithAuthAndDc(t *testing.T) {
	in := `{"action":"get","data":["Vehicle.Speed"],"authorization":"tok","dc":"2+1","requestId":"1"}`
	got := MultiGetRequestJsonToPb(in)
	if got.GetAuthorization() != "tok" {
		t.Errorf("Authorization = %q; want tok", got.GetAuthorization())
	}
	if got.GetDC() != "2+1" {
		t.Errorf("DC = %q; want 2+1", got.GetDC())
	}
}

func TestMultiGetRequestJsonToPb_BadJSON(t *testing.T) {
	if got := MultiGetRequestJsonToPb("not json"); got != nil {
		t.Errorf("expected nil for bad JSON; got %+v", got)
	}
}

func TestMultiGetRequestJsonToPb_MissingFieldsDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("missing fields panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"data":"not-an-array"}`,
		`{"data":[42, "Vehicle.Speed"]}`, // non-string element is skipped, not fatal
		`{"data":["X"],"authorization":42}`,
		`{"data":["X"],"dc":42}`,
		`{"data":["X"],"requestId":42}`,
	}
	for _, in := range cases {
		got := MultiGetRequestJsonToPb(in)
		if got == nil {
			t.Errorf("input %q produced nil - should produce empty/partial proto", in)
		}
	}
}

func TestMultiGetRequestJsonToPb_NonStringElementSkipped(t *testing.T) {
	got := MultiGetRequestJsonToPb(`{"data":[42, "Vehicle.Speed"]}`)
	if got == nil {
		t.Fatalf("got nil")
	}
	if len(got.GetData()) != 1 || got.GetData()[0] != "Vehicle.Speed" {
		t.Errorf("Data = %v; want [\"Vehicle.Speed\"] (non-string element skipped)", got.GetData())
	}
}

func TestMultiGetRequestPbToJson_RoundTrip(t *testing.T) {
	pbReq := &pb.MultiGetRequestMessage{
		Data:      []string{"Vehicle.Speed", "Vehicle.Powertrain.CombustionEngine.RPM"},
		RequestId: "8756",
	}
	jsonOut := MultiGetRequestPbToJson(pbReq)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("MultiGetRequestPbToJson produced invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	if m["action"] != "get" {
		t.Errorf("action = %v; want get", m["action"])
	}
	data, ok := m["data"].([]interface{})
	if !ok || len(data) != 2 {
		t.Fatalf("data = %v; want a 2-element array", m["data"])
	}
	if data[0] != "Vehicle.Speed" || data[1] != "Vehicle.Powertrain.CombustionEngine.RPM" {
		t.Errorf("data = %v", data)
	}
	if m["requestId"] != "8756" {
		t.Errorf("requestId = %v; want 8756", m["requestId"])
	}
}

func TestMultiGetRequestPbToJson_EmptyDataProducesEmptyArray(t *testing.T) {
	pbReq := &pb.MultiGetRequestMessage{RequestId: "1"}
	jsonOut := MultiGetRequestPbToJson(pbReq)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	data, ok := m["data"].([]interface{})
	if !ok || len(data) != 0 {
		t.Errorf("data = %v; want empty array", m["data"])
	}
}

// --------------------------------------------------------------------------
// MultiGetResponse
// --------------------------------------------------------------------------

func TestMultiGetResponseJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"get","requestId":"8756","data":[{"path":"Vehicle.Powertrain.CombustionEngine.RPM","dp":{"value":"2372","ts":"2020-04-15T13:37:00Z"}},{"path":"Trailer1.Cargo.Hold1.Temperature","dp":{"value":"-1","ts":"2020-04-15T13:36:00Z"}}],"ts":"2020-04-15T13:37:05Z"}`
	got := MultiGetResponseJsonToPb(in)
	if got == nil {
		t.Fatalf("got nil")
	}
	if got.GetStatus() != pb.ResponseStatus_SUCCESS {
		t.Errorf("Status = %v; want SUCCESS", got.GetStatus())
	}
	data := got.GetSuccessResponse().GetDataPack().GetData()
	if len(data) != 2 {
		t.Fatalf("DataPack.Data len = %d; want 2", len(data))
	}
	if data[0].GetPath() != "Vehicle.Powertrain.CombustionEngine.RPM" {
		t.Errorf("data[0].Path = %q", data[0].GetPath())
	}
	if data[0].GetDp()[0].GetValue() != "2372" {
		t.Errorf("data[0].Dp[0].Value = %q; want 2372", data[0].GetDp()[0].GetValue())
	}
}

func TestMultiGetResponseJsonToPb_ErrorResponse(t *testing.T) {
	in := `{"action":"get","requestId":"8756","error":{"number":"404","reason":"unavailable_data","description":"x"},"ts":"2020-04-15T13:37:00Z"}`
	got := MultiGetResponseJsonToPb(in)
	if got.GetStatus() != pb.ResponseStatus_ERROR {
		t.Errorf("Status = %v; want ERROR", got.GetStatus())
	}
	if got.GetErrorResponse().GetReason() != "unavailable_data" {
		t.Errorf("ErrorResponse.Reason = %q", got.GetErrorResponse().GetReason())
	}
}

func TestMultiGetResponseJsonToPb_MalformedDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"data":"not an array"}`,
		`{"error":"not a map"}`,
		`{"error":42}`,
	}
	for _, in := range cases {
		_ = MultiGetResponseJsonToPb(in)
	}
}

func TestMultiGetResponsePbToJson_RoundTrip(t *testing.T) {
	pbResp := &pb.MultiGetResponseMessage{
		Status: pb.ResponseStatus_SUCCESS,
		SuccessResponse: &pb.MultiGetResponseMessage_SuccessResponseMessage{
			DataPack: &pb.DataPackages{
				Data: []*pb.DataPackages_DataPackage{
					{Path: "Vehicle.Speed", Dp: []*pb.DataPackages_DataPackage_DataPoint{{Value: "10", Ts: "2020-04-15T13:37:00Z"}}},
				},
			},
		},
		RequestId: "1",
		Ts:        "2020-04-15T13:37:05Z",
	}
	jsonOut := MultiGetResponsePbToJson(pbResp)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	if m["action"] != "get" {
		t.Errorf("action = %v; want get", m["action"])
	}
	if _, present := m["error"]; present {
		t.Errorf("unexpected error field in success response: %v", m["error"])
	}
}

// --------------------------------------------------------------------------
// MultiSetRequest
// --------------------------------------------------------------------------

func TestMultiSetRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"set","data":[{"path":"Vehicle.Powertrain.Transmission.PerformanceMode","value":"sport"}],"requestId":"5687"}`
	got := MultiSetRequestJsonToPb(in)
	if got == nil {
		t.Fatalf("got nil")
	}
	if len(got.GetData()) != 1 {
		t.Fatalf("Data len = %d; want 1", len(got.GetData()))
	}
	if got.GetData()[0].GetPath() != "Vehicle.Powertrain.Transmission.PerformanceMode" {
		t.Errorf("Data[0].Path = %q", got.GetData()[0].GetPath())
	}
	if got.GetData()[0].GetValue() != "sport" {
		t.Errorf("Data[0].Value = %q; want sport", got.GetData()[0].GetValue())
	}
	if got.GetRequestId() != "5687" {
		t.Errorf("RequestId = %q; want 5687", got.GetRequestId())
	}
}

func TestMultiSetRequestJsonToPb_MultipleElements(t *testing.T) {
	in := `{"action":"set","data":[{"path":"A","value":"1"},{"path":"B","value":"2"}],"requestId":"1"}`
	got := MultiSetRequestJsonToPb(in)
	if len(got.GetData()) != 2 {
		t.Fatalf("Data len = %d; want 2", len(got.GetData()))
	}
	if got.GetData()[1].GetPath() != "B" || got.GetData()[1].GetValue() != "2" {
		t.Errorf("Data[1] = %+v", got.GetData()[1])
	}
}

func TestMultiSetRequestJsonToPb_BadJSON(t *testing.T) {
	if got := MultiSetRequestJsonToPb("not json"); got != nil {
		t.Errorf("expected nil for bad JSON; got %+v", got)
	}
}

func TestMultiSetRequestJsonToPb_MissingFieldsDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("missing fields panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"data":"not-an-array"}`,
		`{"data":[42]}`,                     // non-object element skipped
		`{"data":[{"path":"X"}]}`,           // missing value, skipped
		`{"data":[{"value":"1"}]}`,          // missing path, skipped
		`{"data":[{"path":"X","value":1}]}`, // non-string value, skipped
		`{"data":["X"],"requestId":42}`,
		`{"data":["X"],"authorization":42}`,
	}
	for _, in := range cases {
		got := MultiSetRequestJsonToPb(in)
		if got == nil {
			t.Errorf("input %q produced nil - should produce empty/partial proto", in)
		}
	}
}

func TestMultiSetRequestJsonToPb_InvalidElementsAreSkippedNotFatal(t *testing.T) {
	// Only the third element is well-formed; the others should be
	// silently dropped rather than aborting the whole conversion.
	in := `{"action":"set","data":[42,{"path":"NoValue"},{"path":"X","value":"1"}],"requestId":"1"}`
	got := MultiSetRequestJsonToPb(in)
	if got == nil {
		t.Fatalf("got nil")
	}
	if len(got.GetData()) != 1 {
		t.Fatalf("Data len = %d; want 1 (only the well-formed element)", len(got.GetData()))
	}
	if got.GetData()[0].GetPath() != "X" || got.GetData()[0].GetValue() != "1" {
		t.Errorf("Data[0] = %+v", got.GetData()[0])
	}
}

func TestMultiSetRequestPbToJson_RoundTrip(t *testing.T) {
	pbReq := &pb.MultiSetRequestMessage{
		Data: []*pb.MultiSetRequestMessage_SetData{
			{Path: "Vehicle.Powertrain.Transmission.PerformanceMode", Value: "sport"},
		},
		RequestId: "5687",
	}
	jsonOut := MultiSetRequestPbToJson(pbReq)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	if m["action"] != "set" {
		t.Errorf("action = %v; want set", m["action"])
	}
	data, ok := m["data"].([]interface{})
	if !ok || len(data) != 1 {
		t.Fatalf("data = %v; want a 1-element array", m["data"])
	}
	elem, ok := data[0].(map[string]interface{})
	if !ok || elem["path"] != "Vehicle.Powertrain.Transmission.PerformanceMode" || elem["value"] != "sport" {
		t.Errorf("data[0] = %v", data[0])
	}
	if m["requestId"] != "5687" {
		t.Errorf("requestId = %v; want 5687", m["requestId"])
	}
}

// --------------------------------------------------------------------------
// DiscoverRequest
// --------------------------------------------------------------------------

func TestDiscoverRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"discover","path":"Vehicle.Powertrain.FuelSystem","depth":"2","requestId":"5786"}`
	got := DiscoverRequestJsonToPb(in)
	if got == nil {
		t.Fatalf("got nil")
	}
	if got.GetPath() != "Vehicle.Powertrain.FuelSystem" {
		t.Errorf("Path = %q", got.GetPath())
	}
	if got.GetDepth() != "2" {
		t.Errorf("Depth = %q; want 2", got.GetDepth())
	}
	if got.GetRequestId() != "5786" {
		t.Errorf("RequestId = %q; want 5786", got.GetRequestId())
	}
}

func TestDiscoverRequestJsonToPb_ForestDiscoveryPath(t *testing.T) {
	in := `{"action":"discover","path":"HIM","depth":"0","requestId":"5786"}`
	got := DiscoverRequestJsonToPb(in)
	if got.GetPath() != "HIM" || got.GetDepth() != "0" {
		t.Errorf("got Path=%q Depth=%q", got.GetPath(), got.GetDepth())
	}
}

func TestDiscoverRequestJsonToPb_BadJSON(t *testing.T) {
	if got := DiscoverRequestJsonToPb("not json"); got != nil {
		t.Errorf("expected nil for bad JSON; got %+v", got)
	}
}

func TestDiscoverRequestJsonToPb_MissingFieldsDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("missing fields panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"path":42}`,
		`{"depth":42}`,
		`{"requestId":42}`,
	}
	for _, in := range cases {
		got := DiscoverRequestJsonToPb(in)
		if got == nil {
			t.Errorf("input %q produced nil - should produce empty/partial proto", in)
		}
	}
}

func TestDiscoverRequestPbToJson_RoundTrip(t *testing.T) {
	pbReq := &pb.DiscoverRequestMessage{Path: "Vehicle.Powertrain.FuelSystem", Depth: "2", RequestId: "5786"}
	jsonOut := DiscoverRequestPbToJson(pbReq)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	if m["action"] != "discover" {
		t.Errorf("action = %v; want discover", m["action"])
	}
	if m["path"] != "Vehicle.Powertrain.FuelSystem" {
		t.Errorf("path = %v", m["path"])
	}
	if m["depth"] != "2" {
		t.Errorf("depth = %v; want 2", m["depth"])
	}
	if m["requestId"] != "5786" {
		t.Errorf("requestId = %v; want 5786", m["requestId"])
	}
}

// --------------------------------------------------------------------------
// DiscoverResponse
// --------------------------------------------------------------------------

func TestDiscoverResponseJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"discover","metadata":{"FuelSystem":{"type":"branch","children":["HybridType"]}},"requestId":"5786","ts":"2020-04-15T13:37:00Z"}`
	got := DiscoverResponseJsonToPb(in)
	if got == nil {
		t.Fatalf("got nil")
	}
	if got.GetStatus() != pb.ResponseStatus_SUCCESS {
		t.Errorf("Status = %v; want SUCCESS", got.GetStatus())
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(got.GetMetadata()), &metadata); err != nil {
		t.Fatalf("Metadata is not valid JSON: %v, got=%q", err, got.GetMetadata())
	}
	if _, present := metadata["FuelSystem"]; !present {
		t.Errorf("expected metadata to contain FuelSystem key: %v", metadata)
	}
	if got.GetRequestId() != "5786" {
		t.Errorf("RequestId = %q; want 5786", got.GetRequestId())
	}
}

func TestDiscoverResponseJsonToPb_ErrorResponse(t *testing.T) {
	in := `{"action":"discover","requestId":"5786","error":{"number":"400","reason":"invalid_data","description":"Data present in the request is invalid."},"ts":"2020-04-15T13:37:00Z"}`
	got := DiscoverResponseJsonToPb(in)
	if got.GetStatus() != pb.ResponseStatus_ERROR {
		t.Errorf("Status = %v; want ERROR", got.GetStatus())
	}
	if got.GetErrorResponse().GetReason() != "invalid_data" {
		t.Errorf("ErrorResponse.Reason = %q", got.GetErrorResponse().GetReason())
	}
}

func TestDiscoverResponseJsonToPb_MalformedDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"metadata":"not an object, but marshal handles any type"}`,
		`{"error":"not a map"}`,
		`{"error":42}`,
	}
	for _, in := range cases {
		_ = DiscoverResponseJsonToPb(in)
	}
}

func TestDiscoverResponsePbToJson_RoundTrip(t *testing.T) {
	pbResp := &pb.DiscoverResponseMessage{
		Status:    pb.ResponseStatus_SUCCESS,
		Metadata:  `{"FuelSystem":{"type":"branch"}}`,
		RequestId: "5786",
		Ts:        "2020-04-15T13:37:00Z",
	}
	jsonOut := DiscoverResponsePbToJson(pbResp)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	if m["action"] != "discover" {
		t.Errorf("action = %v; want discover", m["action"])
	}
	metadata, ok := m["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata field missing or not an object (double-encoded regression?): %v", m["metadata"])
	}
	if _, present := metadata["FuelSystem"]; !present {
		t.Errorf("expected metadata to contain FuelSystem key: %v", metadata)
	}
	if m["requestId"] != "5786" {
		t.Errorf("requestId = %v; want 5786", m["requestId"])
	}
}

func TestDiscoverResponsePbToJson_EmptyMetadataProducesEmptyObject(t *testing.T) {
	pbResp := &pb.DiscoverResponseMessage{Status: pb.ResponseStatus_SUCCESS, RequestId: "1", Ts: "2020-04-15T13:37:00Z"}
	jsonOut := DiscoverResponsePbToJson(pbResp)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	metadata, ok := m["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata field missing or not an object: %v", m["metadata"])
	}
	if len(metadata) != 0 {
		t.Errorf("expected empty metadata object; got %v", metadata)
	}
}

func TestDiscoverResponsePbToJson_ErrorRoundTrip(t *testing.T) {
	pbResp := &pb.DiscoverResponseMessage{
		Status:        pb.ResponseStatus_ERROR,
		ErrorResponse: &pb.ErrorResponseMessage{Number: "404", Reason: "unavailable_data", Description: "x"},
		RequestId:     "1",
		Ts:            "2020-04-15T13:37:00Z",
	}
	jsonOut := DiscoverResponsePbToJson(pbResp)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, jsonOut)
	}
	errMap, ok := m["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error field; got %v", m)
	}
	if errMap["reason"] != "unavailable_data" {
		t.Errorf("error.reason = %v", errMap["reason"])
	}
	if _, present := m["metadata"]; present {
		t.Errorf("error response should not contain a metadata field: %v", m)
	}
}
