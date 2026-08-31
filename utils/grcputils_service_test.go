/**
* (C) 2026 Ford Motor Company
*
* Tests for the VISSv3.2 Service profile (invoke/monitor/cancel/discover)
* JSON <-> protobuf conversion helpers added to grcputils.go alongside the
* gRPC InvokeRequest/MonitorRequest/CancelRequest/DiscoverRequest RPCs
* (grpcMgr.go). Mirrors the style/coverage approach of grcputils_test.go:
* round-trip happy paths plus malformed-input cases that must not panic.
**/

package utils

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/covesa/vissr/grpc_pb"
	"google.golang.org/protobuf/types/known/structpb"
)

// --------------------------------------------------------------------------
// ServiceFilter (invoke/monitor "filter")
// --------------------------------------------------------------------------

func TestApplyServiceFilterFromMessage_SingleObject(t *testing.T) {
	m := map[string]interface{}{"filter": map[string]interface{}{"variant": "status"}}
	got := applyServiceFilterFromMessage(m)
	if len(got) != 1 || got[0].GetVariant() != pb.ServiceFilterVariant_STATUS {
		t.Errorf("got %+v", got)
	}
}

func TestApplyServiceFilterFromMessage_Array(t *testing.T) {
	m := map[string]interface{}{"filter": []interface{}{
		map[string]interface{}{"variant": "resource", "parameter": []interface{}{"Row1.DriverSide"}},
		map[string]interface{}{"variant": "timebased", "parameter": map[string]interface{}{"period": "950"}},
	}}
	got := applyServiceFilterFromMessage(m)
	if len(got) != 2 {
		t.Fatalf("got %d filters, want 2", len(got))
	}
	if got[0].GetVariant() != pb.ServiceFilterVariant_RESOURCE || got[0].GetResourcePath()[0] != "Row1.DriverSide" {
		t.Errorf("resource filter: got %+v", got[0])
	}
	if got[1].GetVariant() != pb.ServiceFilterVariant_TIMEBASED_SVC || got[1].GetPeriod() != "950" {
		t.Errorf("timebased filter: got %+v", got[1])
	}
}

func TestApplyServiceFilterFromMessage_Absent(t *testing.T) {
	if got := applyServiceFilterFromMessage(map[string]interface{}{}); got != nil {
		t.Errorf("expected nil for absent filter; got %+v", got)
	}
}

func TestApplyServiceFilterFromMessage_ArrayElementNotObject(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	m := map[string]interface{}{"filter": []interface{}{"not-an-object", 42}}
	got := applyServiceFilterFromMessage(m)
	if len(got) != 0 {
		t.Errorf("expected 0 filters for malformed array elements; got %d", len(got))
	}
}

func TestCreateServiceFilterPb_AllVariants(t *testing.T) {
	cases := []struct {
		name    string
		in      map[string]interface{}
		variant pb.ServiceFilterVariant
	}{
		{"all", map[string]interface{}{"variant": "all"}, pb.ServiceFilterVariant_ALL},
		{"status", map[string]interface{}{"variant": "status"}, pb.ServiceFilterVariant_STATUS},
		{"none", map[string]interface{}{"variant": "none"}, pb.ServiceFilterVariant_NONE},
	}
	for _, tc := range cases {
		got := createServiceFilterPb(tc.in)
		if got.GetVariant() != tc.variant {
			t.Errorf("%s: got variant %v, want %v", tc.name, got.GetVariant(), tc.variant)
		}
	}
}

func TestCreateServiceFilterPb_UnknownVariant(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	got := createServiceFilterPb(map[string]interface{}{"variant": "bogus"})
	if got == nil {
		t.Fatalf("expected non-nil ServiceFilter for unknown variant")
	}
}

