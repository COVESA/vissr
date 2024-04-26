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
	pb "github.com/covesa/vissr/grpc_pb"
	"strconv"
)

var currentCompression Compression

func GetRequestPbToJson(pbGetReq *pb.GetRequestMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoGetReq(pbGetReq)
	return jsonMessage
}

func GetResponsePbToJson(pbGetResp *pb.GetResponseMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoGetResp(pbGetResp)
	return jsonMessage
}

func GetRequestJsonToPb(vssGetReq string, compression Compression) *pb.GetRequestMessage {
	currentCompression = compression
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

func GetResponseJsonToPb(vssGetResp string, compression Compression) *pb.GetResponseMessage {
	currentCompression = compression

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

func SetRequestPbToJson(pbSetReq *pb.SetRequestMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoSetReq(pbSetReq)
	return jsonMessage
}

func SetResponsePbToJson(pbSetResp *pb.SetResponseMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoSetResp(pbSetResp)
	return jsonMessage
}

func SetRequestJsonToPb(vssSetReq string, compression Compression) *pb.SetRequestMessage {
	currentCompression = compression
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

func SetResponseJsonToPb(vssSetResp string, compression Compression) *pb.SetResponseMessage {
	currentCompression = compression
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

func SubscribeRequestPbToJson(pbSubscribeReq *pb.SubscribeRequestMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoSubscribeReq(pbSubscribeReq)
	return jsonMessage
}

func SubscribeStreamPbToJson(pbSubscribeResp *pb.SubscribeStreamMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoSubscribeStream(pbSubscribeResp)
	return jsonMessage
}

func SubscribeRequestJsonToPb(vssSubscribeReq string, compression Compression) *pb.SubscribeRequestMessage {
	currentCompression = compression
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

func SubscribeStreamJsonToPb(vssSubscribeStream string, compression Compression) *pb.SubscribeStreamMessage {
	currentCompression = compression
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

func UnsubscribeRequestPbToJson(pbUnsubscribeReq *pb.UnsubscribeRequestMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoUnsubscribeReq(pbUnsubscribeReq)
	return jsonMessage
}

func UnsubscribeResponsePbToJson(pbUnsubscribeResp *pb.UnsubscribeResponseMessage, compression Compression) string {
	currentCompression = compression
	jsonMessage := populateJsonFromProtoUnsubscribeResp(pbUnsubscribeResp)
	return jsonMessage
}

func UnsubscribeRequestJsonToPb(vssUnsubscribeReq string, compression Compression) *pb.UnsubscribeRequestMessage {
	currentCompression = compression
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

func UnsubscribeResponseJsonToPb(vssUnsubscribeResp string, compression Compression) *pb.UnsubscribeResponseMessage {
	currentCompression = compression
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

/*func ExtractSubscriptionId(jsonSubResponse string) string {
	var subResponseMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonSubResponse), &subResponseMap)
	if err != nil {
		Error.Printf("ExtractSubscriptionId:Unmarshal error response=%s, err=%s", jsonSubResponse, err)
		return ""
	}
	return subResponseMap["subscriptionId"].(string)
}*/

func createGetRequestPb(protoMessage *pb.GetRequestMessage, messageMap map[string]interface{}) {
	path := messageMap["path"].(string)
	protoMessage.Path = path
	if messageMap["filter"] != nil {
		filter := messageMap["filter"]
		switch vv := filter.(type) {
		case []interface{}:
			Info.Println(filter, "is an array:, len=", strconv.Itoa(len(vv)))
			if len(vv) != 2 {
				Error.Printf("Max two filter expressions are allowed.")
				break
			}
			protoMessage.Filter = &pb.FilterExpressions{}
			protoMessage.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 2)
			protoMessage.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			protoMessage.Filter.FilterExp[1] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter(0, vv[0].(map[string]interface{}), protoMessage.Filter)
			createPbFilter(1, vv[1].(map[string]interface{}), protoMessage.Filter)
		case map[string]interface{}:
			Info.Println(vv, "is a map:")
			protoMessage.Filter = &pb.FilterExpressions{}
			protoMessage.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 1)
			protoMessage.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter(0, vv, protoMessage.Filter)
		default:
			Info.Println(filter, "is of an unknown type")
		}
	}
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Authorization = &auth
	}
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.RequestId = &reqId
	}
}

func createGetResponsePb(protoMessage *pb.GetResponseMessage, messageMap map[string]interface{}) {
	requestId := messageMap["requestId"].(string)
	protoMessage.RequestId = &requestId
	ts := messageMap["ts"].(string)
	if currentCompression == PB_LEVEL1 {
		protoMessage.Ts = &ts
	} else {
		tsc := CompressTS(ts)
		protoMessage.TsC = &tsc
	}
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Authorization = &auth
	}
	if messageMap["error"] == nil {
		protoMessage.Status = pb.ResponseStatus_SUCCESS
		protoMessage.SuccessResponse = &pb.GetResponseMessage_SuccessResponseMessage{}
		numOfDataElements := getNumOfDataElements(messageMap["data"])
		if numOfDataElements > 0 {
			protoMessage.SuccessResponse.DataPack = &pb.DataPackages{}
			protoMessage.SuccessResponse.DataPack.Data = make([]*pb.DataPackages_DataPackage, numOfDataElements)
			for i := 0; i < numOfDataElements; i++ {
				protoMessage.SuccessResponse.DataPack.Data[i] = createDataElement(i, messageMap["data"])
			}
		} else {
			metadata, _ := json.Marshal(messageMap["metadata"])
			metadataStr := string(metadata)
			protoMessage.SuccessResponse.Metadata = &metadataStr
		}
	} else {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = getProtoErrorMessage(messageMap["error"].(map[string]interface{}))
	}
}

func getProtoErrorMessage(messageErrorMap map[string]interface{}) *pb.ErrorResponseMessage {
	protoErrorMessage := &pb.ErrorResponseMessage{}
	for k, v := range messageErrorMap {
		//Info.Println("key=",k, "v=", v)
		if k == "number" {
			protoErrorMessage.Number = v.(string)
		}
		if k == "reason" {
			reason := v.(string)
			protoErrorMessage.Reason = &reason
		}
		if k == "message" {
			message := v.(string)
			protoErrorMessage.Message = &message
		}
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

func createDataElement(index int, messageDataMap interface{}) *pb.DataPackages_DataPackage {

	var dataObject map[string]interface{}
	switch vv := messageDataMap.(type) {
	case []interface{}:
		dataObject = vv[index].(map[string]interface{})
	default:
		dataObject = vv.(map[string]interface{})
	}
	var protoDataElement pb.DataPackages_DataPackage
	path := dataObject["path"].(string)
	if currentCompression == PB_LEVEL1 {
		protoDataElement.Path = &path
	} else {
		protoDataElement.PathC = CompressPath(path)
	}
	numOfDataPointElements := getNumOfDataPointElements(dataObject["dp"])
	protoDataElement.Dp = make([]*pb.DataPackages_DataPackage_DataPoint, numOfDataPointElements)
	for i := 0; i < numOfDataPointElements; i++ {
		protoDataElement.Dp[i] = createDataPointElement(i, dataObject["dp"])
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

func createDataPointElement(index int, messageDataPointMap any) *pb.DataPackages_DataPackage_DataPoint {
	var dataPointObject map[string]any
	switch vv := messageDataPointMap.(type) {
	case []any:
		dataPointObject = vv[index].(map[string]any)
	default:
		dataPointObject = vv.(map[string]any)
	}
	var protoDataPointElement pb.DataPackages_DataPackage_DataPoint
	protoDataPointElement.Value = dataPointObject["value"].(string)
	ts := dataPointObject["ts"].(string)
	if currentCompression == PB_LEVEL1 {
		protoDataPointElement.Ts = &ts
	} else {
		tsc := CompressTS(ts)
		protoDataPointElement.TsC = &tsc
	}
	return &protoDataPointElement
}

func createPbFilter(index int, filterExpression map[string]interface{}, filter *pb.FilterExpressions) {
	filterType := getFilterType(filterExpression["type"].(string))
	filter.FilterExp[index].FType = filterType
	filter.FilterExp[index].Value = &pb.FilterExpressions_FilterExpression_FilterValue{}
	switch filterType {
	case pb.FilterExpressions_FilterExpression_PATHS:
		filter.FilterExp[index].Value.ValuePaths = &pb.FilterExpressions_FilterExpression_FilterValue_PathsValue{}
		filter.FilterExp[index].Value.ValuePaths = getPbPathsFilterValue(filterExpression["parameter"])
	case pb.FilterExpressions_FilterExpression_TIMEBASED:
		filter.FilterExp[index].Value.ValueTimebased = &pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue{}
		filter.FilterExp[index].Value.ValueTimebased = getPbTimebasedFilterValue(filterExpression["parameter"].(map[string]interface{}))
	case pb.FilterExpressions_FilterExpression_RANGE:
		rangeLen := getNumOfRangeExpressions(filterExpression["parameter"])
		filter.FilterExp[index].Value.ValueRange = make([]*pb.FilterExpressions_FilterExpression_FilterValue_RangeValue, rangeLen)
		for i := 0; i < rangeLen; i++ {
			filter.FilterExp[index].Value.ValueRange[i] = getPbRangeFilterValue(i, filterExpression["parameter"])
		}
	case pb.FilterExpressions_FilterExpression_CHANGE:
		filter.FilterExp[index].Value.ValueChange = &pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue{}
		filter.FilterExp[index].Value.ValueChange = getPbChangeFilterValue(filterExpression["parameter"].(map[string]interface{}))
	case pb.FilterExpressions_FilterExpression_CURVELOG:
		filter.FilterExp[index].Value.ValueCurvelog = &pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue{}
		filter.FilterExp[index].Value.ValueCurvelog = getPbCurvelogFilterValue(filterExpression["parameter"].(map[string]interface{}))
	case pb.FilterExpressions_FilterExpression_HISTORY:
		filter.FilterExp[index].Value.ValueHistory = &pb.FilterExpressions_FilterExpression_FilterValue_HistoryValue{}
		filter.FilterExp[index].Value.ValueHistory.TimePeriod = filterExpression["parameter"].(string)
	case pb.FilterExpressions_FilterExpression_STATIC_METADATA:
		Warning.Printf("Filter type is not supported by protobuf compression.")
	case pb.FilterExpressions_FilterExpression_DYNAMIC_METADATA:
		filter.FilterExp[index].Value.ValueDynamicMetadata = &pb.FilterExpressions_FilterExpression_FilterValue_DynamicMetadataValue{}
		filter.FilterExp[index].Value.ValueDynamicMetadata.MetadataDomain = filterExpression["parameter"].(string)
	default:
		Error.Printf("Filter type is unknown.")
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
		Info.Println(filterValueExpression, "is a string array:, len=", strconv.Itoa(len(vv)))
		protoPathsValue.RelativePath = make([]string, len(vv))
		for i := 0; i < len(vv); i++ {
			protoPathsValue.RelativePath[i] = vv[i].(string)
		}
	case string:
		Info.Println(filterValueExpression, "is a string:")
		protoPathsValue.RelativePath = make([]string, 1)
		protoPathsValue.RelativePath[0] = vv
	default:
		Info.Println(filterValueExpression, "is of an unknown type")
	}
	return &protoPathsValue
}

func getPbTimebasedFilterValue(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue {
	var protoTimebasedValue pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue
	protoTimebasedValue.Period = filterExpression["period"].(string)
	return &protoTimebasedValue
}

func getPbRangeFilterValue(index int, valueMap interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_RangeValue {
	var protoRangeValue pb.FilterExpressions_FilterExpression_FilterValue_RangeValue
	switch vv := valueMap.(type) {
	case []interface{}:
		rangeObject := vv[index].(map[string]interface{})
		protoRangeValue.LogicOperator = rangeObject["boundary-op"].(string)
		protoRangeValue.Boundary = rangeObject["boundary"].(string)
	case map[string]interface{}:
		protoRangeValue.LogicOperator = vv["boundary-op"].(string)
		protoRangeValue.Boundary = vv["boundary"].(string)
	default:
		return nil
	}
	return &protoRangeValue
}

func getPbChangeFilterValue(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue {
	var protoChangeValue pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue
	protoChangeValue.LogicOperator = filterExpression["logic-op"].(string)
	protoChangeValue.Diff = filterExpression["diff"].(string)
	return &protoChangeValue
}

func getPbCurvelogFilterValue(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue {
	var protoCurvelogValue pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue
	protoCurvelogValue.MaxErr = filterExpression["maxerr"].(string)
	protoCurvelogValue.BufSize = filterExpression["bufsize"].(string)
	return &protoCurvelogValue
}

func getFilterType(filterType string) pb.FilterExpressions_FilterExpression_FilterType {
	switch filterType {
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
	case "static-metadata":
		return pb.FilterExpressions_FilterExpression_STATIC_METADATA
	case "dynamic-metadata":
		return pb.FilterExpressions_FilterExpression_DYNAMIC_METADATA
	}
	return pb.FilterExpressions_FilterExpression_DYNAMIC_METADATA + 100 //undefined filter type
}

func createSubscribeRequestPb(protoMessage *pb.SubscribeRequestMessage, messageMap map[string]interface{}) {
	protoMessage.Path = messageMap["path"].(string)
	if messageMap["filter"] != nil {
		filter := messageMap["filter"]
		switch vv := filter.(type) {
		case []interface{}:
			Info.Println(filter, "is an array:, len=", strconv.Itoa(len(vv)))
			if len(vv) != 2 {
				Error.Printf("Max two filter expressions are allowed.")
				break
			}
			protoMessage.Filter = &pb.FilterExpressions{}
			protoMessage.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 2)
			protoMessage.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			protoMessage.Filter.FilterExp[1] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter(0, vv[0].(map[string]interface{}), protoMessage.Filter)
			createPbFilter(1, vv[1].(map[string]interface{}), protoMessage.Filter)
		case map[string]interface{}:
			Info.Println(filter, "is a map:")
			protoMessage.Filter = &pb.FilterExpressions{}
			protoMessage.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 1)
			protoMessage.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter(0, vv, protoMessage.Filter)
		default:
			Info.Println(filter, "is of an unknown type")
		}
	}
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Authorization = &auth
	}
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.RequestId = reqId
	}
}

func createSubscribeStreamPb(protoMessage *pb.SubscribeStreamMessage, messageMap map[string]interface{}) {
	if messageMap["action"] == "subscribe" { // RESPONSE
		protoMessage.MType = pb.SubscribeResponseType_RESPONSE
		protoMessage.Response = &pb.SubscribeStreamMessage_SubscribeResponseMessage{}
		protoMessage.Response.RequestId = messageMap["requestId"].(string)
		protoMessage.Response.Ts = messageMap["ts"].(string)
		if messageMap["authorization"] != nil {
			auth := messageMap["authorization"].(string)
			protoMessage.Response.Authorization = &auth
		}
		if messageMap["error"] == nil {
			if messageMap["subscriptionId"] != nil {
				subId := messageMap["subscriptionId"].(string)
				protoMessage.Response.SubscriptionId = &subId
			}
			protoMessage.Status = pb.ResponseStatus_SUCCESS
		} else {
			protoMessage.Status = pb.ResponseStatus_ERROR
			protoMessage.Response.ErrorResponse = getProtoErrorMessage(messageMap["error"].(map[string]interface{}))
		}
	} else { //EVENT
		protoMessage.MType = pb.SubscribeResponseType_EVENT
		protoMessage.Event = &pb.SubscribeStreamMessage_SubscribeEventMessage{}
		protoMessage.Event.SubscriptionId = messageMap["subscriptionId"].(string)
		ts := messageMap["ts"].(string)
		if currentCompression == PB_LEVEL1 {
			protoMessage.Event.Ts = &ts
		} else {
			tsc := CompressTS(ts)
			protoMessage.Event.TsC = &tsc
		}
		if messageMap["error"] == nil {
			protoMessage.Status = pb.ResponseStatus_SUCCESS
			protoMessage.Event.SuccessResponse = &pb.SubscribeStreamMessage_SubscribeEventMessage_SuccessResponseMessage{}
			numOfDataElements := getNumOfDataElements(messageMap["data"])
			protoMessage.Event.SuccessResponse.DataPack = &pb.DataPackages{}
			protoMessage.Event.SuccessResponse.DataPack.Data = make([]*pb.DataPackages_DataPackage, numOfDataElements)
			for i := 0; i < numOfDataElements; i++ {
				protoMessage.Event.SuccessResponse.DataPack.Data[i] = createDataElement(i, messageMap["data"])
			}
		} else {
			protoMessage.Status = pb.ResponseStatus_ERROR
			protoMessage.Event.ErrorResponse = getProtoErrorMessage(messageMap["error"].(map[string]interface{}))
		}
	}
}

func createSetRequestPb(protoMessage *pb.SetRequestMessage, messageMap map[string]interface{}) {
	protoMessage.Path = messageMap["path"].(string)
	protoMessage.Value = messageMap["value"].(string)
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Authorization = &auth
	}
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.RequestId = &reqId
	}
}

func createSetResponsePb(protoMessage *pb.SetResponseMessage, messageMap map[string]interface{}) {
	requestId := messageMap["requestId"].(string)
	protoMessage.RequestId = &requestId
	protoMessage.Ts = messageMap["ts"].(string)
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Authorization = &auth
	}
	if messageMap["error"] == nil {
		protoMessage.Status = pb.ResponseStatus_SUCCESS
	} else {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = getProtoErrorMessage(messageMap["error"].(map[string]interface{}))
	}
}

func createUnsubscribeRequestPb(protoMessage *pb.UnsubscribeRequestMessage, messageMap map[string]interface{}) {
	protoMessage.SubscriptionId = messageMap["subscriptionId"].(string)
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.RequestId = &reqId
	}
}

func createUnsubscribeResponsePb(protoMessage *pb.UnsubscribeResponseMessage, messageMap map[string]interface{}) {
	protoMessage.SubscriptionId = messageMap["subscriptionId"].(string)
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.RequestId = &reqId
	}
	protoMessage.Ts = messageMap["ts"].(string)
	if messageMap["error"] == nil {
		protoMessage.Status = pb.ResponseStatus_SUCCESS
	} else {
		protoMessage.Status = pb.ResponseStatus_ERROR
		protoMessage.ErrorResponse = getProtoErrorMessage(messageMap["error"].(map[string]interface{}))
	}
}

// *******************************Proto to JSON code ***************************************
func populateJsonFromProtoGetReq(protoMessage *pb.GetRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"get"`
	jsonMessage += `,"path":"` + protoMessage.GetPath() + `"` + getJsonFilter(protoMessage.Filter) +
		createJSON(protoMessage.GetAuthorization(), "authorization") + createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoGetResp(protoMessage *pb.GetResponseMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"get"`
	if protoMessage.GetStatus() == 0 { //SUCCESSFUL
		jsonMessage += createJsonData(protoMessage.SuccessResponse.GetDataPack().GetData())

	} else { // ERROR
		jsonMessage += getJsonError(protoMessage.GetErrorResponse())
	}
	if currentCompression == PB_LEVEL1 {
		jsonMessage += `,"ts":"` + protoMessage.GetTs() + `"` + createJSON(protoMessage.GetRequestId(), "requestId") + createJSON(protoMessage.GetAuthorization(), "authorization")
	} else {
		jsonMessage += `,"ts":"` + DecompressTs(protoMessage.GetTsC()) + `"` + createJSON(protoMessage.GetRequestId(), "requestId") +
			createJSON(protoMessage.GetAuthorization(), "authorization")
	}
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
	//	if currentCompression == PB_LEVEL1 {
	jsonMessage += `,"ts":"` + protoMessage.GetTs() + `"` + createJSON(protoMessage.GetRequestId(), "requestId") + createJSON(protoMessage.GetAuthorization(), "authorization")
	/*	} else {
		jsonMessage += `,"ts":"` + DecompressTs(protoMessage.GetTsC()) + `"` + createJSON(protoMessage.GetRequestId(), "requestId") +
		createJSON(protoMessage.GetAuthorization(), "authorization")
	}*/
	return jsonMessage + "}"
}

func populateJsonFromProtoSubscribeReq(protoMessage *pb.SubscribeRequestMessage) string {
	jsonMessage := "{"
	jsonMessage += `"action":"subscribe"`
	jsonMessage += `,"path":"` + protoMessage.GetPath() + `"` + getJsonFilter(protoMessage.Filter) +
		createJSON(protoMessage.GetAuthorization(), "authorization") + createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func populateJsonFromProtoSubscribeStream(protoMessage *pb.SubscribeStreamMessage) string {
	jsonMessage := "{"
	switch protoMessage.GetMType() {
	case pb.SubscribeResponseType_RESPONSE:
		jsonMessage += `"action":"subscribe"`
		if protoMessage.GetStatus() != 0 { //ERROR
			jsonMessage += getJsonError(protoMessage.Response.GetErrorResponse())
		}
		jsonMessage += `,"ts":"` + protoMessage.Response.GetTs() + `"` + createJSON(protoMessage.Response.GetSubscriptionId(), "subscriptionId") +
			createJSON(protoMessage.Response.GetRequestId(), "requestId") + createJSON(protoMessage.Response.GetAuthorization(), "authorization")
	case pb.SubscribeResponseType_EVENT:
		jsonMessage += `"action":"subscription"`
		if protoMessage.GetStatus() == 0 { //SUCCESSFUL
			jsonMessage += createJsonData(protoMessage.Event.SuccessResponse.GetDataPack().GetData())
		} else { // ERROR
			jsonMessage += getJsonError(protoMessage.Event.GetErrorResponse())
		}
		if currentCompression == PB_LEVEL1 {
			jsonMessage += `,"ts":"` + protoMessage.Event.GetTs() + `"` + createJSON(protoMessage.Event.GetSubscriptionId(), "subscriptionId")
		} else {
			jsonMessage += `,"ts":"` + DecompressTs(protoMessage.Event.GetTsC()) + `"` +
				createJSON(protoMessage.Event.GetSubscriptionId(), "subscriptionId")
		}
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
	jsonMessage += `,"ts":"` + protoMessage.GetTs() + `"` + createJSON(protoMessage.GetSubscriptionId(), "subscriptionId") + createJSON(protoMessage.GetRequestId(), "requestId")
	return jsonMessage + "}"
}

func getJsonFilter(filter *pb.FilterExpressions) string {
	var filterExp []*pb.FilterExpressions_FilterExpression
	if filter == nil {
		return ""
	}
	filterExp = filter.GetFilterExp()
	jsonFilter := ""
	if len(filterExp) > 1 {
		jsonFilter = "["
	}
	for i := 0; i < len(filterExp); i++ {
		jsonFilter += synthesizeFilter(filterExp[i]) + ","
	}
	jsonFilter = jsonFilter[:len(jsonFilter)-1]
	if len(filterExp) > 1 {
		jsonFilter += "]"
	}
	return `,"filter":` + jsonFilter
}

func synthesizeFilter(filterExp *pb.FilterExpressions_FilterExpression) string {
	fType := ""
	value := ""
	switch filterExp.GetFType() {
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
		fType = "static-metadata"
		value = getJsonFilterValueStaticMetadata(filterExp)
	case 7:
		fType = "dynamic-metadata"
		value = getJsonFilterValueDynamicMetadata(filterExp)
	}
	return `{"type":"` + fType + `","parameter":` + value + `}`
}

func getJsonFilterValuePaths(filterExp *pb.FilterExpressions_FilterExpression) string {
	relativePaths := filterExp.GetValue().GetValuePaths().GetRelativePath()
	value := ""
	if len(relativePaths) > 1 {
		value = "["
	}
	for i := 0; i < len(relativePaths); i++ {
		value += `"` + relativePaths[i] + `",`
	}
	value = value[:len(value)-1]
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
	value := ""
	if len(rangeValue) > 1 {
		value = "["
	}
	for i := 0; i < len(rangeValue); i++ {
		logicOperator := rangeValue[i].GetLogicOperator()
		boundary := rangeValue[i].GetBoundary()
		value += `{"logic-op":"` + logicOperator + `","boundary":"` + boundary + `"},`
	}
	value = value[:len(value)-1]
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

func getJsonFilterValueStaticMetadata(filterExp *pb.FilterExpressions_FilterExpression) string {
	tree := filterExp.GetValue().GetValueStaticMetadata().GetTree()
	return tree
}

func getJsonFilterValueDynamicMetadata(filterExp *pb.FilterExpressions_FilterExpression) string {
	metadataDomain := filterExp.GetValue().GetValueDynamicMetadata().GetMetadataDomain()
	return metadataDomain
}

func createJSON(value string, key string) string {
	if len(value) > 0 {
		return `,"` + key + `":"` + value + `"`
	}
	return ""
}

func createJsonData(dataPack []*pb.DataPackages_DataPackage) string {
	data := ""
Info.Printf("createJsonData:len(dataPack)=%d", len(dataPack))
	if len(dataPack) > 1 {
		data += "["
	}
	for i := 0; i < len(dataPack); i++ {
		var path string
		if currentCompression == PB_LEVEL1 {
			path = dataPack[i].GetPath()
		} else {
			path = DecompressPath(dataPack[i].GetPathC())
		}
Info.Printf("createJsonData:path=%s", path)
		dp := getJsonDp(dataPack[i])
		data += `{"path":"` + path + `","dp":` + dp + `},`
	}
	data = data[:len(data)-1]
	if len(dataPack) > 1 {
		data += "]"
	}
	return `,"data":` + data
}

func getJsonDp(dataPack *pb.DataPackages_DataPackage) string {
	dpPack := dataPack.GetDp()
	//Info(dpPack)
	dp := ""
	if len(dpPack) > 1 {
		dp += "["
	}
	for i := 0; i < len(dpPack); i++ {
		value := dpPack[i].GetValue()
		var ts string
		if currentCompression == PB_LEVEL1 {
			ts = dpPack[i].GetTs()
		} else {
			ts = DecompressTs(dpPack[i].GetTsC())
		}
		dp += `{"value":"` + value + `","ts":"` + ts + `"},`
	}
	dp = dp[:len(dp)-1]
	if len(dpPack) > 1 {
		dp += "]"
	}
	return dp
}

func getJsonError(errorResponse *pb.ErrorResponseMessage) string {
	number := errorResponse.GetNumber()
	reason := errorResponse.GetReason()
	message := errorResponse.GetMessage()
	return `,"error":{"number":"` + number + `","reason":"` + reason + `","message":"` + message + `"}`
}
