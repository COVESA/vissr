/**
* (C) 2026 Matt Jones / Ford
*
* Tests for grcputils.go (JSON <-> protobuf converter for the gRPC
* transport). Covers every unit-testable function plus the defensive
* helpers added in this branch. Each malformed-input case pins a fix.
**/

package utils

import (
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/covesa/vissr/grpc_pb"
)

func init() {
	InitLog("grcputils_test-log.txt", "./logs", false, "info")
}

// --------------------------------------------------------------------------
// Defensive helpers
// --------------------------------------------------------------------------

func TestMapString(t *testing.T) {
	m := map[string]interface{}{
		"a": "hello",
		"b": 42,
		"c": nil,
	}
	if v, ok := mapString(m, "a"); !ok || v != "hello" {
		t.Errorf("a: got %q,%v", v, ok)
	}
	if _, ok := mapString(m, "b"); ok {
		t.Errorf("non-string returned ok")
	}
	if _, ok := mapString(m, "c"); ok {
		t.Errorf("nil returned ok")
	}
	if _, ok := mapString(m, "missing"); ok {
		t.Errorf("missing returned ok")
	}
}

func TestMapStringOrEmpty(t *testing.T) {
	m := map[string]interface{}{"a": "hi"}
	if got := mapStringOrEmpty(m, "a"); got != "hi" {
		t.Errorf("got %q", got)
	}
	if got := mapStringOrEmpty(m, "missing"); got != "" {
		t.Errorf("missing should be empty; got %q", got)
	}
}

func TestMapAsMap(t *testing.T) {
	m := map[string]interface{}{
		"a": map[string]interface{}{"x": "y"},
		"b": "not a map",
	}
	if inner, ok := mapAsMap(m, "a"); !ok || inner["x"] != "y" {
		t.Errorf("got %+v ok=%v", inner, ok)
	}
	if _, ok := mapAsMap(m, "b"); ok {
		t.Errorf("string returned ok")
	}
	if _, ok := mapAsMap(m, "missing"); ok {
		t.Errorf("missing returned ok")
	}
}

func TestAsMapAsString(t *testing.T) {
	if _, ok := asMap(nil); ok {
		t.Errorf("nil should return false")
	}
	if _, ok := asMap("string"); ok {
		t.Errorf("string should return false")
	}
	if _, ok := asMap(map[string]interface{}{}); !ok {
		t.Errorf("empty map should return ok")
	}
	if _, ok := asString(nil); ok {
		t.Errorf("nil should return false")
	}
	if _, ok := asString(42); ok {
		t.Errorf("int should return false")
	}
	if s, ok := asString("hi"); !ok || s != "hi" {
		t.Errorf("got %q,%v", s, ok)
	}
}

func TestTrimTrailingComma(t *testing.T) {
	if got := trimTrailingComma(""); got != "" {
		t.Errorf("empty should stay empty; got %q", got)
	}
	if got := trimTrailingComma("a,"); got != "a" {
		t.Errorf("got %q; want a", got)
	}
	if got := trimTrailingComma("a"); got != "a" {
		t.Errorf("got %q; want a (no trim needed)", got)
	}
	if got := trimTrailingComma(","); got != "" {
		t.Errorf("got %q; want empty", got)
	}
}

// --------------------------------------------------------------------------
// jsonEscape
// --------------------------------------------------------------------------

func TestJsonEscape(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"hello":         "hello",
		`with "quote"`:  `with \"quote\"`,
		`back\slash`:    `back\\slash`,
		"with\nnewl":    `with\nnewl`,
		"with\ttab":     `with\ttab`,
		"with\rret":     `with\rret`,
		"\x01control":   `\u0001control`,
	}
	for in, want := range cases {
		if got := jsonEscape(in); got != want {
			t.Errorf("jsonEscape(%q) = %q; want %q", in, got, want)
		}
	}
}

// --------------------------------------------------------------------------
// ExtractSubscriptionId
// --------------------------------------------------------------------------