func TestGetJsonServiceFilter_RoundTrip(t *testing.T) {
	filters := []*pb.ServiceFilter{
		{Variant: pb.ServiceFilterVariant_RESOURCE, ResourcePath: []string{"Row1.DriverSide", "Row1.PassengerSide"}},
		{Variant: pb.ServiceFilterVariant_TIMEBASED_SVC, Period: strPtr("950")},
	}
	jsonFrag := getJsonServiceFilter(filters)
	full := "{" + strings.TrimPrefix(jsonFrag, ",") + "}"
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(full), &m); err != nil {
		t.Fatalf("produced invalid JSON: %s, err=%s", full, err)
	}
	arr, ok := m["filter"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("got %+v", m["filter"])
	}
}

func TestGetJsonServiceFilter_SingleAndEmpty(t *testing.T) {
	if got := getJsonServiceFilter(nil); got != "" {
		t.Errorf("nil filters: got %q, want empty", got)
	}
	got := getJsonServiceFilter([]*pb.ServiceFilter{{Variant: pb.ServiceFilterVariant_ALL}})
	if !strings.Contains(got, `"variant":"all"`) {
		t.Errorf("got %q", got)
	}
}

func strPtr(s string) *string { return &s }

// --------------------------------------------------------------------------
// ServiceError (unifies invoke/monitor/cancel/discover and monitoring-event
// error shapes)
// --------------------------------------------------------------------------

func TestCreateServiceErrorPb_FullShape(t *testing.T) {
	m := map[string]interface{}{
		"number":      "400",
		"reason":      "bad_request",
		"description": "bad stuff",
		"fields":      []interface{}{"Position", "MovementType"},
	}
	got := createServiceErrorPb(m)
	if got.GetNumber() != "400" || got.GetReason() != "bad_request" || got.GetDescription() != "bad stuff" {
		t.Errorf("got %+v", got)
	}
	if len(got.Fields) != 2 || got.Fields[0] != "Position" {
		t.Errorf("fields: got %+v", got.Fields)
	}
}

func TestCreateServiceErrorPb_MonitoringEventShape(t *testing.T) {
	m := map[string]interface{}{"code": "E1", "message": "failed"}
	got := createServiceErrorPb(m)
	if got.GetCode() != "E1" || got.GetMessage() != "failed" {
		t.Errorf("got %+v", got)
	}
}

func TestGetJsonServiceError_RoundTrip(t *testing.T) {
	se := &pb.ServiceError{Number: strPtr("400"), Reason: strPtr("bad_request"), Description: strPtr("desc"), Fields: []string{"A"}}
	frag := getJsonServiceError(se)
	full := "{\"x\":1" + frag + "}"
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(full), &m); err != nil {
		t.Fatalf("invalid JSON: %s, err=%s", full, err)
	}
	errMap, ok := m["error"].(map[string]interface{})
	if !ok || errMap["number"] != "400" || errMap["reason"] != "bad_request" {
		t.Errorf("got %+v", m["error"])
	}
	fields, ok := errMap["fields"].([]interface{})
	if !ok || len(fields) != 1 || fields[0] != "A" {
		t.Errorf("fields: got %+v", errMap["fields"])
	}
}

