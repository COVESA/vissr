/**
* (C) 2023 Ford Motor Company
* (C) 2021 Geotab
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package utils

import (
	"encoding/json"
	"strconv"
	"strings"

	pb "github.com/covesa/vissr/grpc_pb"
	"google.golang.org/protobuf/types/known/structpb"
)

// -- Defensive helpers --------------------------------------------------
// Every JSON map access in this file flows through these so a malformed
// request never panics the gRPC server. Pre-fix, ~60 bare type assertions
// (e.g. `messageMap["path"].(string)`) panicked on any missing or
// wrong-typed field, turning malformed input into a crash-DoS.

// mapString returns m[key] as a string. Returns "", false when the key
// is absent, nil, or the wrong type.
func mapString(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// mapStringOrEmpty is the convenience form for fields where a missing
// value is equivalent to "".
func mapStringOrEmpty(m map[string]interface{}, key string) string {
	s, _ := mapString(m, key)
	return s
}

// mapAsMap returns m[key] as a nested map; ok=false on missing or wrong type.
func mapAsMap(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return nil, false
	}
	mm, ok := v.(map[string]interface{})
	return mm, ok
}

// asMap converts an interface to a map[string]interface{}, ok=false if
// it's a different type.
func asMap(v interface{}) (map[string]interface{}, bool) {
	if v == nil {
		return nil, false
	}
	m, ok := v.(map[string]interface{})
	return m, ok
}

// asString converts an interface to a string, ok=false otherwise.
func asString(v interface{}) (string, bool) {
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// trimTrailingComma returns s with the final byte stripped iff that byte
// is a ','. The previous code unconditionally did `s[:len(s)-1]` which
// panicked on an empty accumulator (e.g. an ERROR response with no data
// fields, or an empty FilterExp slice).
func trimTrailingComma(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[len(s)-1] == ',' {
		return s[:len(s)-1]
	}
	return s
}

func GetRequestPbToJson(pbGetReq *pb.GetRequestMessage) string {
	jsonMessage := populateJsonFromProtoGetReq(pbGetReq)
	return jsonMessage
}

func GetResponsePbToJson(pbGetResp *pb.GetResponseMessage) string {
	jsonMessage := populateJsonFromProtoGetResp(pbGetResp)
	return jsonMessage
}

func GetRequestJsonToPb(vssGetReq string) *pb.GetRequestMessage {
	var getReqMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssGetReq), &getReqMessageMap)
	if err != nil {
		Error.Printf("GetRequestJsonToPb:Unmarshal error data=%s, err=%s", vssGetReq, err)
		return nil
	}
	pbGetRequestMessage := &pb.GetRequestMessage{}
	createGetRequestPb(pbGetRequestMessage, getReqMessageMap)
	return pbGetRequestMessage
}

func GetResponseJsonToPb(vssGetResp string) *pb.GetResponseMessage {
	var getRespMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssGetResp), &getRespMessageMap)
	if err != nil {
		Error.Printf("GetResponseJsonToPb:Unmarshal error data=%s, err=%s", vssGetResp, err)
		return nil
	}
	pbGetResponseMessage := &pb.GetResponseMessage{}
	createGetResponsePb(pbGetResponseMessage, getRespMessageMap)
	return pbGetResponseMessage
}

func SetRequestPbToJson(pbSetReq *pb.SetRequestMessage) string {
	jsonMessage := populateJsonFromProtoSetReq(pbSetReq)
	return jsonMessage
}

func SetResponsePbToJson(pbSetResp *pb.SetResponseMessage) string {
	jsonMessage := populateJsonFromProtoSetResp(pbSetResp)
	return jsonMessage
}

func SetRequestJsonToPb(vssSetReq string) *pb.SetRequestMessage {
	var setReqMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssSetReq), &setReqMessageMap)
	if err != nil {
		Error.Printf("SetRequestJsonToPb:Unmarshal error data=%s, err=%s", vssSetReq, err)
		return nil
	}
	pbSetRequestMessage := &pb.SetRequestMessage{}
	createSetRequestPb(pbSetRequestMessage, setReqMessageMap)
	return pbSetRequestMessage
}

func SetResponseJsonToPb(vssSetResp string) *pb.SetResponseMessage {
	var setRespMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssSetResp), &setRespMessageMap)
	if err != nil {
		Error.Printf("SetResponseJsonToPb:Unmarshal error data=%s, err=%s", vssSetResp, err)
		return nil
	}
	pbSetResponseMessage := &pb.SetResponseMessage{}
	createSetResponsePb(pbSetResponseMessage, setRespMessageMap)
	return pbSetResponseMessage
}

func SubscribeRequestPbToJson(pbSubscribeReq *pb.SubscribeRequestMessage) string {
	jsonMessage := populateJsonFromProtoSubscribeReq(pbSubscribeReq)
	return jsonMessage
}

func SubscribeStreamPbToJson(pbSubscribeResp *pb.SubscribeStreamMessage) string {
	jsonMessage := populateJsonFromProtoSubscribeStream(pbSubscribeResp)
	return jsonMessage
}

func SubscribeRequestJsonToPb(vssSubscribeReq string) *pb.SubscribeRequestMessage {
	var subscribeReqMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssSubscribeReq), &subscribeReqMessageMap)
	if err != nil {
		Error.Printf("SubscribeRequestJsonToPb:Unmarshal error data=%s, err=%s", vssSubscribeReq, err)
		return nil
	}
	pbSubscribeRequestMessage := &pb.SubscribeRequestMessage{}
	createSubscribeRequestPb(pbSubscribeRequestMessage, subscribeReqMessageMap)
	return pbSubscribeRequestMessage
}

func SubscribeStreamJsonToPb(vssSubscribeStream string) *pb.SubscribeStreamMessage {
	var subscribeStreamMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssSubscribeStream), &subscribeStreamMessageMap)
	if err != nil {
		Error.Printf("SubscribeStreamJsonToPb:Unmarshal error data=%s, err=%s", vssSubscribeStream, err)
		return nil
	}
	pbSubscribeStreamMessage := &pb.SubscribeStreamMessage{}
	createSubscribeStreamPb(pbSubscribeStreamMessage, subscribeStreamMessageMap)
	return pbSubscribeStreamMessage
}

func UnsubscribeRequestPbToJson(pbUnsubscribeReq *pb.UnsubscribeRequestMessage) string {
	jsonMessage := populateJsonFromProtoUnsubscribeReq(pbUnsubscribeReq)
	return jsonMessage
}

func UnsubscribeResponsePbToJson(pbUnsubscribeResp *pb.UnsubscribeResponseMessage) string {
	jsonMessage := populateJsonFromProtoUnsubscribeResp(pbUnsubscribeResp)
	return jsonMessage
}

func UnsubscribeRequestJsonToPb(vssUnsubscribeReq string) *pb.UnsubscribeRequestMessage {
	var unsubscribeReqMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssUnsubscribeReq), &unsubscribeReqMessageMap)
	if err != nil {
		Error.Printf("UnsubscribeRequestJsonToPb:Unmarshal error data=%s, err=%s", vssUnsubscribeReq, err)
		return nil
	}
	pbUnsubscribeRequestMessage := &pb.UnsubscribeRequestMessage{}
	createUnsubscribeRequestPb(pbUnsubscribeRequestMessage, unsubscribeReqMessageMap)
	return pbUnsubscribeRequestMessage
}

func UnsubscribeResponseJsonToPb(vssUnsubscribeResp string) *pb.UnsubscribeResponseMessage {
	var unsubscribeRespMessageMap map[string]interface{}
	err := json.Unmarshal([]byte(vssUnsubscribeResp), &unsubscribeRespMessageMap)
	if err != nil {
		Error.Printf("UnsubscribeResponseJsonToPb:Unmarshal error data=%s, err=%s", vssUnsubscribeResp, err)
		return nil
	}
	pbUnsubscribeResponseMessage := &pb.UnsubscribeResponseMessage{}
	createUnsubscribeResponsePb(pbUnsubscribeResponseMessage, unsubscribeRespMessageMap)
	return pbUnsubscribeResponseMessage
}

func ExtractSubscriptionId(jsonSubResponse string) string {
	var subResponseMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonSubResponse), &subResponseMap)
	if err != nil {
		Error.Printf("ExtractSubscriptionId:Unmarshal error response=%s, err=%s", jsonSubResponse, err)
		return ""
	}
	return mapStringOrEmpty(subResponseMap, "subscriptionId")
}

// ExtractServiceId returns the "serviceId" field from a VISSv3.2 Service
// profile invoke/monitor ACK response, or "" if absent/malformed. Mirrors
// ExtractSubscriptionId's role for the subscribe/unsubscribe pair; used by
// callers (e.g. the test client) that need to cancel an ONGOING invocation
// or monitoring session.
func ExtractServiceId(jsonServiceResponse string) string {
	var respMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonServiceResponse), &respMap)
	if err != nil {
		Error.Printf("ExtractServiceId:Unmarshal error response=%s, err=%s", jsonServiceResponse, err)
		return ""
	}
	return mapStringOrEmpty(respMap, "serviceId")
}

// applyFilterFromMessage parses messageMap["filter"] into a *pb.FilterExpressions.
// Returns nil if no filter is present or the shape is invalid - the caller
// leaves the proto's Filter field nil.
func applyFilterFromMessage(messageMap map[string]interface{}) *pb.FilterExpressions {
	filter, ok := messageMap["filter"]
	if !ok || filter == nil {
		return nil
	}
	switch vv := filter.(type) {
	case []interface{}:
		if len(vv) != 2 {
			Error.Printf("Max two filter expressions are allowed.")
			return nil
		}
		m0, ok0 := asMap(vv[0])
		m1, ok1 := asMap(vv[1])
		if !ok0 || !ok1 {
			Error.Printf("applyFilterFromMessage: filter array elements must be objects")
			return nil
		}
		out := &pb.FilterExpressions{
			FilterExp: []*pb.FilterExpressions_FilterExpression{
				{}, {},
			},
		}
		createPbFilter(0, m0, out)
		createPbFilter(1, m1, out)
		return out
	case map[string]interface{}:
		out := &pb.FilterExpressions{
			FilterExp: []*pb.FilterExpressions_FilterExpression{{}},
		}
		createPbFilter(0, vv, out)
		return out
	default:
		Info.Println(filter, "is of an unknown type")
		return nil
	}
}

func createGetRequestPb(protoMessage *pb.GetRequestMessage, messageMap map[string]interface{}) {
	if path, ok := mapString(messageMap, "path"); ok {
		protoMessage.Path = path
	}
	if f := applyFilterFromMessage(messageMap); f != nil {
		protoMessage.Filter = f
	}
	if auth, ok := mapString(messageMap, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if dc, ok := mapString(messageMap, "dc"); ok {
		protoMessage.DC = &dc
	}
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createGetResponsePb(protoMessage *pb.GetResponseMessage, messageMap map[string]interface{}) {
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
	if ts, ok := mapString(messageMap, "ts"); ok {
		protoMessage.Ts = ts
	}
	if auth, ok := mapString(messageMap, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if errMap, isErr := mapAsMap(messageMap, "error"); isErr {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = getProtoErrorMessage(errMap)
		return
	}
	if _, present := messageMap["error"]; present {
		// Present but not a map — treat as a generic error to avoid silent success.
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = &pb.ErrorResponseMessage{}
		return
	}
	protoMessage.Status = pb.ResponseStatus_SUCCESS
	protoMessage.SuccessResponse = &pb.GetResponseMessage_SuccessResponseMessage{}
	numOfDataElements := getNumOfDataElements(messageMap["data"])
	if numOfDataElements > 0 {
		protoMessage.SuccessResponse.DataPack = &pb.DataPackages{}
		protoMessage.SuccessResponse.DataPack.Data = make([]*pb.DataPackages_DataPackage, 0, numOfDataElements)
		for i := 0; i < numOfDataElements; i++ {
			if elem := createDataElement(i, messageMap["data"]); elem != nil {
				protoMessage.SuccessResponse.DataPack.Data = append(protoMessage.SuccessResponse.DataPack.Data, elem)
			}
		}
	} else {
		metadata, _ := json.Marshal(messageMap["metadata"])
		metadataStr := string(metadata)
		protoMessage.SuccessResponse.Metadata = &metadataStr
	}
}

// getProtoErrorMessage extracts an error sub-message defensively. Non-string
// field values are skipped rather than panicking.
func getProtoErrorMessage(messageErrorMap map[string]interface{}) *pb.ErrorResponseMessage {
	protoErrorMessage := &pb.ErrorResponseMessage{}
	if messageErrorMap == nil {
		return protoErrorMessage
	}
	if v, ok := mapString(messageErrorMap, "number"); ok {
		protoErrorMessage.Number = v
	}
	if v, ok := mapString(messageErrorMap, "reason"); ok {
		protoErrorMessage.Reason = v
	}
	if v, ok := mapString(messageErrorMap, "description"); ok {
		protoErrorMessage.Description = v
	}
	return protoErrorMessage
}

func getNumOfDataElements(messageDataMap interface{}) int {
	if messageDataMap == nil {
		return 0
	}
	switch vv := messageDataMap.(type) {
	case []interface{}:
		return len(vv)
	}
	return 1
}

// createDataElement extracts the i'th data element from messageDataMap.
// Returns nil if the input shape is invalid (not an object, missing fields,
// or wrong types) so a malformed client payload no longer panics.
func createDataElement(index int, messageDataMap interface{}) *pb.DataPackages_DataPackage {
	var dataObject map[string]interface{}
	switch vv := messageDataMap.(type) {
	case []interface{}:
		if index < 0 || index >= len(vv) {
			return nil
		}
		m, ok := asMap(vv[index])
		if !ok {
			return nil
		}
		dataObject = m
	default:
		m, ok := asMap(vv)
		if !ok {
			return nil
		}
		dataObject = m
	}
	var protoDataElement pb.DataPackages_DataPackage
	if p, ok := mapString(dataObject, "path"); ok {
		protoDataElement.Path = p
	}
	dpRaw := dataObject["dp"]
	numOfDataPointElements := getNumOfDataPointElements(dpRaw)
	protoDataElement.Dp = make([]*pb.DataPackages_DataPackage_DataPoint, 0, numOfDataPointElements)
	for i := 0; i < numOfDataPointElements; i++ {
		if dp := createDataPointElement(i, dpRaw); dp != nil {
			protoDataElement.Dp = append(protoDataElement.Dp, dp)
		}
	}
	return &protoDataElement
}

func getNumOfDataPointElements(messageDataPointMap interface{}) int {
	if messageDataPointMap == nil {
		return 0
	}
	switch vv := messageDataPointMap.(type) {
	case []interface{}:
		return len(vv)
	}
	return 1
}

// createDataPointElement extracts the i'th data-point. Returns nil for any
// shape mismatch (not an object, missing fields, etc.) so malformed input
// doesn't panic.
func createDataPointElement(index int, messageDataPointMap any) *pb.DataPackages_DataPackage_DataPoint {
	var dataPointObject map[string]any
	switch vv := messageDataPointMap.(type) {
	case []any:
		if index < 0 || index >= len(vv) {
			return nil
		}
		m, ok := asMap(vv[index])
		if !ok {
			return nil
		}
		dataPointObject = m
	default:
		m, ok := asMap(vv)
		if !ok {
			return nil
		}
		dataPointObject = m
	}
	var protoDataPointElement pb.DataPackages_DataPackage_DataPoint
	if v, ok := mapString(dataPointObject, "value"); ok {
		protoDataPointElement.Value = v
	}
	if ts, ok := mapString(dataPointObject, "ts"); ok {
		protoDataPointElement.Ts = ts
	}
	return &protoDataPointElement
}

// createPbFilter populates filter.FilterExp[index] from a JSON filter
// expression. Defensive: invalid variant or wrong-typed parameter logs
// and leaves the entry's Value sub-fields nil rather than panicking.
func createPbFilter(index int, filterExpression map[string]interface{}, filter *pb.FilterExpressions) {
	if index < 0 || index >= len(filter.FilterExp) {
		return
	}
	variantStr, ok := mapString(filterExpression, "variant")
	if !ok {
		Error.Printf("createPbFilter: missing/invalid variant field")
		return
	}
	filterVariant := getFilterVariant(variantStr)
	filter.FilterExp[index].Variant = filterVariant
	filter.FilterExp[index].Value = &pb.FilterExpressions_FilterExpression_FilterValue{}
	parameter := filterExpression["parameter"]
	switch filterVariant {
	case pb.FilterExpressions_FilterExpression_PATHS:
		filter.FilterExp[index].Value.ValuePaths = getPbPathsFilterValue(parameter)
	case pb.FilterExpressions_FilterExpression_TIMEBASED:
		if m, ok := asMap(parameter); ok {
			filter.FilterExp[index].Value.ValueTimebased = getPbTimebasedFilterValue(m)
		}
	case pb.FilterExpressions_FilterExpression_RANGE:
		rangeLen := getNumOfRangeExpressions(parameter)
		filter.FilterExp[index].Value.ValueRange = make([]*pb.FilterExpressions_FilterExpression_FilterValue_RangeValue, 0, rangeLen)
		for i := 0; i < rangeLen; i++ {
			if rv := getPbRangeFilterValue(i, parameter); rv != nil {
				filter.FilterExp[index].Value.ValueRange = append(filter.FilterExp[index].Value.ValueRange, rv)
			}
		}
	case pb.FilterExpressions_FilterExpression_CHANGE:
		if m, ok := asMap(parameter); ok {
			filter.FilterExp[index].Value.ValueChange = getPbChangeFilterValue(m)
		}
	case pb.FilterExpressions_FilterExpression_CURVELOG:
		if m, ok := asMap(parameter); ok {
			filter.FilterExp[index].Value.ValueCurvelog = getPbCurvelogFilterValue(m)
		}
	case pb.FilterExpressions_FilterExpression_HISTORY:
		if period, ok := asString(parameter); ok {
			filter.FilterExp[index].Value.ValueHistory = &pb.FilterExpressions_FilterExpression_FilterValue_HistoryValue{TimePeriod: period}
		}
	case pb.FilterExpressions_FilterExpression_METADATA:
		Warning.Printf("Filter variant is not supported by protobuf encoding.")
	default:
		Error.Printf("Filter variant is unknown: %q", variantStr)
	}
}

func getNumOfRangeExpressions(valueMap interface{}) int {
	switch vv := valueMap.(type) {
	case []interface{}:
		return len(vv)
	default:
		return 1
	}
}

func getPbPathsFilterValue(filterValueExpression interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_PathsValue {
	var protoPathsValue pb.FilterExpressions_FilterExpression_FilterValue_PathsValue
	switch vv := filterValueExpression.(type) {
	case []interface{}:
		protoPathsValue.RelativePath = make([]string, 0, len(vv))
		for i := 0; i < len(vv); i++ {
			s, ok := asString(vv[i])
			if !ok {
				Error.Printf("getPbPathsFilterValue: element %d not a string", i)
				continue
			}
			protoPathsValue.RelativePath = append(protoPathsValue.RelativePath, s)
		}
	case string:
		protoPathsValue.RelativePath = []string{vv}
	default:
		Info.Println(filterValueExpression, "is of an unknown type")
	}
	return &protoPathsValue
}

func getPbTimebasedFilterValue(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue {
	period, _ := mapString(filterExpression, "period")
	return &pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue{Period: period}
}

func getPbRangeFilterValue(index int, valueMap interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_RangeValue {
	var rangeObject map[string]interface{}
	switch vv := valueMap.(type) {
	case []interface{}:
		if index < 0 || index >= len(vv) {
			return nil
		}
		m, ok := asMap(vv[index])
		if !ok {
			return nil
		}
		rangeObject = m
	case map[string]interface{}:
		rangeObject = vv
	default:
		return nil
	}
	logicOp, _ := mapString(rangeObject, "logic-op")
	boundary, _ := mapString(rangeObject, "boundary")
	return &pb.FilterExpressions_FilterExpression_FilterValue_RangeValue{
		LogicOperator: logicOp,
		Boundary:      boundary,
	}
}

func getPbChangeFilterValue(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue {
	logicOp, _ := mapString(filterExpression, "logic-op")
	diff, _ := mapString(filterExpression, "diff")
	return &pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue{
		LogicOperator: logicOp,
		Diff:          diff,
	}
}

func getPbCurvelogFilterValue(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue {
	maxErr, _ := mapString(filterExpression, "maxerr")
	bufSize, _ := mapString(filterExpression, "bufsize")
	return &pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue{
		MaxErr:  maxErr,
		BufSize: bufSize,
	}
}

func getFilterVariant(filterVariant string) pb.FilterExpressions_FilterExpression_FilterVariant {
	switch filterVariant {
	case "paths":
		return pb.FilterExpressions_FilterExpression_PATHS
	case "timebased":
		return pb.FilterExpressions_FilterExpression_TIMEBASED
	case "range":
		return pb.FilterExpressions_FilterExpression_RANGE
	case "change":
		return pb.FilterExpressions_FilterExpression_CHANGE
	case "curvelog":
		return pb.FilterExpressions_FilterExpression_CURVELOG
	case "history":
		return pb.FilterExpressions_FilterExpression_HISTORY
	case "metadata":
		return pb.FilterExpressions_FilterExpression_METADATA
	}
	return pb.FilterExpressions_FilterExpression_METADATA + 100 //undefined filter variant
}

func createSubscribeRequestPb(protoMessage *pb.SubscribeRequestMessage, messageMap map[string]interface{}) {
	if path, ok := mapString(messageMap, "path"); ok {
		protoMessage.Path = path
	}
	if f := applyFilterFromMessage(messageMap); f != nil {
		protoMessage.Filter = f
	}
	if auth, ok := mapString(messageMap, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if dc, ok := mapString(messageMap, "dc"); ok {
		protoMessage.DC = &dc
	}
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createSubscribeStreamPb(protoMessage *pb.SubscribeStreamMessage, messageMap map[string]interface{}) {
	action, _ := mapString(messageMap, "action")
	errMap, isErr := mapAsMap(messageMap, "error")
	_, errPresent := messageMap["error"]
	if action == "subscribe" { // RESPONSE
		protoMessage.MType = pb.SubscribeResponseType_SUB_RESPONSE
		protoMessage.Response = &pb.SubscribeStreamMessage_SubscribeResponseMessage{}
		if reqId, ok := mapString(messageMap, "requestId"); ok {
			protoMessage.Response.RequestId = reqId
		}
		if ts, ok := mapString(messageMap, "ts"); ok {
			protoMessage.Response.Ts = ts
		}
		if auth, ok := mapString(messageMap, "authorization"); ok {
			protoMessage.Response.Authorization = &auth
		}
		if !errPresent {
			if subId, ok := mapString(messageMap, "subscriptionId"); ok {
				protoMessage.Response.SubscriptionId = &subId
			}
			protoMessage.Status = pb.ResponseStatus_SUCCESS
		} else {
			protoMessage.Status = pb.ResponseStatus_ERROR
			if isErr {
				protoMessage.Response.ErrorResponse = getProtoErrorMessage(errMap)
			} else {
				protoMessage.Response.ErrorResponse = &pb.ErrorResponseMessage{}
			}
		}
		return
	}
	// EVENT
	protoMessage.MType = pb.SubscribeResponseType_SUB_EVENT
	protoMessage.Event = &pb.SubscribeStreamMessage_SubscribeEventMessage{}
	if subId, ok := mapString(messageMap, "subscriptionId"); ok {
		protoMessage.Event.SubscriptionId = subId
	}
	if ts, ok := mapString(messageMap, "ts"); ok {
		protoMessage.Event.Ts = ts
	}
	if !errPresent {
		protoMessage.Status = pb.ResponseStatus_SUCCESS
		protoMessage.Event.SuccessResponse = &pb.SubscribeStreamMessage_SubscribeEventMessage_SuccessResponseMessage{}
		numOfDataElements := getNumOfDataElements(messageMap["data"])
		protoMessage.Event.SuccessResponse.DataPack = &pb.DataPackages{}
		protoMessage.Event.SuccessResponse.DataPack.Data = make([]*pb.DataPackages_DataPackage, 0, numOfDataElements)
		for i := 0; i < numOfDataElements; i++ {
			if elem := createDataElement(i, messageMap["data"]); elem != nil {
				protoMessage.Event.SuccessResponse.DataPack.Data = append(protoMessage.Event.SuccessResponse.DataPack.Data, elem)
			}
		}
	} else {
		protoMessage.Status = pb.ResponseStatus_ERROR
		if isErr {
			protoMessage.Event.ErrorResponse = getProtoErrorMessage(errMap)
		} else {
			protoMessage.Event.ErrorResponse = &pb.ErrorResponseMessage{}
		}
	}
}

func createSetRequestPb(protoMessage *pb.SetRequestMessage, messageMap map[string]interface{}) {
	if path, ok := mapString(messageMap, "path"); ok {
		protoMessage.Path = path
	}
	if val, ok := mapString(messageMap, "value"); ok {
		protoMessage.Value = val
	}
	if auth, ok := mapString(messageMap, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createSetResponsePb(protoMessage *pb.SetResponseMessage, messageMap map[string]interface{}) {
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
	if ts, ok := mapString(messageMap, "ts"); ok {
		protoMessage.Ts = ts
	}
	if auth, ok := mapString(messageMap, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if errMap, isErr := mapAsMap(messageMap, "error"); isErr {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = getProtoErrorMessage(errMap)
	} else if _, present := messageMap["error"]; present {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = &pb.ErrorResponseMessage{}
	} else {
		protoMessage.Status = pb.ResponseStatus_SUCCESS
	}
}

func createUnsubscribeRequestPb(protoMessage *pb.UnsubscribeRequestMessage, messageMap map[string]interface{}) {
	if subId, ok := mapString(messageMap, "subscriptionId"); ok {
		protoMessage.SubscriptionId = subId
	}
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createUnsubscribeResponsePb(protoMessage *pb.UnsubscribeResponseMessage, messageMap map[string]interface{}) {
	if reqId, ok := mapString(messageMap, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
	if ts, ok := mapString(messageMap, "ts"); ok {
		protoMessage.Ts = ts
	}
	if errMap, isErr := mapAsMap(messageMap, "error"); isErr {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = getProtoErrorMessage(errMap)
	} else if _, present := messageMap["error"]; present {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = &pb.ErrorResponseMessage{}
	} else {
		protoMessage.Status = pb.ResponseStatus_SUCCESS
	}
}

// *******************************Proto to JSON code ***************************************
func populateJsonFromProtoGetReq(protoMessage *pb.GetRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"get"`
	jsonMessage += `,"path":"` + protoMessage.GetPath() + `"` + getJsonFilter(protoMessage.Filter) +
		createJSON(protoMessage.GetAuthorization(), "authorization") + createJSON(protoMessage.GetDC(), "dc") +
		createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoGetResp(protoMessage *pb.GetResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"get"`
	if protoMessage.GetStatus() == 0 { //SUCCESSFUL
		// Defensive: SuccessResponse may be nil for a metadata-only response,
		// or if the proto was incompletely populated. GetDataPack() / GetData()
		// are nil-safe but only when SuccessResponse itself is non-nil.
		var data []*pb.DataPackages_DataPackage
		if sr := protoMessage.GetSuccessResponse(); sr != nil {
			data = sr.GetDataPack().GetData()
		}
		jsonMessage += createJsonData(data)
	} else { // ERROR
		jsonMessage += getJsonError(protoMessage.GetErrorResponse())
	}
	jsonMessage += `,"ts":"` + protoMessage.GetTs() + `"` + createJSON(protoMessage.GetRequestId(), "requestId") + createJSON(protoMessage.GetAuthorization(), "authorization")
	return jsonMessage + "}"
}

func populateJsonFromProtoSetReq(protoMessage *pb.SetRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"set"`
	jsonMessage += `,"path":"` + protoMessage.GetPath() + `","value":"` +
		protoMessage.GetValue() + `"` + createJSON(protoMessage.GetAuthorization(), "authorization") + createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoSetResp(protoMessage *pb.SetResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"set"`
	if protoMessage.GetStatus() != 0 { //ERROR
		jsonMessage += getJsonError(protoMessage.GetErrorResponse())
	}
	jsonMessage += `,"ts":"` + protoMessage.GetTs() + `"` + createJSON(protoMessage.GetRequestId(), "requestId") + createJSON(protoMessage.GetAuthorization(), "authorization")
	return jsonMessage + "}"
}

func populateJsonFromProtoSubscribeReq(protoMessage *pb.SubscribeRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"subscribe"`
	jsonMessage += `,"path":"` + protoMessage.GetPath() + `"` + getJsonFilter(protoMessage.Filter) +
		createJSON(protoMessage.GetAuthorization(), "authorization") + createJSON(protoMessage.GetDC(), "dc") +
		createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoSubscribeStream(protoMessage *pb.SubscribeStreamMessage) string {
	jsonMessage := "{"
	switch protoMessage.GetMType() {
	case pb.SubscribeResponseType_SUB_RESPONSE:
		jsonMessage += `"action":"subscribe"`
		// Defensive: Response sub-message may be nil if the proto was
		// incompletely populated.
		resp := protoMessage.GetResponse()
		if protoMessage.GetStatus() != 0 { //ERROR
			var errResp *pb.ErrorResponseMessage
			if resp != nil {
				errResp = resp.GetErrorResponse()
			}
			jsonMessage += getJsonError(errResp)
		}
		var ts, subId, reqId, auth string
		if resp != nil {
			ts = resp.GetTs()
			subId = resp.GetSubscriptionId()
			reqId = resp.GetRequestId()
			auth = resp.GetAuthorization()
		}
		jsonMessage += `,"ts":"` + ts + `"` + createJSON(subId, "subscriptionId") +
			createJSON(reqId, "requestId") + createJSON(auth, "authorization")
	case pb.SubscribeResponseType_SUB_EVENT:
		jsonMessage += `"action":"subscription"`
		// Defensive: Event sub-message may be nil.
		ev := protoMessage.GetEvent()
		if protoMessage.GetStatus() == 0 { //SUCCESSFUL
			var data []*pb.DataPackages_DataPackage
			if ev != nil {
				if sr := ev.GetSuccessResponse(); sr != nil {
					data = sr.GetDataPack().GetData()
				}
			}
			jsonMessage += createJsonData(data)
		} else { // ERROR
			var errResp *pb.ErrorResponseMessage
			if ev != nil {
				errResp = ev.GetErrorResponse()
			}
			jsonMessage += getJsonError(errResp)
		}
		var ts, subId string
		if ev != nil {
			ts = ev.GetTs()
			subId = ev.GetSubscriptionId()
		}
		jsonMessage += `,"ts":"` + ts + `"` + createJSON(subId, "subscriptionId")
	}
	return jsonMessage + "}"
}

func populateJsonFromProtoUnsubscribeReq(protoMessage *pb.UnsubscribeRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"unsubscribe"`
	jsonMessage += createJSON(protoMessage.GetSubscriptionId(), "subscriptionId") + createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoUnsubscribeResp(protoMessage *pb.UnsubscribeResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"unsubscribe"`
	if protoMessage.GetStatus() != 0 { // ERROR
		jsonMessage += getJsonError(protoMessage.GetErrorResponse())
	}
	jsonMessage += `,"ts":"` + protoMessage.GetTs() + `"` + createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func getJsonFilter(filter *pb.FilterExpressions) string {
	if filter == nil {
		return ""
	}
	filterExp := filter.GetFilterExp()
	if len(filterExp) == 0 {
		return ""
	}
	jsonFilter := ""
	if len(filterExp) > 1 {
		jsonFilter = "["
	}
	for i := 0; i < len(filterExp); i++ {
		jsonFilter += synthesizeFilter(filterExp[i]) + ","
	}
	jsonFilter = trimTrailingComma(jsonFilter)
	if len(filterExp) > 1 {
		jsonFilter += "]"
	}
	return `,"filter":` + jsonFilter
}

func synthesizeFilter(filterExp *pb.FilterExpressions_FilterExpression) string {
	fType := ""
	value := ""
	switch filterExp.GetVariant() {
	case 0:
		fType = "paths"
		value = getJsonFilterValuePaths(filterExp)
	case 1:
		fType = "timebased"
		value = getJsonFilterValueTimebased(filterExp)
	case 2:
		fType = "range"
		value = getJsonFilterValueRange(filterExp)
	case 3:
		fType = "change"
		value = getJsonFilterValueChange(filterExp)
	case 4:
		fType = "curvelog"
		value = getJsonFilterValueCurvelog(filterExp)
	case 5:
		fType = "history"
		value = getJsonFilterValueHistory(filterExp)
	case 6:
		fType = "metadata"
		value = getJsonFilterValueMetadata(filterExp)
	}
	return `{"variant":"` + fType + `","parameter":` + value + `}`
}

func getJsonFilterValuePaths(filterExp *pb.FilterExpressions_FilterExpression) string {
	relativePaths := filterExp.GetValue().GetValuePaths().GetRelativePath()
	if len(relativePaths) == 0 {
		return `""`
	}
	value := ""
	if len(relativePaths) > 1 {
		value = "["
	}
	for i := 0; i < len(relativePaths); i++ {
		value += `"` + jsonEscape(relativePaths[i]) + `",`
	}
	value = trimTrailingComma(value)
	if len(relativePaths) > 1 {
		value += "]"
	}
	return value
}

func getJsonFilterValueTimebased(filterExp *pb.FilterExpressions_FilterExpression) string {
	period := filterExp.GetValue().GetValueTimebased().GetPeriod()
	return `{"period":"` + period + `"}`
}

func getJsonFilterValueRange(filterExp *pb.FilterExpressions_FilterExpression) string {
	rangeValue := filterExp.GetValue().GetValueRange()
	if len(rangeValue) == 0 {
		return `{}`
	}
	value := ""
	if len(rangeValue) > 1 {
		value = "["
	}
	for i := 0; i < len(rangeValue); i++ {
		logicOperator := rangeValue[i].GetLogicOperator()
		boundary := rangeValue[i].GetBoundary()
		value += `{"logic-op":"` + jsonEscape(logicOperator) + `","boundary":"` + jsonEscape(boundary) + `"},`
	}
	value = trimTrailingComma(value)
	if len(rangeValue) > 1 {
		value += "]"
	}
	return value
}

func getJsonFilterValueChange(filterExp *pb.FilterExpressions_FilterExpression) string {
	logicOperator := filterExp.GetValue().GetValueChange().GetLogicOperator()
	diff := filterExp.GetValue().GetValueChange().GetDiff()
	return `{"logic-op":"` + logicOperator + `","diff":"` + diff + `"}`
}

func getJsonFilterValueCurvelog(filterExp *pb.FilterExpressions_FilterExpression) string {
	maxErr := filterExp.GetValue().GetValueCurvelog().GetMaxErr()
	bufSize := filterExp.GetValue().GetValueCurvelog().GetBufSize()
	return `{"maxerr":"` + maxErr + `","bufsize":"` + bufSize + `"}`
}

func getJsonFilterValueHistory(filterExp *pb.FilterExpressions_FilterExpression) string {
	timePeriod := filterExp.GetValue().GetValueHistory().GetTimePeriod()
	return `"` + timePeriod + `"`
}

func getJsonFilterValueMetadata(filterExp *pb.FilterExpressions_FilterExpression) string {
	tree := filterExp.GetValue().GetValueMetadata().GetTree()
	return tree
}

func createJSON(value string, key string) string {
	if len(value) > 0 {
		return `,"` + key + `":"` + value + `"`
	}
	return ""
}

func createJsonData(dataPack []*pb.DataPackages_DataPackage) string {
	// Defensive: empty dataPack must not produce malformed JSON.
	if len(dataPack) == 0 {
		return ""
	}
	data := ""
	if len(dataPack) > 1 {
		data += "["
	}
	for i := 0; i < len(dataPack); i++ {
		if dataPack[i] == nil {
			continue
		}
		path := dataPack[i].GetPath()
		dp := getJsonDp(dataPack[i])
		data += `{"path":"` + jsonEscape(path) + `","dp":` + dp + `},`
	}
	data = trimTrailingComma(data)
	if len(dataPack) > 1 {
		data += "]"
	}
	return `,"data":` + data
}

func getJsonDp(dataPack *pb.DataPackages_DataPackage) string {
	if dataPack == nil {
		return `{}`
	}
	dpPack := dataPack.GetDp()
	if len(dpPack) == 0 {
		return `{}`
	}
	dp := ""
	if len(dpPack) > 1 {
		dp += "["
	}
	for i := 0; i < len(dpPack); i++ {
		if dpPack[i] == nil {
			continue
		}
		value := dpPack[i].GetValue()
		ts := dpPack[i].GetTs()
		dp += `{"value":"` + jsonEscape(value) + `","ts":"` + jsonEscape(ts) + `"},`
	}
	dp = trimTrailingComma(dp)
	if len(dpPack) > 1 {
		dp += "]"
	}
	return dp
}

func getJsonError(errorResponse *pb.ErrorResponseMessage) string {
	// Defensive nil receiver: GetNumber/etc. are nil-safe via the generated
	// proto getters, so this is belt-and-suspenders.
	number := errorResponse.GetNumber()
	reason := errorResponse.GetReason()
	description := errorResponse.GetDescription()
	return `,"error":{"number":"` + jsonEscape(number) + `","reason":"` + jsonEscape(reason) + `","description":"` + jsonEscape(description) + `"}`
}

// jsonEscape produces a JSON-safe string body (without surrounding quotes).
// Pre-fix the file built JSON via string concatenation, so any value
// containing '"' or '\' produced invalid JSON that downstream parsers
// rejected. This helper handles the minimum set of metacharacters that
// json.Marshal would escape.
func jsonEscape(s string) string {
	if !needsEscape(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				// Control char - encode as \u00XX
				b.WriteString(`\u00`)
				const hex = "0123456789abcdef"
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func needsEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			return true
		}
	}
	return false
}

// *****************************************************************
// VISSv3.2 Service profile (invoke/monitor/cancel/discover) support.
//
// Unlike the VSS Data profile messages above, the Service profile's
// input/output/metadata payloads are arbitrary, per-procedure/per-tree-node
// JSON objects whose shape isn't known at compile time, so they are carried
// as google.protobuf.Struct (structpb) rather than a hand-rolled message.
// *****************************************************************

func serviceStatusFromString(status string) pb.ServiceStatus {
	if v, ok := pb.ServiceStatus_value[status]; ok {
		return pb.ServiceStatus(v)
	}
	return pb.ServiceStatus_UNKNOWN
}

// -- ServiceFilter (invoke/monitor "filter": a single monitoring-variant
// object, a single resource-variant object, or a 2-element array combining
// one of each) --------------------------------------------------------

func applyServiceFilterFromMessage(messageMap map[string]interface{}) []*pb.ServiceFilter {
	filter, ok := messageMap["filter"]
	if !ok || filter == nil {
		return nil
	}
	switch vv := filter.(type) {
	case []interface{}:
		out := make([]*pb.ServiceFilter, 0, len(vv))
		for _, item := range vv {
			m, ok := asMap(item)
			if !ok {
				Error.Printf("applyServiceFilterFromMessage: filter array elements must be objects")
				continue
			}
			out = append(out, createServiceFilterPb(m))
		}
		return out
	case map[string]interface{}:
		return []*pb.ServiceFilter{createServiceFilterPb(vv)}
	default:
		Info.Println(filter, "is of an unknown type")
		return nil
	}
}

func createServiceFilterPb(m map[string]interface{}) *pb.ServiceFilter {
	sf := &pb.ServiceFilter{}
	variant, _ := mapString(m, "variant")
	switch variant {
	case "all":
		sf.Variant = pb.ServiceFilterVariant_ALL
	case "status":
		sf.Variant = pb.ServiceFilterVariant_STATUS
	case "none":
		sf.Variant = pb.ServiceFilterVariant_NONE
	case "timebased":
		sf.Variant = pb.ServiceFilterVariant_TIMEBASED_SVC
		if param, ok := asMap(m["parameter"]); ok {
			if period, ok := mapString(param, "period"); ok {
				sf.Period = &period
			}
		}
	case "resource":
		sf.Variant = pb.ServiceFilterVariant_RESOURCE
		if arr, ok := m["parameter"].([]interface{}); ok {
			for _, v := range arr {
				if s, ok := asString(v); ok {
					sf.ResourcePath = append(sf.ResourcePath, s)
				}
			}
		}
	default:
		Error.Printf("createServiceFilterPb: unknown service filter variant %q", variant)
	}
	return sf
}

// getJsonServiceFilter renders a `,"filter":...` fragment from 0, 1, or 2
// ServiceFilter entries, matching the shape parseServiceFilter/
// parseFilterArray on the server accept (single object, or a 2-element
// array). Returns "" when filters is empty so the "filter" key is omitted
// entirely (parseServiceFilter(nil) defaults to the "all" variant).
func getJsonServiceFilter(filters []*pb.ServiceFilter) string {
	if len(filters) == 0 {
		return ""
	}
	if len(filters) == 1 {
		return `,"filter":` + synthesizeServiceFilter(filters[0])
	}
	parts := make([]string, 0, len(filters))
	for _, f := range filters {
		parts = append(parts, synthesizeServiceFilter(f))
	}
	return `,"filter":[` + strings.Join(parts, ",") + `]`
}

func synthesizeServiceFilter(sf *pb.ServiceFilter) string {
	switch sf.GetVariant() {
	case pb.ServiceFilterVariant_ALL:
		return `{"variant":"all"}`
	case pb.ServiceFilterVariant_STATUS:
		return `{"variant":"status"}`
	case pb.ServiceFilterVariant_NONE:
		return `{"variant":"none"}`
	case pb.ServiceFilterVariant_TIMEBASED_SVC:
		return `{"variant":"timebased","parameter":{"period":"` + jsonEscape(sf.GetPeriod()) + `"}}`
	case pb.ServiceFilterVariant_RESOURCE:
		paths := sf.GetResourcePath()
		items := make([]string, 0, len(paths))
		for _, p := range paths {
			items = append(items, `"`+jsonEscape(p)+`"`)
		}
		return `{"variant":"resource","parameter":[` + strings.Join(items, ",") + `]}`
	}
	return `{}`
}

// -- ServiceError (unifies the invoke/monitor/cancel/discover error shape
// {number,reason,description,[fields]} and the monitoring-event error shape
// {code,message}) -------------------------------------------------------

func createServiceErrorPb(errMap map[string]interface{}) *pb.ServiceError {
	se := &pb.ServiceError{}
	if v, ok := mapString(errMap, "number"); ok {
		se.Number = &v
	}
	if v, ok := mapString(errMap, "reason"); ok {
		se.Reason = &v
	}
	if v, ok := mapString(errMap, "description"); ok {
		se.Description = &v
	}
	if arr, ok := errMap["fields"].([]interface{}); ok {
		for _, f := range arr {
			if s, ok := asString(f); ok {
				se.Fields = append(se.Fields, s)
			}
		}
	}
	if v, ok := mapString(errMap, "code"); ok {
		se.Code = &v
	}
	if v, ok := mapString(errMap, "message"); ok {
		se.Message = &v
	}
	return se
}

func getJsonServiceError(se *pb.ServiceError) string {
	if se == nil {
		return ""
	}
	var parts []string
	if se.Number != nil {
		parts = append(parts, `"number":"`+jsonEscape(se.GetNumber())+`"`)
	}
	if se.Reason != nil {
		parts = append(parts, `"reason":"`+jsonEscape(se.GetReason())+`"`)
	}
	if se.Description != nil {
		parts = append(parts, `"description":"`+jsonEscape(se.GetDescription())+`"`)
	}
	if len(se.Fields) > 0 {
		items := make([]string, 0, len(se.Fields))
		for _, f := range se.Fields {
			items = append(items, `"`+jsonEscape(f)+`"`)
		}
		parts = append(parts, `"fields":[`+strings.Join(items, ",")+`]`)
	}
	if se.Code != nil {
		parts = append(parts, `"code":"`+jsonEscape(se.GetCode())+`"`)
	}
	if se.Message != nil {
		parts = append(parts, `"message":"`+jsonEscape(se.GetMessage())+`"`)
	}
	if len(parts) == 0 {
		return `,"error":{}`
	}
	return `,"error":{` + strings.Join(parts, ",") + `}`
}

// -- ServiceOutdata / ServiceIndata / ServiceOutdataWrapper (single- vs
// multi-resource outdata shape - see wrapOutdataByResource/
// buildOutdataArrayFromWrapped in vissServiceMgr.go) ---------------------

func createServiceOutdataSinglePb(m map[string]interface{}) *pb.ServiceOutdata {
	so := &pb.ServiceOutdata{}
	if ts, ok := mapString(m, "ts"); ok {
		so.Ts = ts
	}
	if outMap, ok := asMap(m["output"]); ok {
		st, err := structpb.NewStruct(outMap)
		if err != nil {
			Error.Printf("createServiceOutdataSinglePb: NewStruct error: %s", err)
		} else {
			so.Output = st
		}
	}
	return so
}

// createServiceOutdataWrapperPb builds a ServiceOutdataWrapper from the raw
// "outdata" field value, which is either a single {"output":...,"ts":...}
// object or an array of {"<resourceKey>":{"output":...,"ts":...}} objects
// (multi-resource invocations).
func createServiceOutdataWrapperPb(outdataField interface{}) *pb.ServiceOutdataWrapper {
	switch v := outdataField.(type) {
	case map[string]interface{}:
		return &pb.ServiceOutdataWrapper{OutdataVariant: &pb.ServiceOutdataWrapper_Single{Single: createServiceOutdataSinglePb(v)}}
	case []interface{}:
		arr := &pb.ServiceResourceOutdataArray{}
		for _, item := range v {
			m, ok := asMap(item)
			if !ok {
				continue
			}
			for resKey, wrapped := range m {
				wm, ok := asMap(wrapped)
				if !ok {
					continue
				}
				arr.Resource = append(arr.Resource, &pb.ServiceResourceOutdata{
					ResourceKey: resKey,
					Outdata:     createServiceOutdataSinglePb(wm),
				})
			}
		}
		return &pb.ServiceOutdataWrapper{OutdataVariant: &pb.ServiceOutdataWrapper_Multi{Multi: arr}}
	default:
		return nil
	}
}

func jsonServiceOutdataSingle(so *pb.ServiceOutdata) string {
	outputJson := "{}"
	if so.GetOutput() != nil {
		if b, err := json.Marshal(so.GetOutput().AsMap()); err == nil {
			outputJson = string(b)
		}
	}
	return `{"output":` + outputJson + `,"ts":"` + jsonEscape(so.GetTs()) + `"}`
}

func getJsonServiceOutdata(wrapper *pb.ServiceOutdataWrapper) string {
	if wrapper == nil {
		return ""
	}
	if single := wrapper.GetSingle(); single != nil {
		return `,"outdata":` + jsonServiceOutdataSingle(single)
	}
	if multi := wrapper.GetMulti(); multi != nil {
		parts := make([]string, 0, len(multi.GetResource()))
		for _, r := range multi.GetResource() {
			parts = append(parts, `{"`+jsonEscape(r.GetResourceKey())+`":`+jsonServiceOutdataSingle(r.GetOutdata())+`}`)
		}
		return `,"outdata":[` + strings.Join(parts, ",") + `]`
	}
	return ""
}

func createServiceIndataPb(m map[string]interface{}) *pb.ServiceIndata {
	si := &pb.ServiceIndata{}
	if ts, ok := mapString(m, "ts"); ok {
		si.Ts = ts
	}
	if inMap, ok := asMap(m["input"]); ok {
		st, err := structpb.NewStruct(inMap)
		if err != nil {
			Error.Printf("createServiceIndataPb: NewStruct error: %s", err)
		} else {
			si.Input = st
		}
	}
	return si
}

func getJsonServiceIndata(si *pb.ServiceIndata) string {
	if si == nil {
		return ""
	}
	inputJson := "{}"
	if si.GetInput() != nil {
		if b, err := json.Marshal(si.GetInput().AsMap()); err == nil {
			inputJson = string(b)
		}
	}
	return `,"indata":{"input":` + inputJson + `,"ts":"` + jsonEscape(si.GetTs()) + `"}`
}

// timeoutStringFromMessage normalizes the "timeout" field (accepted by the
// server as either a JSON number or a numeric string, both milliseconds -
// see timeoutFromRequest in vissServiceMgr.go) to a string for the proto
// wire, which carries Timeout as a string.
func timeoutStringFromMessage(m map[string]interface{}) (string, bool) {
	switch v := m["timeout"].(type) {
	case string:
		return v, v != ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	default:
		return "", false
	}
}

// -- MonitoringEventMessage (the asynchronous "monitoring" event pushed on
// an Invoke/Monitor stream) ---------------------------------------------

func createMonitoringEventPb(m map[string]interface{}) *pb.MonitoringEventMessage {
	ev := &pb.MonitoringEventMessage{}
	if path, ok := mapString(m, "path"); ok {
		ev.Path = path
	}
	if outdataField, present := m["outdata"]; present {
		ev.Outdata = createServiceOutdataWrapperPb(outdataField)
	}
	if sid, ok := mapString(m, "serviceId"); ok {
		ev.ServiceId = sid
	}
	if status, ok := mapString(m, "status"); ok {
		ev.Status = serviceStatusFromString(status)
	}
	if errMap, ok := asMap(m["error"]); ok {
		ev.Error = createServiceErrorPb(errMap)
	}
	if pr, ok := m["progress"].(float64); ok {
		p := int32(pr)
		ev.Progress = &p
	}
	if ts, ok := mapString(m, "ts"); ok {
		ev.Ts = ts
	}
	return ev
}

func populateJsonFromProtoMonitoringEvent(ev *pb.MonitoringEventMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"monitoring"`
	jsonMessage += `,"path":"` + jsonEscape(ev.GetPath()) + `"`
	jsonMessage += getJsonServiceOutdata(ev.GetOutdata())
	jsonMessage += `,"serviceId":"` + jsonEscape(ev.GetServiceId()) + `"`
	jsonMessage += `,"status":"` + ev.GetStatus().String() + `"`
	jsonMessage += getJsonServiceError(ev.GetError())
	if ev.Progress != nil {
		jsonMessage += `,"progress":` + strconv.Itoa(int(ev.GetProgress()))
	}
	jsonMessage += `,"ts":"` + jsonEscape(ev.GetTs()) + `"`
	return jsonMessage + "}"
}

// -- invoke ---------------------------------------------------------------

func createInvokeRequestPb(protoMessage *pb.InvokeRequestMessage, m map[string]interface{}) {
	if path, ok := mapString(m, "path"); ok {
		protoMessage.Path = path
	}
	if inMap, ok := asMap(m["input"]); ok {
		st, err := structpb.NewStruct(inMap)
		if err != nil {
			Error.Printf("createInvokeRequestPb: NewStruct error: %s", err)
		} else {
			protoMessage.Input = st
		}
	}
	protoMessage.Filter = applyServiceFilterFromMessage(m)
	if auth, ok := mapString(m, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if to, ok := timeoutStringFromMessage(m); ok {
		protoMessage.Timeout = &to
	}
	if reqId, ok := mapString(m, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createInvokeResponsePb(protoMessage *pb.InvokeResponseMessage, m map[string]interface{}) {
	if status, ok := mapString(m, "status"); ok {
		protoMessage.Status = serviceStatusFromString(status)
	}
	if path, ok := mapString(m, "path"); ok {
		protoMessage.Path = &path
	}
	if outdataField, present := m["outdata"]; present {
		protoMessage.Outdata = createServiceOutdataWrapperPb(outdataField)
	}
	if sid, ok := mapString(m, "serviceId"); ok {
		protoMessage.ServiceId = &sid
	}
	if errMap, ok := asMap(m["error"]); ok {
		protoMessage.Error = createServiceErrorPb(errMap)
	}
	if reqId, ok := mapString(m, "requestId"); ok {
		protoMessage.RequestId = &reqId
	}
	if ts, ok := mapString(m, "ts"); ok {
		protoMessage.Ts = ts
	}
}

func createInvokeStreamPb(protoMessage *pb.InvokeStreamMessage, m map[string]interface{}) {
	action, _ := mapString(m, "action")
	if action == "monitoring" {
		protoMessage.MType = pb.ServiceStreamType_SERVICE_EVENT
		protoMessage.Payload = &pb.InvokeStreamMessage_Event{Event: createMonitoringEventPb(m)}
		return
	}
	protoMessage.MType = pb.ServiceStreamType_SERVICE_RESPONSE
	resp := &pb.InvokeResponseMessage{}
	createInvokeResponsePb(resp, m)
	protoMessage.Payload = &pb.InvokeStreamMessage_Response{Response: resp}
}

func populateJsonFromProtoInvokeReq(protoMessage *pb.InvokeRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"invoke"`
	jsonMessage += `,"path":"` + jsonEscape(protoMessage.GetPath()) + `"`
	if protoMessage.GetInput() != nil {
		if b, err := json.Marshal(protoMessage.GetInput().AsMap()); err == nil {
			jsonMessage += `,"input":` + string(b)
		}
	}
	jsonMessage += getJsonServiceFilter(protoMessage.GetFilter())
	jsonMessage += createJSON(protoMessage.GetAuthorization(), "authorization")
	jsonMessage += createJSON(protoMessage.GetTimeout(), "timeout")
	jsonMessage += createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoInvokeResp(protoMessage *pb.InvokeResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"invoke"`
	jsonMessage += `,"status":"` + protoMessage.GetStatus().String() + `"`
	jsonMessage += createJSON(protoMessage.GetPath(), "path")
	jsonMessage += getJsonServiceOutdata(protoMessage.GetOutdata())
	jsonMessage += createJSON(protoMessage.GetServiceId(), "serviceId")
	jsonMessage += getJsonServiceError(protoMessage.GetError())
	jsonMessage += createJSON(protoMessage.GetRequestId(), "requestId")
	jsonMessage += `,"ts":"` + jsonEscape(protoMessage.GetTs()) + `"`
	return jsonMessage + "}"
}

func populateJsonFromProtoInvokeStream(protoMessage *pb.InvokeStreamMessage) string {
	if protoMessage.GetMType() == pb.ServiceStreamType_SERVICE_EVENT {
		return populateJsonFromProtoMonitoringEvent(protoMessage.GetEvent())
	}
	return populateJsonFromProtoInvokeResp(protoMessage.GetResponse())
}

func InvokeRequestPbToJson(pbInvokeReq *pb.InvokeRequestMessage) string {
	return populateJsonFromProtoInvokeReq(pbInvokeReq)
}

func InvokeResponsePbToJson(pbInvokeResp *pb.InvokeResponseMessage) string {
	return populateJsonFromProtoInvokeResp(pbInvokeResp)
}

func InvokeStreamPbToJson(pbInvokeStream *pb.InvokeStreamMessage) string {
	return populateJsonFromProtoInvokeStream(pbInvokeStream)
}

func InvokeRequestJsonToPb(vssInvokeReq string) *pb.InvokeRequestMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssInvokeReq), &m); err != nil {
		Error.Printf("InvokeRequestJsonToPb:Unmarshal error data=%s, err=%s", vssInvokeReq, err)
		return nil
	}
	pbInvokeRequestMessage := &pb.InvokeRequestMessage{}
	createInvokeRequestPb(pbInvokeRequestMessage, m)
	return pbInvokeRequestMessage
}

func InvokeResponseJsonToPb(vssInvokeResp string) *pb.InvokeResponseMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssInvokeResp), &m); err != nil {
		Error.Printf("InvokeResponseJsonToPb:Unmarshal error data=%s, err=%s", vssInvokeResp, err)
		return nil
	}
	pbInvokeResponseMessage := &pb.InvokeResponseMessage{}
	createInvokeResponsePb(pbInvokeResponseMessage, m)
	return pbInvokeResponseMessage
}

func InvokeStreamJsonToPb(vssInvokeStream string) *pb.InvokeStreamMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssInvokeStream), &m); err != nil {
		Error.Printf("InvokeStreamJsonToPb:Unmarshal error data=%s, err=%s", vssInvokeStream, err)
		return nil
	}
	pbInvokeStreamMessage := &pb.InvokeStreamMessage{}
	createInvokeStreamPb(pbInvokeStreamMessage, m)
	return pbInvokeStreamMessage
}

// -- monitor ----------------------------------------------------------------

func createMonitorRequestPb(protoMessage *pb.MonitorRequestMessage, m map[string]interface{}) {
	if path, ok := mapString(m, "path"); ok {
		protoMessage.Path = path
	}
	protoMessage.Filter = applyServiceFilterFromMessage(m)
	if auth, ok := mapString(m, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if reqId, ok := mapString(m, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createMonitorResponsePb(protoMessage *pb.MonitorResponseMessage, m map[string]interface{}) {
	if status, ok := mapString(m, "status"); ok {
		protoMessage.Status = serviceStatusFromString(status)
	}
	if path, ok := mapString(m, "path"); ok {
		protoMessage.Path = &path
	}
	if indataMap, ok := asMap(m["indata"]); ok {
		protoMessage.Indata = createServiceIndataPb(indataMap)
	}
	if outdataField, present := m["outdata"]; present {
		protoMessage.Outdata = createServiceOutdataWrapperPb(outdataField)
	}
	if sid, ok := mapString(m, "serviceId"); ok {
		protoMessage.ServiceId = &sid
	}
	if errMap, ok := asMap(m["error"]); ok {
		protoMessage.Error = createServiceErrorPb(errMap)
	}
	if reqId, ok := mapString(m, "requestId"); ok {
		protoMessage.RequestId = &reqId
	}
	if ts, ok := mapString(m, "ts"); ok {
		protoMessage.Ts = ts
	}
}

func createMonitorStreamPb(protoMessage *pb.MonitorStreamMessage, m map[string]interface{}) {
	action, _ := mapString(m, "action")
	if action == "monitoring" {
		protoMessage.MType = pb.ServiceStreamType_SERVICE_EVENT
		protoMessage.Payload = &pb.MonitorStreamMessage_Event{Event: createMonitoringEventPb(m)}
		return
	}
	protoMessage.MType = pb.ServiceStreamType_SERVICE_RESPONSE
	resp := &pb.MonitorResponseMessage{}
	createMonitorResponsePb(resp, m)
	protoMessage.Payload = &pb.MonitorStreamMessage_Response{Response: resp}
}

func populateJsonFromProtoMonitorReq(protoMessage *pb.MonitorRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"monitor"`
	jsonMessage += `,"path":"` + jsonEscape(protoMessage.GetPath()) + `"`
	jsonMessage += getJsonServiceFilter(protoMessage.GetFilter())
	jsonMessage += createJSON(protoMessage.GetAuthorization(), "authorization")
	jsonMessage += createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoMonitorResp(protoMessage *pb.MonitorResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"monitor"`
	jsonMessage += `,"status":"` + protoMessage.GetStatus().String() + `"`
	jsonMessage += createJSON(protoMessage.GetPath(), "path")
	jsonMessage += getJsonServiceIndata(protoMessage.GetIndata())
	jsonMessage += getJsonServiceOutdata(protoMessage.GetOutdata())
	jsonMessage += createJSON(protoMessage.GetServiceId(), "serviceId")
	jsonMessage += getJsonServiceError(protoMessage.GetError())
	jsonMessage += createJSON(protoMessage.GetRequestId(), "requestId")
	jsonMessage += `,"ts":"` + jsonEscape(protoMessage.GetTs()) + `"`
	return jsonMessage + "}"
}

func populateJsonFromProtoMonitorStream(protoMessage *pb.MonitorStreamMessage) string {
	if protoMessage.GetMType() == pb.ServiceStreamType_SERVICE_EVENT {
		return populateJsonFromProtoMonitoringEvent(protoMessage.GetEvent())
	}
	return populateJsonFromProtoMonitorResp(protoMessage.GetResponse())
}

func MonitorRequestPbToJson(pbMonitorReq *pb.MonitorRequestMessage) string {
	return populateJsonFromProtoMonitorReq(pbMonitorReq)
}

func MonitorResponsePbToJson(pbMonitorResp *pb.MonitorResponseMessage) string {
	return populateJsonFromProtoMonitorResp(pbMonitorResp)
}

func MonitorStreamPbToJson(pbMonitorStream *pb.MonitorStreamMessage) string {
	return populateJsonFromProtoMonitorStream(pbMonitorStream)
}

func MonitorRequestJsonToPb(vssMonitorReq string) *pb.MonitorRequestMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssMonitorReq), &m); err != nil {
		Error.Printf("MonitorRequestJsonToPb:Unmarshal error data=%s, err=%s", vssMonitorReq, err)
		return nil
	}
	pbMonitorRequestMessage := &pb.MonitorRequestMessage{}
	createMonitorRequestPb(pbMonitorRequestMessage, m)
	return pbMonitorRequestMessage
}

func MonitorResponseJsonToPb(vssMonitorResp string) *pb.MonitorResponseMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssMonitorResp), &m); err != nil {
		Error.Printf("MonitorResponseJsonToPb:Unmarshal error data=%s, err=%s", vssMonitorResp, err)
		return nil
	}
	pbMonitorResponseMessage := &pb.MonitorResponseMessage{}
	createMonitorResponsePb(pbMonitorResponseMessage, m)
	return pbMonitorResponseMessage
}

func MonitorStreamJsonToPb(vssMonitorStream string) *pb.MonitorStreamMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssMonitorStream), &m); err != nil {
		Error.Printf("MonitorStreamJsonToPb:Unmarshal error data=%s, err=%s", vssMonitorStream, err)
		return nil
	}
	pbMonitorStreamMessage := &pb.MonitorStreamMessage{}
	createMonitorStreamPb(pbMonitorStreamMessage, m)
	return pbMonitorStreamMessage
}

// -- cancel -------------------------------------------------------------

func createCancelRequestPb(protoMessage *pb.CancelRequestMessage, m map[string]interface{}) {
	if sid, ok := mapString(m, "serviceId"); ok {
		protoMessage.ServiceId = sid
	}
}

func createCancelResponsePb(protoMessage *pb.CancelResponseMessage, m map[string]interface{}) {
	if status, ok := mapString(m, "status"); ok {
		protoMessage.Status = serviceStatusFromString(status)
	}
	if outdataField, present := m["outdata"]; present {
		protoMessage.Outdata = createServiceOutdataWrapperPb(outdataField)
	}
	if errMap, ok := asMap(m["error"]); ok {
		protoMessage.Error = createServiceErrorPb(errMap)
	}
	if sid, ok := mapString(m, "serviceId"); ok {
		protoMessage.ServiceId = sid
	}
	if ts, ok := mapString(m, "ts"); ok {
		protoMessage.Ts = ts
	}
}

func populateJsonFromProtoCancelReq(protoMessage *pb.CancelRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"cancel"`
	jsonMessage += `,"serviceId":"` + jsonEscape(protoMessage.GetServiceId()) + `"`
	return jsonMessage + "}"
}

func populateJsonFromProtoCancelResp(protoMessage *pb.CancelResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"cancel"`
	jsonMessage += `,"status":"` + protoMessage.GetStatus().String() + `"`
	jsonMessage += getJsonServiceOutdata(protoMessage.GetOutdata())
	jsonMessage += getJsonServiceError(protoMessage.GetError())
	jsonMessage += `,"serviceId":"` + jsonEscape(protoMessage.GetServiceId()) + `"`
	jsonMessage += `,"ts":"` + jsonEscape(protoMessage.GetTs()) + `"`
	return jsonMessage + "}"
}

func CancelRequestPbToJson(pbCancelReq *pb.CancelRequestMessage) string {
	return populateJsonFromProtoCancelReq(pbCancelReq)
}

func CancelResponsePbToJson(pbCancelResp *pb.CancelResponseMessage) string {
	return populateJsonFromProtoCancelResp(pbCancelResp)
}

func CancelRequestJsonToPb(vssCancelReq string) *pb.CancelRequestMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssCancelReq), &m); err != nil {
		Error.Printf("CancelRequestJsonToPb:Unmarshal error data=%s, err=%s", vssCancelReq, err)
		return nil
	}
	pbCancelRequestMessage := &pb.CancelRequestMessage{}
	createCancelRequestPb(pbCancelRequestMessage, m)
	return pbCancelRequestMessage
}

func CancelResponseJsonToPb(vssCancelResp string) *pb.CancelResponseMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssCancelResp), &m); err != nil {
		Error.Printf("CancelResponseJsonToPb:Unmarshal error data=%s, err=%s", vssCancelResp, err)
		return nil
	}
	pbCancelResponseMessage := &pb.CancelResponseMessage{}
	createCancelResponsePb(pbCancelResponseMessage, m)
	return pbCancelResponseMessage
}

// -- discover -------------------------------------------------------------
//
// Per the "Discover" spec revision, discover no longer accepts a "filter" -
// "depth" (mandatory; "0" = all generations) replaces it, and metadata for
// all instances of a multiplexed service is always returned.

func createDiscoverRequestPb(protoMessage *pb.DiscoverRequestMessage, m map[string]interface{}) {
	if path, ok := mapString(m, "path"); ok {
		protoMessage.Path = path
	}
	if depth, ok := mapString(m, "depth"); ok {
		protoMessage.Depth = depth
	}
	if auth, ok := mapString(m, "authorization"); ok {
		protoMessage.Authorization = &auth
	}
	if reqId, ok := mapString(m, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
}

func createDiscoverResponsePb(protoMessage *pb.DiscoverResponseMessage, m map[string]interface{}) {
	if metaMap, ok := asMap(m["metadata"]); ok {
		st, err := structpb.NewStruct(metaMap)
		if err != nil {
			Error.Printf("createDiscoverResponsePb: NewStruct error: %s", err)
		} else {
			protoMessage.Metadata = st
		}
	}
	if errMap, ok := asMap(m["error"]); ok {
		protoMessage.Error = createServiceErrorPb(errMap)
	}
	if reqId, ok := mapString(m, "requestId"); ok {
		protoMessage.RequestId = reqId
	}
	if ts, ok := mapString(m, "ts"); ok {
		protoMessage.Ts = ts
	}
}

func populateJsonFromProtoDiscoverReq(protoMessage *pb.DiscoverRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"discover"`
	jsonMessage += `,"path":"` + jsonEscape(protoMessage.GetPath()) + `"`
	jsonMessage += `,"depth":"` + jsonEscape(protoMessage.GetDepth()) + `"`
	jsonMessage += createJSON(protoMessage.GetAuthorization(), "authorization")
	jsonMessage += createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoDiscoverResp(protoMessage *pb.DiscoverResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"discover"`
	if protoMessage.GetMetadata() != nil {
		if b, err := json.Marshal(protoMessage.GetMetadata().AsMap()); err == nil {
			jsonMessage += `,"metadata":` + string(b)
		}
	}
	jsonMessage += getJsonServiceError(protoMessage.GetError())
	jsonMessage += createJSON(protoMessage.GetRequestId(), "requestId")
	jsonMessage += `,"ts":"` + jsonEscape(protoMessage.GetTs()) + `"`
	return jsonMessage + "}"
}

func DiscoverRequestPbToJson(pbDiscoverReq *pb.DiscoverRequestMessage) string {
	return populateJsonFromProtoDiscoverReq(pbDiscoverReq)
}

func DiscoverResponsePbToJson(pbDiscoverResp *pb.DiscoverResponseMessage) string {
	return populateJsonFromProtoDiscoverResp(pbDiscoverResp)
}

func DiscoverRequestJsonToPb(vssDiscoverReq string) *pb.DiscoverRequestMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssDiscoverReq), &m); err != nil {
		Error.Printf("DiscoverRequestJsonToPb:Unmarshal error data=%s, err=%s", vssDiscoverReq, err)
		return nil
	}
	pbDiscoverRequestMessage := &pb.DiscoverRequestMessage{}
	createDiscoverRequestPb(pbDiscoverRequestMessage, m)
	return pbDiscoverRequestMessage
}

func DiscoverResponseJsonToPb(vssDiscoverResp string) *pb.DiscoverResponseMessage {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(vssDiscoverResp), &m); err != nil {
		Error.Printf("DiscoverResponseJsonToPb:Unmarshal error data=%s, err=%s", vssDiscoverResp, err)
		return nil
	}
	pbDiscoverResponseMessage := &pb.DiscoverResponseMessage{}
	createDiscoverResponsePb(pbDiscoverResponseMessage, m)
	return pbDiscoverResponseMessage
}