func TestExtractSubscriptionId_HappyPath(t *testing.T) {
	if got := ExtractSubscriptionId(`{"subscriptionId":"123"}`); got != "123" {
		t.Errorf("got %q", got)
	}
}

func TestExtractSubscriptionId_MissingFieldDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("missing field panicked: %v", r)
		}
	}()
	if got := ExtractSubscriptionId(`{}`); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestExtractSubscriptionId_NonStringDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := ExtractSubscriptionId(`{"subscriptionId":42}`); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestExtractSubscriptionId_BadJSON(t *testing.T) {
	if got := ExtractSubscriptionId(`not json`); got != "" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// GetRequestJsonToPb / SetRequestJsonToPb / SubscribeRequestJsonToPb /
// SubscribeStreamJsonToPb / UnsubscribeRequestJsonToPb /
// UnsubscribeResponseJsonToPb / SetResponseJsonToPb / GetResponseJsonToPb
// — all top-level entry points must tolerate malformed JSON
// --------------------------------------------------------------------------

func TestGetRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"get","path":"Vehicle.Speed","requestId":"42"}`
	got := GetRequestJsonToPb(in)
	if got == nil || got.GetPath() != "Vehicle.Speed" || got.GetRequestId() != "42" {
		t.Errorf("got %+v", got)
	}
}

func TestGetRequestJsonToPb_BadJSON(t *testing.T) {
	if got := GetRequestJsonToPb("not json"); got != nil {
		t.Errorf("expected nil for bad JSON; got %+v", got)
	}
}

func TestGetRequestJsonToPb_MissingFieldsDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("missing fields panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"path":42}`,
		`{"path":"X","requestId":42}`,
		`{"path":"X","filter":42}`,
		`{"path":"X","filter":["only-one-element"]}`,
		`{"path":"X","authorization":42}`,
	}
	for _, in := range cases {
		got := GetRequestJsonToPb(in)
		if got == nil {
			t.Errorf("input %q produced nil — should produce empty/partial proto", in)
		}
	}
}