func TestGetJsonServiceError_Nil(t *testing.T) {
	if got := getJsonServiceError(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// --------------------------------------------------------------------------
// ServiceOutdata / ServiceIndata / ServiceOutdataWrapper
// --------------------------------------------------------------------------

func TestCreateServiceOutdataWrapperPb_Single(t *testing.T) {
	outdata := map[string]interface{}{"output": map[string]interface{}{"Position": "10"}, "ts": "2026-01-01T00:00:00Z"}
	got := createServiceOutdataWrapperPb(outdata)
	single := got.GetSingle()
	if single == nil || single.GetTs() != "2026-01-01T00:00:00Z" {
		t.Fatalf("got %+v", got)
	}
	if single.GetOutput().AsMap()["Position"] != "10" {
		t.Errorf("output: got %+v", single.GetOutput().AsMap())
	}
}

func TestCreateServiceOutdataWrapperPb_Multi(t *testing.T) {
	outdata := []interface{}{
		map[string]interface{}{"Row1.DriverSide": map[string]interface{}{"output": map[string]interface{}{"Position": "5"}, "ts": "2026-01-01T00:00:00Z"}},
		map[string]interface{}{"Row1.PassengerSide": map[string]interface{}{"output": map[string]interface{}{"Position": "7"}, "ts": "2026-01-01T00:00:01Z"}},
	}
	got := createServiceOutdataWrapperPb(outdata)
	multi := got.GetMulti()
	if multi == nil || len(multi.GetResource()) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateServiceOutdataWrapperPb_NilAndMalformed(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := createServiceOutdataWrapperPb(nil); got != nil {
		t.Errorf("nil input: got %+v, want nil", got)
	}
	if got := createServiceOutdataWrapperPb(42); got != nil {
		t.Errorf("non-map/array input: got %+v, want nil", got)
	}
	if got := createServiceOutdataWrapperPb([]interface{}{"not-a-map"}); got == nil || len(got.GetMulti().GetResource()) != 0 {
		t.Errorf("malformed array element: got %+v", got)
	}
}

func TestGetJsonServiceOutdata_SingleAndMulti(t *testing.T) {
	single := &pb.ServiceOutdataWrapper{OutdataVariant: &pb.ServiceOutdataWrapper_Single{Single: &pb.ServiceOutdata{Ts: "T1"}}}
	got := getJsonServiceOutdata(single)
	if !strings.Contains(got, `"outdata":{"output"`) {
		t.Errorf("single: got %q", got)
	}

	multi := &pb.ServiceOutdataWrapper{OutdataVariant: &pb.ServiceOutdataWrapper_Multi{Multi: &pb.ServiceResourceOutdataArray{
		Resource: []*pb.ServiceResourceOutdata{{ResourceKey: "Row1.DriverSide", Outdata: &pb.ServiceOutdata{Ts: "T2"}}},
	}}}
	got = getJsonServiceOutdata(multi)
	if !strings.Contains(got, `"outdata":[{"Row1.DriverSide"`) {
		t.Errorf("multi: got %q", got)
	}

	if got := getJsonServiceOutdata(nil); got != "" {
		t.Errorf("nil: got %q, want empty", got)
	}
}

func TestServiceIndata_RoundTrip(t *testing.T) {
	m := map[string]interface{}{"input": map[string]interface{}{"Position": "10"}, "ts": "2026-01-01T00:00:00Z"}
	pbIndata := createServiceIndataPb(m)
	if pbIndata.GetTs() != "2026-01-01T00:00:00Z" || pbIndata.GetInput().AsMap()["Position"] != "10" {
		t.Fatalf("got %+v", pbIndata)
	}
	frag := getJsonServiceIndata(pbIndata)
	if !strings.Contains(frag, `"indata":{"input":{"Position":"10"}`) {
		t.Errorf("got %q", frag)
	}
	if got := getJsonServiceIndata(nil); got != "" {
		t.Errorf("nil: got %q, want empty", got)
	}
}

// --------------------------------------------------------------------------
// timeoutStringFromMessage
// --------------------------------------------------------------------------

func TestTimeoutStringFromMessage(t *testing.T) {
	if v, ok := timeoutStringFromMessage(map[string]interface{}{"timeout": "5000"}); !ok || v != "5000" {
		t.Errorf("string case: got %q,%v", v, ok)
	}
	if v, ok := timeoutStringFromMessage(map[string]interface{}{"timeout": float64(5000)}); !ok || v != "5000" {
		t.Errorf("float64 case: got %q,%v", v, ok)
	}
	if _, ok := timeoutStringFromMessage(map[string]interface{}{}); ok {
		t.Errorf("absent case: expected ok=false")
	}
}

// --------------------------------------------------------------------------
// invoke
// --------------------------------------------------------------------------

func TestInvokeRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"invoke","path":"VehicleService.Seating.MoveSeat","input":{"MovementType":"longitudinal","Position":"10"},"filter":{"variant":"status"},"requestId":"8756","timeout":"5000"}`
	got := InvokeRequestJsonToPb(in)
	if got.GetPath() != "VehicleService.Seating.MoveSeat" || got.GetRequestId() != "8756" || got.GetTimeout() != "5000" {
		t.Fatalf("got %+v", got)
	}
	if got.GetInput().AsMap()["Position"] != "10" {
		t.Errorf("input: got %+v", got.GetInput().AsMap())
	}
	if len(got.GetFilter()) != 1 || got.GetFilter()[0].GetVariant() != pb.ServiceFilterVariant_STATUS {
		t.Errorf("filter: got %+v", got.GetFilter())
	}
}

func TestInvokeRequestJsonToPb_BadJSON(t *testing.T) {
	if got := InvokeRequestJsonToPb("not json"); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
}

func TestInvokeRequestPbToJson_RoundTrip(t *testing.T) {
	pbReq := &pb.InvokeRequestMessage{Path: "VehicleService.Seating.GetCapabilities", RequestId: "1"}
	jsonOut := InvokeRequestPbToJson(pbReq)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %s, err=%s", jsonOut, err)
	}
	if m["action"] != "invoke" || m["path"] != "VehicleService.Seating.GetCapabilities" {
		t.Errorf("got %+v", m)
	}
}

func TestInvokeResponseJsonToPb_OngoingWithServiceId(t *testing.T) {
	in := `{"action":"invoke","path":"VehicleService.Seating.MoveSeat","status":"ONGOING","requestId":"8756","ts":"2026-01-01T00:00:00Z","serviceId":"123456"}`
	got := InvokeResponseJsonToPb(in)
	if got.GetStatus() != pb.ServiceStatus_ONGOING || got.GetServiceId() != "123456" {
		t.Fatalf("got %+v", got)
	}
}

func TestInvokeResponseJsonToPb_ErrorShape(t *testing.T) {
	in := `{"action":"invoke","status":"FAILED","error":{"number":"400","reason":"bad_request","description":"bad path"},"ts":"2026-01-01T00:00:00Z"}`
	got := InvokeResponseJsonToPb(in)
	if got.GetStatus() != pb.ServiceStatus_FAILED || got.GetError() == nil || got.GetError().GetNumber() != "400" {
		t.Fatalf("got %+v", got)
	}
}

func TestInvokeResponsePbToJson_RoundTrip(t *testing.T) {
	pbResp := &pb.InvokeResponseMessage{Status: pb.ServiceStatus_SUCCESSFUL, RequestId: strPtr("8756"), Ts: "2026-01-01T00:00:00Z"}
	jsonOut := InvokeResponsePbToJson(pbResp)
	if !strings.Contains(jsonOut, `"action":"invoke"`) || !strings.Contains(jsonOut, `"status":"SUCCESSFUL"`) {
		t.Errorf("got %q", jsonOut)
	}
}

func TestInvokeStreamJsonToPb_ResponseVsEvent(t *testing.T) {
	resp := InvokeStreamJsonToPb(`{"action":"invoke","status":"ONGOING","requestId":"1","ts":"T","serviceId":"S1"}`)
	if resp.GetMType() != pb.ServiceStreamType_SERVICE_RESPONSE || resp.GetResponse() == nil {
		t.Fatalf("expected SERVICE_RESPONSE; got %+v", resp)
	}
	event := InvokeStreamJsonToPb(`{"action":"monitoring","path":"P","serviceId":"S1","status":"SUCCESSFUL","ts":"T"}`)
	if event.GetMType() != pb.ServiceStreamType_SERVICE_EVENT || event.GetEvent() == nil {
		t.Fatalf("expected SERVICE_EVENT; got %+v", event)
	}
}

func TestInvokeStreamPbToJson_RoundTrip(t *testing.T) {
	stream := &pb.InvokeStreamMessage{MType: pb.ServiceStreamType_SERVICE_EVENT, Payload: &pb.InvokeStreamMessage_Event{
		Event: &pb.MonitoringEventMessage{Path: "P", ServiceId: "S1", Status: pb.ServiceStatus_ONGOING, Ts: "T"},
	}}
	jsonOut := InvokeStreamPbToJson(stream)
	if !strings.Contains(jsonOut, `"action":"monitoring"`) || !strings.Contains(jsonOut, `"status":"ONGOING"`) {
		t.Errorf("got %q", jsonOut)
	}
}

// --------------------------------------------------------------------------
// monitor
// --------------------------------------------------------------------------

func TestMonitorRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"monitor","path":"VehicleService.Seating.MoveSeat","filter":{"variant":"all"},"requestId":"m1"}`
	got := MonitorRequestJsonToPb(in)
	if got.GetPath() != "VehicleService.Seating.MoveSeat" || got.GetRequestId() != "m1" {
		t.Fatalf("got %+v", got)
	}
}

func TestMonitorResponseJsonToPb_WithIndataAndOutdata(t *testing.T) {
	in := `{"action":"monitor","path":"P","status":"ONGOING","requestId":"m1","ts":"T","indata":{"input":{"a":"1"},"ts":"T"},"outdata":{"output":{"b":"2"},"ts":"T"},"serviceId":"S1"}`
	got := MonitorResponseJsonToPb(in)
	if got.GetIndata() == nil || got.GetIndata().GetInput().AsMap()["a"] != "1" {
		t.Errorf("indata: got %+v", got.GetIndata())
	}
	if got.GetOutdata() == nil || got.GetOutdata().GetSingle() == nil {
		t.Errorf("outdata: got %+v", got.GetOutdata())
	}
	if got.GetServiceId() != "S1" {
		t.Errorf("serviceId: got %q", got.GetServiceId())
	}
}

func TestMonitorResponsePbToJson_RoundTrip(t *testing.T) {
	pbResp := &pb.MonitorResponseMessage{Status: pb.ServiceStatus_UNKNOWN, RequestId: strPtr("m1"), Ts: "T"}
	jsonOut := MonitorResponsePbToJson(pbResp)
	if !strings.Contains(jsonOut, `"action":"monitor"`) || !strings.Contains(jsonOut, `"status":"UNKNOWN"`) {
		t.Errorf("got %q", jsonOut)
	}
}

func TestMonitorStreamJsonToPb_ResponseVsEvent(t *testing.T) {
	resp := MonitorStreamJsonToPb(`{"action":"monitor","status":"ONGOING","requestId":"1","ts":"T"}`)
	if resp.GetMType() != pb.ServiceStreamType_SERVICE_RESPONSE {
		t.Fatalf("expected SERVICE_RESPONSE; got %+v", resp)
	}
	event := MonitorStreamJsonToPb(`{"action":"monitoring","path":"P","serviceId":"S1","status":"ONGOING","ts":"T","progress":42}`)
	if event.GetMType() != pb.ServiceStreamType_SERVICE_EVENT || event.GetEvent().GetProgress() != 42 {
		t.Fatalf("expected SERVICE_EVENT with progress=42; got %+v", event)
	}
}

func TestMonitorRequestPbToJson_FilterRoundTrip(t *testing.T) {
	pbReq := &pb.MonitorRequestMessage{Path: "P", RequestId: "m1", Filter: []*pb.ServiceFilter{
		{Variant: pb.ServiceFilterVariant_RESOURCE, ResourcePath: []string{"Row1"}},
		{Variant: pb.ServiceFilterVariant_STATUS},
	}}
	jsonOut := MonitorRequestPbToJson(pbReq)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %s, err=%s", jsonOut, err)
	}
	arr, ok := m["filter"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("got %+v", m["filter"])
	}
}

// --------------------------------------------------------------------------
// cancel
// --------------------------------------------------------------------------

func TestCancelRequestJsonToPb_HappyPath(t *testing.T) {
	got := CancelRequestJsonToPb(`{"action":"cancel","serviceId":"S1"}`)
	if got.GetServiceId() != "S1" {
		t.Fatalf("got %+v", got)
	}
}

func TestCancelRequestPbToJson_RoundTrip(t *testing.T) {
	jsonOut := CancelRequestPbToJson(&pb.CancelRequestMessage{ServiceId: "S1"})
	if !strings.Contains(jsonOut, `"action":"cancel"`) || !strings.Contains(jsonOut, `"serviceId":"S1"`) {
		t.Errorf("got %q", jsonOut)
	}
}

func TestCancelResponseJsonToPb_WithOutdata(t *testing.T) {
	in := `{"action":"cancel","status":"CANCELED","serviceId":"S1","ts":"T","outdata":{"output":{"a":"1"},"ts":"T"}}`
	got := CancelResponseJsonToPb(in)
	if got.GetStatus() != pb.ServiceStatus_CANCELED || got.GetServiceId() != "S1" || got.GetOutdata() == nil {
		t.Fatalf("got %+v", got)
	}
}

func TestCancelResponsePbToJson_ErrorShape(t *testing.T) {
	pbResp := &pb.CancelResponseMessage{
		Status:    pb.ServiceStatus_FAILED,
		ServiceId: "S1",
		Ts:        "T",
		Error:     &pb.ServiceError{Number: strPtr("400"), Reason: strPtr("bad_request"), Description: strPtr("serviceId not found")},
	}
	jsonOut := CancelResponsePbToJson(pbResp)
	if !strings.Contains(jsonOut, `"error":{"number":"400"`) {
		t.Errorf("got %q", jsonOut)
	}
}