func TestGetResponseJsonToPb_MalformedDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"requestId":42}`,
		`{"error":"not a map"}`,
		`{"error":42}`,
		`{"data":"not an array"}`,
	}
	for _, in := range cases {
		_ = GetResponseJsonToPb(in)
	}
}

func TestSetRequestJsonToPb_HappyPath(t *testing.T) {
	in := `{"action":"set","path":"Vehicle.Speed","value":"42","requestId":"1"}`
	got := SetRequestJsonToPb(in)
	if got.GetPath() != "Vehicle.Speed" || got.GetValue() != "42" {
		t.Errorf("got %+v", got)
	}
}

func TestSetRequestJsonToPb_MissingFieldsDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := SetRequestJsonToPb(`{}`); got == nil {
		t.Errorf("expected non-nil partial proto")
	}
}

func TestSubscribeRequestJsonToPb_MissingFieldsDoNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	_ = SubscribeRequestJsonToPb(`{}`)
	_ = SubscribeRequestJsonToPb(`{"filter":{"variant":42}}`)
	_ = SubscribeRequestJsonToPb(`{"filter":{"variant":"paths"}}`)
}

func TestSubscribeStreamJsonToPb_MalformedDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	cases := []string{
		`{}`,
		`{"action":"subscribe"}`,
		`{"action":"subscribe","error":"oops"}`,
		`{"action":"subscription"}`,
		`{"action":"subscription","error":{}}`,
		`{"action":"subscription","data":"not-an-array"}`,
	}
	for _, in := range cases {
		_ = SubscribeStreamJsonToPb(in)
	}
}

func TestUnsubscribeRequestJsonToPb_MalformedDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	_ = UnsubscribeRequestJsonToPb(`{}`)
}

func TestUnsubscribeResponseJsonToPb_MalformedDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	_ = UnsubscribeResponseJsonToPb(`{}`)
	_ = UnsubscribeResponseJsonToPb(`{"error":"not a map"}`)
}

func TestSetResponseJsonToPb_ErrorBranch(t *testing.T) {
	in := `{"requestId":"1","ts":"2026-05-17T12:00:00Z","error":{"number":"404","reason":"not found"}}`
	got := SetResponseJsonToPb(in)
	if got.GetStatus() != pb.ResponseStatus_ERROR {
		t.Errorf("expected ERROR status; got %v", got.GetStatus())
	}
	if got.GetErrorResponse().GetNumber() != "404" {
		t.Errorf("expected number 404; got %q", got.GetErrorResponse().GetNumber())
	}
}

// --------------------------------------------------------------------------
// getNumOfDataElements / getNumOfDataPointElements / getNumOfRangeExpressions
// --------------------------------------------------------------------------

func TestGetNumOfDataElements(t *testing.T) {
	if got := getNumOfDataElements(nil); got != 0 {
		t.Errorf("nil should be 0; got %d", got)
	}
	if got := getNumOfDataElements(map[string]interface{}{}); got != 1 {
		t.Errorf("single object should be 1; got %d", got)
	}
	if got := getNumOfDataElements([]interface{}{1, 2, 3}); got != 3 {
		t.Errorf("array should be 3; got %d", got)
	}
}

func TestGetNumOfRangeExpressions(t *testing.T) {
	if got := getNumOfRangeExpressions(map[string]interface{}{}); got != 1 {
		t.Errorf("single object should be 1; got %d", got)
	}
	if got := getNumOfRangeExpressions([]interface{}{1, 2}); got != 2 {
		t.Errorf("array should be 2; got %d", got)
	}
}

// --------------------------------------------------------------------------
// createDataElement / createDataPointElement — defensive on malformed input
// --------------------------------------------------------------------------

func TestCreateDataElement_NonMapReturnsNil(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := createDataElement(0, "not a map"); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
}

func TestCreateDataElement_OOBIndexReturnsNil(t *testing.T) {
	arr := []interface{}{map[string]interface{}{"path": "X"}}
	if got := createDataElement(99, arr); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
}

func TestCreateDataElement_NonMapArrayElement(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	arr := []interface{}{"not-an-object", 42}
	if got := createDataElement(0, arr); got != nil {
		t.Errorf("expected nil for non-map element; got %+v", got)
	}
}

func TestCreateDataElement_HappyPath(t *testing.T) {
	in := map[string]interface{}{
		"path": "Vehicle.Speed",
		"dp":   map[string]interface{}{"value": "42", "ts": "2026-05-17T12:00:00Z"},
	}
	got := createDataElement(0, in)
	if got == nil || got.GetPath() != "Vehicle.Speed" {
		t.Fatalf("got %+v", got)
	}
	if len(got.GetDp()) != 1 || got.GetDp()[0].GetValue() != "42" {
		t.Errorf("dp wrong: %+v", got.GetDp())
	}
}

func TestCreateDataPointElement_NonMapReturnsNil(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := createDataPointElement(0, "oops"); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
}

// --------------------------------------------------------------------------
// createPbFilter / getFilterVariant / per-variant getters
// --------------------------------------------------------------------------

func TestGetFilterVariant(t *testing.T) {
	cases := map[string]pb.FilterExpressions_FilterExpression_FilterVariant{
		"paths":     pb.FilterExpressions_FilterExpression_PATHS,
		"timebased": pb.FilterExpressions_FilterExpression_TIMEBASED,
		"range":     pb.FilterExpressions_FilterExpression_RANGE,
		"change":    pb.FilterExpressions_FilterExpression_CHANGE,
		"curvelog":  pb.FilterExpressions_FilterExpression_CURVELOG,
		"history":   pb.FilterExpressions_FilterExpression_HISTORY,
		"metadata":  pb.FilterExpressions_FilterExpression_METADATA,
	}
	for in, want := range cases {
		if got := getFilterVariant(in); got != want {
			t.Errorf("%q: got %v; want %v", in, got, want)
		}
	}
	// Unknown variant returns the sentinel
	if got := getFilterVariant("unknown"); got == pb.FilterExpressions_FilterExpression_PATHS {
		t.Errorf("unknown should not collide with PATHS")
	}
}

func TestCreatePbFilter_MissingVariantNoop(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	createPbFilter(0, map[string]interface{}{}, out)
}

func TestCreatePbFilter_PathsVariant(t *testing.T) {
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	in := map[string]interface{}{
		"variant":   "paths",
		"parameter": []interface{}{"Vehicle.Speed", "Vehicle.Direction"},
	}
	createPbFilter(0, in, out)
	if got := out.FilterExp[0].GetVariant(); got != pb.FilterExpressions_FilterExpression_PATHS {
		t.Errorf("variant: got %v", got)
	}
	if paths := out.FilterExp[0].GetValue().GetValuePaths().GetRelativePath(); len(paths) != 2 {
		t.Errorf("paths: got %v", paths)
	}
}

func TestCreatePbFilter_RangeVariant(t *testing.T) {
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	in := map[string]interface{}{
		"variant": "range",
		"parameter": map[string]interface{}{
			"logic-op": "lt",
			"boundary": "100",
		},
	}
	createPbFilter(0, in, out)
	rv := out.FilterExp[0].GetValue().GetValueRange()
	if len(rv) != 1 || rv[0].GetLogicOperator() != "lt" || rv[0].GetBoundary() != "100" {
		t.Errorf("range: got %+v", rv)
	}
}

func TestCreatePbFilter_ChangeVariant(t *testing.T) {
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	in := map[string]interface{}{
		"variant":   "change",
		"parameter": map[string]interface{}{"logic-op": "eq", "diff": "5"},
	}
	createPbFilter(0, in, out)
	cv := out.FilterExp[0].GetValue().GetValueChange()
	if cv.GetLogicOperator() != "eq" || cv.GetDiff() != "5" {
		t.Errorf("change: got %+v", cv)
	}
}

func TestCreatePbFilter_CurvelogVariant(t *testing.T) {
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	in := map[string]interface{}{
		"variant":   "curvelog",
		"parameter": map[string]interface{}{"maxerr": "0.1", "bufsize": "100"},
	}
	createPbFilter(0, in, out)
	cv := out.FilterExp[0].GetValue().GetValueCurvelog()
	if cv.GetMaxErr() != "0.1" || cv.GetBufSize() != "100" {
		t.Errorf("curvelog: got %+v", cv)
	}
}

func TestCreatePbFilter_HistoryVariant(t *testing.T) {
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	in := map[string]interface{}{"variant": "history", "parameter": "P1D"}
	createPbFilter(0, in, out)
	if out.FilterExp[0].GetValue().GetValueHistory().GetTimePeriod() != "P1D" {
		t.Errorf("history: got %+v", out.FilterExp[0])
	}
}

func TestCreatePbFilter_TimebasedVariant(t *testing.T) {
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	in := map[string]interface{}{"variant": "timebased", "parameter": map[string]interface{}{"period": "P1S"}}
	createPbFilter(0, in, out)
	if out.FilterExp[0].GetValue().GetValueTimebased().GetPeriod() != "P1S" {
		t.Errorf("timebased: got %+v", out.FilterExp[0])
	}
}

func TestCreatePbFilter_BadParameterShape(t *testing.T) {
	// Wrong parameter type for variant (string when map expected, etc.)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on bad parameter: %v", r)
		}
	}()
	out := &pb.FilterExpressions{FilterExp: []*pb.FilterExpressions_FilterExpression{{}}}
	createPbFilter(0, map[string]interface{}{"variant": "change", "parameter": "not-a-map"}, out)
	createPbFilter(0, map[string]interface{}{"variant": "range", "parameter": []interface{}{"not-a-map"}}, out)
}

// --------------------------------------------------------------------------
// getPbPathsFilterValue / getPbRangeFilterValue — non-string array elements
// --------------------------------------------------------------------------

func TestGetPbPathsFilterValue_StringValue(t *testing.T) {
	got := getPbPathsFilterValue("Vehicle.Speed")
	if len(got.GetRelativePath()) != 1 || got.GetRelativePath()[0] != "Vehicle.Speed" {
		t.Errorf("got %+v", got)
	}
}

func TestGetPbPathsFilterValue_NonStringElementSkipped(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	got := getPbPathsFilterValue([]interface{}{"a", 42, "b"})
	if len(got.GetRelativePath()) != 2 {
		t.Errorf("expected 2 valid paths; got %v", got.GetRelativePath())
	}
}

func TestGetPbRangeFilterValue_BadInputReturnsNil(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := getPbRangeFilterValue(0, "not-a-shape"); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
	if got := getPbRangeFilterValue(99, []interface{}{map[string]interface{}{"boundary": "1"}}); got != nil {
		t.Errorf("OOB index should return nil; got %+v", got)
	}
}

// --------------------------------------------------------------------------
// getProtoErrorMessage
// --------------------------------------------------------------------------

func TestGetProtoErrorMessage_AllFields(t *testing.T) {
	in := map[string]interface{}{
		"number":      "404",
		"reason":      "not found",
		"description": "the resource is missing",
	}
	got := getProtoErrorMessage(in)
	if got.GetNumber() != "404" || got.GetReason() != "not found" || got.GetDescription() != "the resource is missing" {
		t.Errorf("got %+v", got)
	}
}

func TestGetProtoErrorMessage_NonStringFieldsSkipped(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	in := map[string]interface{}{
		"number":      42,
		"reason":      "ok",
		"description": map[string]interface{}{},
	}
	got := getProtoErrorMessage(in)
	if got.GetNumber() != "" || got.GetDescription() != "" {
		t.Errorf("non-strings should be empty; got %+v", got)
	}
	if got.GetReason() != "ok" {
		t.Errorf("valid field dropped; got %q", got.GetReason())
	}
}

func TestGetProtoErrorMessage_NilInput(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil map panicked: %v", r)
		}
	}()
	got := getProtoErrorMessage(nil)
	if got == nil {
		t.Errorf("expected non-nil ErrorResponseMessage")
	}
}

// --------------------------------------------------------------------------
// Proto-to-JSON converters
// --------------------------------------------------------------------------

func TestGetRequestPbToJson_RoundTrip(t *testing.T) {
	in := &pb.GetRequestMessage{Path: "Vehicle.Speed", RequestId: "42"}
	out := GetRequestPbToJson(in)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output not valid JSON: %v; raw=%q", err, out)
	}
	if m["path"] != "Vehicle.Speed" || m["requestId"] != "42" {
		t.Errorf("got %+v", m)
	}
}

func TestGetResponsePbToJson_EmptyDataPackDoesNotPanic(t *testing.T) {
	// Pre-fix createJsonData panicked on empty dataPack.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on empty data: %v", r)
		}
	}()
	in := &pb.GetResponseMessage{
		Status:          pb.ResponseStatus_SUCCESS,
		SuccessResponse: &pb.GetResponseMessage_SuccessResponseMessage{DataPack: &pb.DataPackages{}},
		Ts:              "2026-05-17T12:00:00Z",
		RequestId:       "1",
	}
	out := GetResponsePbToJson(in)
	// Output must at least be valid JSON
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Errorf("output not valid JSON: %v; raw=%q", err, out)
	}
}

func TestGetResponsePbToJson_NilSuccessResponseDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on nil SuccessResponse: %v", r)
		}
	}()
	in := &pb.GetResponseMessage{
		Status:    pb.ResponseStatus_SUCCESS,
		Ts:        "ts",
		RequestId: "1",
	}
	out := GetResponsePbToJson(in)
	if !strings.Contains(out, `"action":"get"`) {
		t.Errorf("expected action=get; got %q", out)
	}
}

func TestGetResponsePbToJson_ErrorBranch(t *testing.T) {
	in := &pb.GetResponseMessage{
		Status:        pb.ResponseStatus_ERROR,
		ErrorResponse: &pb.ErrorResponseMessage{Number: "404", Reason: "not found"},
		Ts:            "ts",
		RequestId:     "1",
	}
	out := GetResponsePbToJson(in)
	if !strings.Contains(out, `"number":"404"`) {
		t.Errorf("missing error number; got %q", out)
	}
}

func TestSetRequestPbToJson(t *testing.T) {
	in := &pb.SetRequestMessage{Path: "P", Value: "V", RequestId: "1"}
	out := SetRequestPbToJson(in)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("invalid JSON: %v; raw=%q", err, out)
	}
}

func TestSetResponsePbToJson_RoundTrip(t *testing.T) {
	in := &pb.SetResponseMessage{Status: pb.ResponseStatus_SUCCESS, Ts: "ts", RequestId: "1"}
	out := SetResponsePbToJson(in)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Errorf("invalid JSON: %v; raw=%q", err, out)
	}
}

func TestSubscribeRequestPbToJson(t *testing.T) {
	in := &pb.SubscribeRequestMessage{Path: "P", RequestId: "1"}
	out := SubscribeRequestPbToJson(in)
	if !strings.Contains(out, `"action":"subscribe"`) {
		t.Errorf("got %q", out)
	}
}

func TestSubscribeStreamPbToJson_EmptyEventDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on empty event: %v", r)
		}
	}()
	in := &pb.SubscribeStreamMessage{
		MType:  pb.SubscribeResponseType_SUB_EVENT,
		Status: pb.ResponseStatus_SUCCESS,
		Event:  &pb.SubscribeStreamMessage_SubscribeEventMessage{},
	}
	_ = SubscribeStreamPbToJson(in)
}

func TestSubscribeStreamPbToJson_NilResponseDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on nil response: %v", r)
		}
	}()
	in := &pb.SubscribeStreamMessage{
		MType:  pb.SubscribeResponseType_SUB_RESPONSE,
		Status: pb.ResponseStatus_SUCCESS,
	}
	_ = SubscribeStreamPbToJson(in)
}

func TestUnsubscribeRequestPbToJson(t *testing.T) {
	in := &pb.UnsubscribeRequestMessage{SubscriptionId: "S", RequestId: "1"}
	out := UnsubscribeRequestPbToJson(in)
	if !strings.Contains(out, `"action":"unsubscribe"`) {
		t.Errorf("got %q", out)
	}
}

func TestUnsubscribeResponsePbToJson_HappyPath(t *testing.T) {
	in := &pb.UnsubscribeResponseMessage{Status: pb.ResponseStatus_SUCCESS, RequestId: "1", Ts: "ts"}
	out := UnsubscribeResponsePbToJson(in)
	if !strings.Contains(out, `"action":"unsubscribe"`) {
		t.Errorf("got %q", out)
	}
}

// --------------------------------------------------------------------------
// Filter Proto-to-JSON: empty inputs no longer panic
// --------------------------------------------------------------------------

func TestGetJsonFilter_NilOrEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := getJsonFilter(nil); got != "" {
		t.Errorf("nil filter should produce empty; got %q", got)
	}
	if got := getJsonFilter(&pb.FilterExpressions{}); got != "" {
		t.Errorf("empty filter should produce empty; got %q", got)
	}
}

func TestGetJsonFilterValuePaths_Empty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{
			ValuePaths: &pb.FilterExpressions_FilterExpression_FilterValue_PathsValue{},
		},
	}
	// Pre-fix this panicked on empty relative-paths.
	_ = getJsonFilterValuePaths(in)
}

func TestGetJsonFilterValueRange_Empty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{},
	}
	_ = getJsonFilterValueRange(in)
}

// --------------------------------------------------------------------------
// createJsonData / getJsonDp / getJsonError defensive
// --------------------------------------------------------------------------

func TestCreateJsonData_Empty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := createJsonData(nil); got != "" {
		t.Errorf("expected empty for nil; got %q", got)
	}
	if got := createJsonData([]*pb.DataPackages_DataPackage{}); got != "" {
		t.Errorf("expected empty for empty slice; got %q", got)
	}
}

func TestGetJsonDp_Empty(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	if got := getJsonDp(nil); got != `{}` {
		t.Errorf("expected '{}' for nil; got %q", got)
	}
	if got := getJsonDp(&pb.DataPackages_DataPackage{}); got != `{}` {
		t.Errorf("expected '{}' for empty dp; got %q", got)
	}
}

func TestGetJsonError_NilReceiver(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on nil receiver: %v", r)
		}
	}()
	got := getJsonError(nil)
	if !strings.Contains(got, `"error":`) {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// JSON-escape correctness end-to-end
// --------------------------------------------------------------------------

func TestCreateJsonData_EscapesQuotesAndBackslashes(t *testing.T) {
	in := []*pb.DataPackages_DataPackage{
		{
			Path: `weird"path\with"chars`,
			Dp: []*pb.DataPackages_DataPackage_DataPoint{
				{Value: `value"with"quotes`, Ts: "2026-05-17T12:00:00Z"},
			},
		},
	}
	got := createJsonData(in)
	// The wrapper return is `,"data":...` so wrap into an object to parse.
	wrapped := "{" + strings.TrimPrefix(got, ",") + "}"
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(wrapped), &m); err != nil {
		t.Errorf("output not valid JSON after escaping: %v; raw=%q", err, got)
	}
}

// --------------------------------------------------------------------------
// applyFilterFromMessage — array/map/missing/invalid
// --------------------------------------------------------------------------

func TestApplyFilterFromMessage_MissingReturnsNil(t *testing.T) {
	if got := applyFilterFromMessage(map[string]interface{}{}); got != nil {
		t.Errorf("expected nil; got %+v", got)
	}
}

func TestApplyFilterFromMessage_NonMapElementInArrayRejected(t *testing.T) {
	in := map[string]interface{}{
		"filter": []interface{}{"not-a-map", map[string]interface{}{"variant": "paths"}},
	}
	if got := applyFilterFromMessage(in); got != nil {
		t.Errorf("expected nil for non-map array element; got %+v", got)
	}
}

func TestApplyFilterFromMessage_WrongArrayLength(t *testing.T) {
	in := map[string]interface{}{
		"filter": []interface{}{map[string]interface{}{"variant": "paths"}}, // only 1 element
	}
	if got := applyFilterFromMessage(in); got != nil {
		t.Errorf("expected nil for wrong array length; got %+v", got)
	}
}

func TestApplyFilterFromMessage_SingleObject(t *testing.T) {
	in := map[string]interface{}{
		"filter": map[string]interface{}{
			"variant":   "paths",
			"parameter": "Vehicle.Speed",
		},
	}
	got := applyFilterFromMessage(in)
	if got == nil || len(got.FilterExp) != 1 {
		t.Errorf("got %+v", got)
	}
}

// --------------------------------------------------------------------------
// Pb-to-JSON filter helpers (synthesizeFilter dispatch + per-variant)
// --------------------------------------------------------------------------

func TestSynthesizeFilter_AllVariants(t *testing.T) {
	cases := []struct {
		variant pb.FilterExpressions_FilterExpression_FilterVariant
		want    string
	}{
		{pb.FilterExpressions_FilterExpression_PATHS, `"paths"`},
		{pb.FilterExpressions_FilterExpression_TIMEBASED, `"timebased"`},
		{pb.FilterExpressions_FilterExpression_RANGE, `"range"`},
		{pb.FilterExpressions_FilterExpression_CHANGE, `"change"`},
		{pb.FilterExpressions_FilterExpression_CURVELOG, `"curvelog"`},
		{pb.FilterExpressions_FilterExpression_HISTORY, `"history"`},
		{pb.FilterExpressions_FilterExpression_METADATA, `"metadata"`},
	}
	for _, c := range cases {
		in := &pb.FilterExpressions_FilterExpression{
			Variant: c.variant,
			Value:   &pb.FilterExpressions_FilterExpression_FilterValue{},
		}
		got := synthesizeFilter(in)
		if !strings.Contains(got, c.want) {
			t.Errorf("variant %v: missing %s; got %q", c.variant, c.want, got)
		}
	}
}

func TestGetJsonFilterValueTimebased(t *testing.T) {
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{
			ValueTimebased: &pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue{Period: "PT1S"},
		},
	}
	if got := getJsonFilterValueTimebased(in); !strings.Contains(got, `"period":"PT1S"`) {
		t.Errorf("got %q", got)
	}
}

func TestGetJsonFilterValueChange(t *testing.T) {
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{
			ValueChange: &pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue{
				LogicOperator: "lt",
				Diff:          "5",
			},
		},
	}
	got := getJsonFilterValueChange(in)
	if !strings.Contains(got, `"logic-op":"lt"`) || !strings.Contains(got, `"diff":"5"`) {
		t.Errorf("got %q", got)
	}
}

func TestGetJsonFilterValueCurvelog(t *testing.T) {
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{
			ValueCurvelog: &pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue{
				MaxErr:  "0.1",
				BufSize: "100",
			},
		},
	}
	got := getJsonFilterValueCurvelog(in)
	if !strings.Contains(got, `"maxerr":"0.1"`) || !strings.Contains(got, `"bufsize":"100"`) {
		t.Errorf("got %q", got)
	}
}

func TestGetJsonFilterValueHistory(t *testing.T) {
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{
			ValueHistory: &pb.FilterExpressions_FilterExpression_FilterValue_HistoryValue{TimePeriod: "P1D"},
		},
	}
	if got := getJsonFilterValueHistory(in); got != `"P1D"` {
		t.Errorf("got %q", got)
	}
}

func TestGetJsonFilterValueMetadata(t *testing.T) {
	in := &pb.FilterExpressions_FilterExpression{
		Value: &pb.FilterExpressions_FilterExpression_FilterValue{
			ValueMetadata: &pb.FilterExpressions_FilterExpression_FilterValue_MetadataValue{Tree: "raw-tree-content"},
		},
	}
	if got := getJsonFilterValueMetadata(in); got != "raw-tree-content" {
		t.Errorf("got %q", got)
	}
}

// --------------------------------------------------------------------------
// createJSON helper - key formatting
// --------------------------------------------------------------------------

func TestCreateJSON(t *testing.T) {
	if got := createJSON("hello", "myKey"); got != `,"myKey":"hello"` {
		t.Errorf("got %q", got)
	}
	if got := createJSON("", "myKey"); got != "" {
		t.Errorf("empty value should produce empty; got %q", got)
	}
}

// --------------------------------------------------------------------------
// needsEscape predicate
// --------------------------------------------------------------------------

func TestNeedsEscape(t *testing.T) {
	if needsEscape("hello") {
		t.Errorf("plain ASCII should not need escape")
	}
	if !needsEscape(`with "quote"`) {
		t.Errorf("quote should need escape")
	}
	if !needsEscape("with\nnewl") {
		t.Errorf("newline should need escape")
	}
	if !needsEscape("\x01") {
		t.Errorf("control char should need escape")
	}
	if !needsEscape(`back\slash`) {
		t.Errorf("backslash should need escape")
	}
}

// --------------------------------------------------------------------------
// Full pb->JSON roundtrip with non-trivial filter
// --------------------------------------------------------------------------

func TestSubscribeRequestPbToJson_WithRangeFilter(t *testing.T) {
	in := &pb.SubscribeRequestMessage{
		Path:      "Vehicle.Speed",
		RequestId: "1",
		Filter: &pb.FilterExpressions{
			FilterExp: []*pb.FilterExpressions_FilterExpression{
				{
					Variant: pb.FilterExpressions_FilterExpression_RANGE,
					Value: &pb.FilterExpressions_FilterExpression_FilterValue{
						ValueRange: []*pb.FilterExpressions_FilterExpression_FilterValue_RangeValue{
							{LogicOperator: "lt", Boundary: "100"},
						},
					},
				},
			},
		},
	}
	out := SubscribeRequestPbToJson(in)
	// must contain the filter and be valid JSON
	if !strings.Contains(out, `"variant":"range"`) {
		t.Errorf("missing filter variant; got %q", out)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Errorf("output not valid JSON: %v; raw=%q", err, out)
	}
}