// --------------------------------------------------------------------------
// discover
// --------------------------------------------------------------------------

func TestDiscoverRequestJsonToPb_HappyPath(t *testing.T) {
	got := DiscoverRequestJsonToPb(`{"action":"discover","path":"VehicleService.Seating","depth":"0","requestId":"5687"}`)
	if got.GetPath() != "VehicleService.Seating" || got.GetDepth() != "0" || got.GetRequestId() != "5687" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiscoverRequestJsonToPb_BadJSON(t *testing.T) {
	if got := DiscoverRequestJsonToPb("not json"); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
}

func TestDiscoverRequestPbToJson_RoundTrip(t *testing.T) {
	jsonOut := DiscoverRequestPbToJson(&pb.DiscoverRequestMessage{Path: "VehicleService", Depth: "1", RequestId: "1"})
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %s, err=%s", jsonOut, err)
	}
	if m["action"] != "discover" || m["depth"] != "1" {
		t.Errorf("got %+v", m)
	}
}

func TestDiscoverResponseJsonToPb_WithMetadata(t *testing.T) {
	in := `{"action":"discover","metadata":{"MoveSeat":{"type":"procedure"}},"requestId":"1","ts":"T"}`
	got := DiscoverResponseJsonToPb(in)
	if got.GetMetadata() == nil {
		t.Fatalf("got %+v", got)
	}
	moveSeat, ok := got.GetMetadata().AsMap()["MoveSeat"].(map[string]interface{})
	if !ok || moveSeat["type"] != "procedure" {
		t.Errorf("metadata: got %+v", got.GetMetadata().AsMap())
	}
}

func TestDiscoverResponsePbToJson_RoundTrip(t *testing.T) {
	st, err := structpb.NewStruct(map[string]interface{}{"foo": "bar"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %s", err)
	}
	pbResp := &pb.DiscoverResponseMessage{Metadata: st, RequestId: "1", Ts: "T"}
	jsonOut := DiscoverResponsePbToJson(pbResp)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &m); err != nil {
		t.Fatalf("invalid JSON: %s, err=%s", jsonOut, err)
	}
	meta, ok := m["metadata"].(map[string]interface{})
	if !ok || meta["foo"] != "bar" {
		t.Errorf("got %+v", m)
	}
}

func TestDiscoverResponsePbToJson_ErrorShape(t *testing.T) {
	pbResp := &pb.DiscoverResponseMessage{
		Error:     &pb.ServiceError{Number: strPtr("400"), Reason: strPtr("bad_request"), Description: strPtr("path not found")},
		RequestId: "1",
		Ts:        "T",
	}
	jsonOut := DiscoverResponsePbToJson(pbResp)
	if !strings.Contains(jsonOut, `"error":{"number":"400"`) {
		t.Errorf("got %q", jsonOut)
	}
}
