/**
* (C) 2021 Geotab
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package utils

import (
	"encoding/json"
	pb "github.com/covesa/vissr/protobuf/protoc-out"
	"github.com/golang/protobuf/proto"
	"strconv"
)

//var currentCompression Compression

func ProtobufToJson(serialisedMessage []byte, compression Compression) string {
	currentCompression = compression
	protoMessage := &pb.ProtobufMessage{}
	err := proto.Unmarshal(serialisedMessage, protoMessage)
	if err != nil {
		Error.Printf("Unmarshaling error: ", err)
		return ""
	}
	jsonMessage := populateJsonFromProto(protoMessage)
	return jsonMessage
}

func JsonToProtobuf(jsonMessage string, compression Compression) []byte {
	currentCompression = compression
	var protoMessage *pb.ProtobufMessage
	protoMessage = populateProtoFromJson(jsonMessage)
	serialisedMessage, err := proto.Marshal(protoMessage)
	if err != nil {
		Error.Printf("Marshaling error: ", err)
		return nil
	}
	return serialisedMessage
}

func ExtractSubscriptionId(jsonSubResponse string) string {
	var subResponseMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonSubResponse), &subResponseMap)
	if err != nil {
		Error.Printf("ExtractSubscriptionId:Unmarshal error response=%s, err=%s", jsonSubResponse, err)
		return ""
	}
	return subResponseMap["subscriptionId"].(string)
}

func populateProtoFromJson(jsonMessage string) *pb.ProtobufMessage {
	protoMessage := &pb.ProtobufMessage{}
	var messageMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonMessage), &messageMap)
	if err != nil {
		Error.Printf("populateProtoFromJson:Unmarshal error data=%s, err=%s", jsonMessage, err)
		return nil
	}
	mMethod, mType := getMethodAndType(messageMap)
	if mMethod == -1 {
		Error.Printf("Unknown message format=%s", jsonMessage)
		return nil
	}
	protoMessage.Method = mMethod
	switch mMethod {
	case pb.MessageMethod_GET:
		createGetPb(protoMessage, messageMap, mType)
	case pb.MessageMethod_SET:
		createSetPb(protoMessage, messageMap, mType)
	case pb.MessageMethod_SUBSCRIBE:
		createSubscribePb(protoMessage, messageMap, mType)
	case pb.MessageMethod_UNSUBSCRIBE:
		createUnSubscribePb(protoMessage, messageMap, mType)
	}
	return protoMessage
}

func getMethodAndType(messageMap map[string]interface{}) (pb.MessageMethod, pb.MessageType) {
	mType := pb.MessageType_REQUEST
	switch messageMap["action"].(string) {
	case "get":
		if messageMap["path"] == nil {
			mType = pb.MessageType_RESPONSE
		}
		return pb.MessageMethod_GET, mType
	case "set":
		if messageMap["path"] == nil {
			mType = pb.MessageType_RESPONSE
		}
		return pb.MessageMethod_SET, mType
	case "subscribe":
		if messageMap["path"] == nil {
			mType = pb.MessageType_RESPONSE
		}
		return pb.MessageMethod_SUBSCRIBE, mType
	case "unsubscribe":
		if messageMap["ts"] != nil {
			mType = pb.MessageType_RESPONSE
		}
		return pb.MessageMethod_UNSUBSCRIBE, mType
	case "subscription":
		return pb.MessageMethod_SUBSCRIBE, pb.MessageType_NOTIFICATION
	}
	return -1, -1
}

func createGetPb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Get = &pb.GetMessage{}
	protoMessage.Get.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		createGetRequest_Pb(protoMessage, messageMap)
	case pb.MessageType_RESPONSE:
		createGetResponse_Pb(protoMessage, messageMap)
	}
}

func createGetRequest_Pb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Get.Request = &pb.GetMessage_RequestMessage{}
	protoMessage.Get.Request.Path = messageMap["path"].(string)
	if messageMap["filter"] != nil {
		filter := messageMap["filter"]
		switch vv := filter.(type) {
		case []interface{}:
			Info.Println(filter, "is an array:, len=", strconv.Itoa(len(vv)))
			if len(vv) != 2 {
				Error.Printf("Max two filter expressions are allowed.")
				break
			}
			protoMessage.Get.Request.Filter = &pb.FilterExpressions{}
			protoMessage.Get.Request.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 2)
			protoMessage.Get.Request.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			protoMessage.Get.Request.Filter.FilterExp[1] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter_pb(0, vv[0].(map[string]interface{}), protoMessage)
			createPbFilter_pb(1, vv[1].(map[string]interface{}), protoMessage)
		case map[string]interface{}:
			Info.Println(vv, "is a map:")
			protoMessage.Get.Request.Filter = &pb.FilterExpressions{}
			protoMessage.Get.Request.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 1)
			protoMessage.Get.Request.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter_pb(0, vv, protoMessage)
		default:
			Info.Println(filter, "is of an unknown type")
		}
	}
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Get.Request.Authorization = &auth
	}
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.Get.Request.RequestId = &reqId
	}
}

func createGetResponse_Pb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Get.Response = &pb.GetMessage_ResponseMessage{}
	requestId := messageMap["requestId"].(string)
	protoMessage.Get.Response.RequestId = &requestId
	ts := messageMap["ts"].(string)
	if currentCompression == PB_LEVEL1 {
		protoMessage.Get.Response.Ts = &ts
	} else {
		tsc := CompressTS(ts)
		protoMessage.Get.Response.TsC = &tsc
	}
	if messageMap["error"] == nil {
		protoMessage.Get.Response.Status = pb.ResponseStatus_SUCCESS
		protoMessage.Get.Response.SuccessResponse = &pb.GetMessage_ResponseMessage_SuccessResponseMessage{}
		numOfDataElements := getNumOfDataElements_pb(messageMap["data"])
		if numOfDataElements > 0 {
			protoMessage.Get.Response.SuccessResponse.DataPack = &pb.DataPackages{}
			protoMessage.Get.Response.SuccessResponse.DataPack.Data = make([]*pb.DataPackages_DataPackage, numOfDataElements)
			for i := 0; i < numOfDataElements; i++ {
				protoMessage.Get.Response.SuccessResponse.DataPack.Data[i] = createDataElement_pb(i, messageMap["data"])
			}
		} else {
			metadata, _ := json.Marshal(messageMap["metadata"])
			metadataStr := string(metadata)
			protoMessage.Get.Response.SuccessResponse.Metadata = &metadataStr
		}
	} else {
		protoMessage.Get.Response.Status = pb.ResponseStatus_ERROR
		//        protoMessage.Get.Response.ErrorResponse = &pb.ErrorResponseMessage{}
		protoMessage.Get.Response.ErrorResponse = getProtoErrorMessage_pb(messageMap["error"].(map[string]interface{}))
	}
}

func getProtoErrorMessage_pb(messageErrorMap map[string]interface{}) *pb.ErrorResponseMessage {
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

func getNumOfDataElements_pb(messageDataMap interface{}) int {
	if messageDataMap == nil {
		return 0
	}
	switch vv := messageDataMap.(type) {
	case []interface{}:
		return len(vv)
	}
	return 1
}

func createDataElement_pb(index int, messageDataMap interface{}) *pb.DataPackages_DataPackage {
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
	numOfDataPointElements := getNumOfDataPointElements_pb(dataObject["dp"])
	protoDataElement.Dp = make([]*pb.DataPackages_DataPackage_DataPoint, numOfDataPointElements)
	for i := 0; i < numOfDataPointElements; i++ {
		protoDataElement.Dp[i] = createDataPointElement_pb(i, dataObject["dp"])
	}
	return &protoDataElement
}

func getNumOfDataPointElements_pb(messageDataPointMap interface{}) int {
	if messageDataPointMap == nil {
		return 0
	}
	switch vv := messageDataPointMap.(type) {
	case []interface{}:
		return len(vv)
	}
	return 1
}

func createDataPointElement_pb(index int, messageDataPointMap interface{}) *pb.DataPackages_DataPackage_DataPoint {
	var dataPointObject map[string]interface{}
	switch vv := messageDataPointMap.(type) {
	case []interface{}:
		dataPointObject = vv[index].(map[string]interface{})
	default:
		dataPointObject = vv.(map[string]interface{})
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

func createPbFilter_pb(index int, filterExpression map[string]interface{}, protoMessage *pb.ProtobufMessage) {
	filterType := getFilterType_pb(filterExpression["type"].(string))
	if protoMessage.Method == pb.MessageMethod_GET {
		protoMessage.Get.Request.Filter.FilterExp[index].FType = filterType
	} else {
		protoMessage.Subscribe.Request.Filter.FilterExp[index].FType = filterType
	}
	if protoMessage.Method == pb.MessageMethod_GET &&
		(filterType == pb.FilterExpressions_FilterExpression_TIMEBASED ||
			filterType == pb.FilterExpressions_FilterExpression_RANGE ||
			filterType == pb.FilterExpressions_FilterExpression_CHANGE ||
			filterType == pb.FilterExpressions_FilterExpression_CURVELOG) {
		Error.Printf("Filter function is not supported for GET requests.")
		return
	}
	if protoMessage.Method == pb.MessageMethod_SUBSCRIBE &&
		(filterType == pb.FilterExpressions_FilterExpression_HISTORY ||
			filterType == pb.FilterExpressions_FilterExpression_METADATA) {
		Error.Printf("Filter function is not supported for SUBSCRIBE requests.")
		return
	}
	if protoMessage.Method == pb.MessageMethod_GET {
		protoMessage.Get.Request.Filter.FilterExp[index].Value = &pb.FilterExpressions_FilterExpression_FilterValue{}
	} else {
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value = &pb.FilterExpressions_FilterExpression_FilterValue{}
	}
	switch filterType {
	case pb.FilterExpressions_FilterExpression_PATHS:
		if protoMessage.Method == pb.MessageMethod_GET {
			protoMessage.Get.Request.Filter.FilterExp[index].Value.ValuePaths =
				&pb.FilterExpressions_FilterExpression_FilterValue_PathsValue{}
			protoMessage.Get.Request.Filter.FilterExp[index].Value.ValuePaths = getPbPathsFilterValue_pb(filterExpression["parameter"])
		} else {
			protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValuePaths =
				&pb.FilterExpressions_FilterExpression_FilterValue_PathsValue{}
			protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValuePaths = getPbPathsFilterValue_pb(filterExpression["parameter"])
		}
	case pb.FilterExpressions_FilterExpression_TIMEBASED:
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueTimebased =
			&pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue{}
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueTimebased =
			getPbTimebasedFilterValue_pb(filterExpression["parameter"].(map[string]interface{}))
	case pb.FilterExpressions_FilterExpression_RANGE:
		rangeLen := getNumOfRangeExpressions_pb(filterExpression["parameter"])
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueRange =
			make([]*pb.FilterExpressions_FilterExpression_FilterValue_RangeValue, rangeLen)
		for i := 0; i < rangeLen; i++ {
			protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueRange[i] =
				getPbRangeFilterValue_pb(i, filterExpression["parameter"])
		}
	case pb.FilterExpressions_FilterExpression_CHANGE:
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueChange =
			&pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue{}
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueChange =
			getPbChangeFilterValue_pb(filterExpression["parameter"].(map[string]interface{}))
	case pb.FilterExpressions_FilterExpression_CURVELOG:
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueCurvelog =
			&pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue{}
		protoMessage.Subscribe.Request.Filter.FilterExp[index].Value.ValueCurvelog =
			getPbCurvelogFilterValue_pb(filterExpression["parameter"].(map[string]interface{}))
	case pb.FilterExpressions_FilterExpression_HISTORY:
		protoMessage.Get.Request.Filter.FilterExp[index].Value.ValueHistory =
			&pb.FilterExpressions_FilterExpression_FilterValue_HistoryValue{}
		protoMessage.Get.Request.Filter.FilterExp[index].Value.ValueHistory.TimePeriod = filterExpression["parameter"].(string)
	case pb.FilterExpressions_FilterExpression_METADATA:
		Warning.Printf("Filter type is not supported by protobuf compression.")
	default:
		Error.Printf("Filter type is unknown.")
	}
}

func getNumOfRangeExpressions_pb(valueMap interface{}) int {
	switch vv := valueMap.(type) {
	case []interface{}:
		return len(vv)
	default:
		return 1
	}
}

func getPbPathsFilterValue_pb(filterValueExpression interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_PathsValue {
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

func getPbTimebasedFilterValue_pb(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue {
	var protoTimebasedValue pb.FilterExpressions_FilterExpression_FilterValue_TimebasedValue
	protoTimebasedValue.Period = filterExpression["period"].(string)
	return &protoTimebasedValue
}

func getPbRangeFilterValue_pb(index int, valueMap interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_RangeValue {
	var protoRangeValue pb.FilterExpressions_FilterExpression_FilterValue_RangeValue
	switch vv := valueMap.(type) {
	case []interface{}:
		rangeObject := vv[index].(map[string]interface{})
		protoRangeValue.LogicOperator = rangeObject["logic-op"].(string)
		protoRangeValue.Boundary = rangeObject["boundary"].(string)
	case map[string]interface{}:
		protoRangeValue.LogicOperator = vv["logic-op"].(string)
		protoRangeValue.Boundary = vv["boundary"].(string)
	default:
		return nil
	}
	return &protoRangeValue
}

func getPbChangeFilterValue_pb(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue {
	var protoChangeValue pb.FilterExpressions_FilterExpression_FilterValue_ChangeValue
	protoChangeValue.LogicOperator = filterExpression["logic-op"].(string)
	protoChangeValue.Diff = filterExpression["diff"].(string)
	return &protoChangeValue
}

func getPbCurvelogFilterValue_pb(filterExpression map[string]interface{}) *pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue {
	var protoCurvelogValue pb.FilterExpressions_FilterExpression_FilterValue_CurvelogValue
	protoCurvelogValue.MaxErr = filterExpression["maxerr"].(string)
	protoCurvelogValue.BufSize = filterExpression["bufsize"].(string)
	return &protoCurvelogValue
}

func getFilterType_pb(filterType string) pb.FilterExpressions_FilterExpression_FilterType {
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
	case "metadata":
		return pb.FilterExpressions_FilterExpression_METADATA
	}
	return pb.FilterExpressions_FilterExpression_METADATA + 100 //undefined filter type
}

func createSubscribePb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Subscribe = &pb.SubscribeMessage{}
	protoMessage.Subscribe.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		createSubscribeRequest_Pb(protoMessage, messageMap)
	case pb.MessageType_RESPONSE:
		createSubscribeResponsePb(protoMessage, messageMap)
	case pb.MessageType_NOTIFICATION:
		createSubscribeNotificationPb(protoMessage, messageMap)
	}
}

func createSubscribeRequest_Pb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Subscribe = &pb.SubscribeMessage{}
	protoMessage.Subscribe.Request = &pb.SubscribeMessage_RequestMessage{}
	protoMessage.Subscribe.Request.Path = messageMap["path"].(string)
	if messageMap["filter"] != nil {
		filter := messageMap["filter"]
		switch vv := filter.(type) {
		case []interface{}:
			Info.Println(filter, "is an array:, len=", strconv.Itoa(len(vv)))
			if len(vv) != 2 {
				Error.Printf("Max two filter expressions are allowed.")
				break
			}
			protoMessage.Subscribe.Request.Filter = &pb.FilterExpressions{}
			protoMessage.Subscribe.Request.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 2)
			protoMessage.Subscribe.Request.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			protoMessage.Subscribe.Request.Filter.FilterExp[1] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter_pb(0, vv[0].(map[string]interface{}), protoMessage)
			createPbFilter_pb(1, vv[1].(map[string]interface{}), protoMessage)
		case map[string]interface{}:
			Info.Println(filter, "is a map:")
			protoMessage.Subscribe.Request.Filter = &pb.FilterExpressions{}
			protoMessage.Subscribe.Request.Filter.FilterExp = make([]*pb.FilterExpressions_FilterExpression, 1)
			protoMessage.Subscribe.Request.Filter.FilterExp[0] = &pb.FilterExpressions_FilterExpression{}
			createPbFilter_pb(0, vv, protoMessage)
		default:
			Info.Println(filter, "is of an unknown type")
		}
	}
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Subscribe.Request.Authorization = &auth
	}
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.Subscribe.Request.RequestId = reqId
	}
}

func createSubscribeResponsePb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Subscribe.Response = &pb.SubscribeMessage_ResponseMessage{}
	protoMessage.Subscribe.Response.SubscriptionId = messageMap["subscriptionId"].(string)
	protoMessage.Subscribe.Response.RequestId = messageMap["requestId"].(string)
	protoMessage.Subscribe.Response.Ts = messageMap["ts"].(string)
	if messageMap["error"] == nil {
		protoMessage.Subscribe.Response.Status = pb.ResponseStatus_SUCCESS
	} else {
		protoMessage.Subscribe.Response.Status = pb.ResponseStatus_ERROR
		protoMessage.Subscribe.Response.ErrorResponse = getProtoErrorMessage_pb(messageMap["error"].(map[string]interface{}))
	}
}

func createSubscribeNotificationPb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Subscribe.Notification = &pb.SubscribeMessage_NotificationMessage{}
	protoMessage.Subscribe.Notification.SubscriptionId = messageMap["subscriptionId"].(string)
	ts := messageMap["ts"].(string)
	if currentCompression == PB_LEVEL1 {
		protoMessage.Subscribe.Notification.Ts = &ts
	} else {
		tsc := CompressTS(ts)
		protoMessage.Subscribe.Notification.TsC = &tsc
	}
	if messageMap["error"] == nil {
		protoMessage.Subscribe.Notification.Status = pb.ResponseStatus_SUCCESS
		protoMessage.Subscribe.Notification.SuccessResponse = &pb.SubscribeMessage_NotificationMessage_SuccessResponseMessage{}
		numOfDataElements := getNumOfDataElements_pb(messageMap["data"])
		protoMessage.Subscribe.Notification.SuccessResponse.DataPack = &pb.DataPackages{}
		protoMessage.Subscribe.Notification.SuccessResponse.DataPack.Data = make([]*pb.DataPackages_DataPackage, numOfDataElements)
		for i := 0; i < numOfDataElements; i++ {
			protoMessage.Subscribe.Notification.SuccessResponse.DataPack.Data[i] = createDataElement_pb(i, messageMap["data"])
		}
	} else {
		protoMessage.Subscribe.Notification.Status = pb.ResponseStatus_ERROR
		//        protoMessage.Subscribe.Notification.ErrorResponse = &pb.ErrorResponseMessage{}
		protoMessage.Subscribe.Notification.ErrorResponse = getProtoErrorMessage_pb(messageMap["error"].(map[string]interface{}))
	}
}

func createSetPb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Set = &pb.SetMessage{}
	protoMessage.Set.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		createSetRequest_Pb(protoMessage, messageMap)
	case pb.MessageType_RESPONSE:
		createSetResponse_Pb(protoMessage, messageMap)
	}
}

func createSetRequest_Pb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Set.Request = &pb.SetMessage_RequestMessage{}
	protoMessage.Set.Request.Path = messageMap["path"].(string)
	protoMessage.Set.Request.Value = messageMap["value"].(string)
	if messageMap["authorization"] != nil {
		auth := messageMap["authorization"].(string)
		protoMessage.Set.Request.Authorization = &auth
	}
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.Set.Request.RequestId = &reqId
	}
}

func createSetResponse_Pb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.Set.Response = &pb.SetMessage_ResponseMessage{}
	requestId := messageMap["requestId"].(string)
	protoMessage.Set.Response.RequestId = &requestId
	protoMessage.Set.Response.Ts = messageMap["ts"].(string)
	if messageMap["error"] == nil {
		protoMessage.Set.Response.Status = pb.ResponseStatus_SUCCESS
	} else {
		protoMessage.Set.Response.Status = pb.ResponseStatus_ERROR
		protoMessage.Set.Response.ErrorResponse = getProtoErrorMessage_pb(messageMap["error"].(map[string]interface{}))
	}
}

func createUnSubscribePb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.UnSubscribe = &pb.UnSubscribeMessage{}
	protoMessage.UnSubscribe.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		createUnSubscribeRequestPb(protoMessage, messageMap)
	case pb.MessageType_RESPONSE:
		createUnSubscribeResponsePb(protoMessage, messageMap)
	}
}

func createUnSubscribeRequestPb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.UnSubscribe.Request = &pb.UnSubscribeMessage_RequestMessage{}
	protoMessage.UnSubscribe.Request.SubscriptionId = messageMap["subscriptionId"].(string)
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.UnSubscribe.Request.RequestId = &reqId
	}
}

func createUnSubscribeResponsePb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}) {
	protoMessage.UnSubscribe.Response = &pb.UnSubscribeMessage_ResponseMessage{}
	protoMessage.UnSubscribe.Response.SubscriptionId = messageMap["subscriptionId"].(string)
	if messageMap["requestId"] != nil {
		reqId := messageMap["requestId"].(string)
		protoMessage.UnSubscribe.Response.RequestId = &reqId
	}
	protoMessage.UnSubscribe.Response.Ts = messageMap["ts"].(string)
	if messageMap["error"] == nil {
		protoMessage.UnSubscribe.Response.Status = pb.ResponseStatus_SUCCESS
	} else {
		protoMessage.UnSubscribe.Response.Status = pb.ResponseStatus_ERROR
		protoMessage.UnSubscribe.Response.ErrorResponse = getProtoErrorMessage_pb(messageMap["error"].(map[string]interface{}))
	}
}

//      *******************************Proto to JSON code ***************************************

func populateJsonFromProto(protoMessage *pb.ProtobufMessage) string {
	jsonMessage := "{"
	switch protoMessage.GetMethod() {
	case 0: // GET
		jsonMessage += `"action":"get"`
		switch protoMessage.GetGet().GetMType() {
		case 0: //REQUEST
			jsonMessage += `,"path":"` + protoMessage.GetGet().GetRequest().GetPath() + `"` + getJsonFilter_pb(protoMessage, 0) +
				getJsonAuthorization(protoMessage, 0, 0) + getJsonTransactionId(protoMessage, 0, 0)
		case 1: // RESPONSE
			if protoMessage.GetGet().GetResponse().GetStatus() == 0 { //SUCCESSFUL
				jsonMessage += getJsonData(protoMessage, 0)

			} else { // ERROR
				jsonMessage += getJsonError_pb(protoMessage, 0)
			}
			if currentCompression == PB_LEVEL1 {
				jsonMessage += `,"ts":"` + protoMessage.GetGet().GetResponse().GetTs() + `"` + getJsonTransactionId(protoMessage, 0, 1)
			} else {
				jsonMessage += `,"ts":"` + DecompressTs(protoMessage.GetGet().GetResponse().GetTsC()) + `"` + getJsonTransactionId(protoMessage, 0, 1)
			}
		}
	case 1: // SET
		jsonMessage += `"action":"set"`
		switch protoMessage.GetSet().GetMType() {
		case 0: //REQUEST
			jsonMessage += `,"path":"` + protoMessage.GetSet().GetRequest().GetPath() + `","value":"` +
				protoMessage.GetSet().GetRequest().GetValue() + getJsonAuthorization(protoMessage, 1, 0) + getJsonTransactionId(protoMessage, 1, 0)
		case 1: // RESPONSE
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetSet().GetMType())
			if protoMessage.GetSet().GetResponse().GetStatus() == 0 { //SUCCESSFUL
				jsonMessage += protoMessage.GetSet().GetResponse().GetTs()
			} else { // ERROR
				jsonMessage += getJsonError_pb(protoMessage, 1)
			}
		}
	case 2: // SUBSCRIBE
		switch protoMessage.GetSubscribe().GetMType() {
		case 0: //REQUEST
			jsonMessage += `"action":"subscribe","path":"` + protoMessage.GetSubscribe().GetRequest().GetPath() + `"` + getJsonFilter_pb(protoMessage, 2) +
				getJsonAuthorization(protoMessage, 2, 0) + getJsonTransactionId(protoMessage, 2, 0)
		case 1: // RESPONSE
			jsonMessage += `"action":"subscribe"`
			if protoMessage.GetSubscribe().GetResponse().GetStatus() != 0 { //ERROR
				jsonMessage += getJsonError_pb(protoMessage, 2)
			}
			jsonMessage += `,"ts":"` + protoMessage.GetSubscribe().GetResponse().GetTs() + `"` + getJsonTransactionId(protoMessage, 2, 1)
		case 2: // NOTIFICATION
			jsonMessage += `"action":"subscription"`
			if protoMessage.GetSubscribe().GetNotification().GetStatus() == 0 { //SUCCESSFUL
				jsonMessage += getJsonData(protoMessage, 2)

			} else { // ERROR
				jsonMessage += getJsonError_pb(protoMessage, 2)
			}
			if currentCompression == PB_LEVEL1 {
				jsonMessage += `,"ts":"` + protoMessage.GetSubscribe().GetNotification().GetTs() + `"` + getJsonTransactionId(protoMessage, 2, 2)
			} else {
				jsonMessage += `,"ts":"` + DecompressTs(protoMessage.GetSubscribe().GetNotification().GetTsC()) + `"` +
					getJsonTransactionId(protoMessage, 2, 2)
			}
		}
	case 3: // UNSUBSCRIBE
		jsonMessage += `"action":"unsubscribe"`
		switch protoMessage.GetUnSubscribe().GetMType() {
		case 0: //REQUEST
			jsonMessage += getJsonTransactionId(protoMessage, 3, 0)
		case 1: // RESPONSE
			if protoMessage.GetUnSubscribe().GetResponse().GetStatus() == 0 { //SUCCESSFUL
				jsonMessage += getJsonTransactionId(protoMessage, 3, 1)
			} else { // ERROR
				jsonMessage += getJsonError_pb(protoMessage, 3) + getJsonTransactionId(protoMessage, 3, 1)
			}
			jsonMessage += `,"ts":"` + protoMessage.GetUnSubscribe().GetResponse().GetTs() + `"`
		}
	}
	return jsonMessage + "}"
}

func getJsonFilter_pb(protoMessage *pb.ProtobufMessage, mMethod pb.MessageMethod) string {
	var filterExp []*pb.FilterExpressions_FilterExpression
	switch mMethod {
	case 0: // GET
		if protoMessage.GetGet().GetRequest().GetFilter() == nil {
			return ""
		}
		filterExp = protoMessage.GetGet().GetRequest().GetFilter().GetFilterExp()
	case 2: // SUBSCRIBE
		if protoMessage.GetSubscribe().GetRequest().GetFilter() == nil {
			return ""
		}
		filterExp = protoMessage.GetSubscribe().GetRequest().GetFilter().GetFilterExp()
	}
	fType := ""
	value := ""
	switch filterExp[0].GetFType() {
	case 0:
		fType = "paths"
		value = getJsonFilterValuePaths_pb(filterExp[0])
	case 1:
		fType = "timebased"
		value = getJsonFilterValueTimebased_pb(filterExp[0])
	case 2:
		fType = "range"
		value = getJsonFilterValueRange_pb(filterExp[0])
	case 3:
		fType = "change"
		value = getJsonFilterValueChange_pb(filterExp[0])
	case 4:
		fType = "curvelog"
		value = getJsonFilterValueCurvelog_pb(filterExp[0])
	case 5:
		fType = "history"
		value = getJsonFilterValueHistory_pb(filterExp[0])
	case 6:
		fType = "metadata"
		value = getJsonFilterValueMetadata_pb(filterExp[0])
	}
	return `,"filter":{"type":"` + fType + `","parameter":` + value + `}`
}

func getJsonFilterValuePaths_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
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

func getJsonFilterValueTimebased_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
	period := filterExp.GetValue().GetValueTimebased().GetPeriod()
	return `{"period":"` + period + `"}`
}

func getJsonFilterValueRange_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
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

func getJsonFilterValueChange_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
	logicOperator := filterExp.GetValue().GetValueChange().GetLogicOperator()
	diff := filterExp.GetValue().GetValueChange().GetDiff()
	return `{"logic-op":"` + logicOperator + `","diff":"` + diff + `"}`
}

func getJsonFilterValueCurvelog_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
	maxErr := filterExp.GetValue().GetValueCurvelog().GetMaxErr()
	bufSize := filterExp.GetValue().GetValueCurvelog().GetBufSize()
	return `{"maxerr":"` + maxErr + `","bufsize":"` + bufSize + `"}`
}

func getJsonFilterValueHistory_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
	timePeriod := filterExp.GetValue().GetValueHistory().GetTimePeriod()
	return `"` + timePeriod + `"`
}

func getJsonFilterValueMetadata_pb(filterExp *pb.FilterExpressions_FilterExpression) string {
	tree := filterExp.GetValue().GetValueMetadata().GetTree()
	return tree
}

func getJsonAuthorization(protoMessage *pb.ProtobufMessage, mMethod pb.MessageMethod, mType pb.MessageType) string {
	authorization := ""
	value := ""
	switch mMethod {
	case 0: // GET
		switch mType {
		case 0: // REQUEST
			value = protoMessage.GetGet().GetRequest().GetAuthorization()
		}
	case 1: // SET
		switch mType {
		case 0: // REQUEST
			value = protoMessage.GetSet().GetRequest().GetAuthorization()
		}
	case 2: // SUBSCRIBE
		switch mType {
		case 0: // REQUEST
			value = protoMessage.GetSubscribe().GetRequest().GetAuthorization()
		}
	}
	if len(value) > 0 {
		authorization = `,"authorization":"` + value + `"`
	}
	return authorization
}

func getJsonTransactionId(protoMessage *pb.ProtobufMessage, mMethod pb.MessageMethod, mType pb.MessageType) string {
	transactionId := ""
	requestId := ""
	subscriptionId := ""
	switch mMethod {
	case 0: // GET
		switch mType {
		case 0: // REQUEST
			requestId = protoMessage.GetGet().GetRequest().GetRequestId()
		case 1: // RESPONSE
			requestId = protoMessage.GetGet().GetResponse().GetRequestId()
		}
	case 1: // SET
		switch mType {
		case 0: // REQUEST
			requestId = protoMessage.GetSet().GetRequest().GetRequestId()
		case 1: // RESPONSE
			requestId = protoMessage.GetSet().GetResponse().GetRequestId()
		}
	case 2: // SUBSCRIBE
		switch mType {
		case 0: // REQUEST
			requestId = protoMessage.GetSubscribe().GetRequest().GetRequestId()
		case 1: // RESPONSE
			subscriptionId = protoMessage.GetSubscribe().GetResponse().GetSubscriptionId()
			requestId = protoMessage.GetSubscribe().GetResponse().GetRequestId()
		case 2: // NOTIFICATION
			subscriptionId = protoMessage.GetSubscribe().GetNotification().GetSubscriptionId()
		}
	case 3: // UNSUBSCRIBE
		switch mType {
		case 0: // REQUEST
			subscriptionId = protoMessage.GetUnSubscribe().GetRequest().GetSubscriptionId()
			requestId = protoMessage.GetUnSubscribe().GetRequest().GetRequestId()
		case 1: // RESPONSE
			subscriptionId = protoMessage.GetUnSubscribe().GetResponse().GetSubscriptionId()
			requestId = protoMessage.GetUnSubscribe().GetResponse().GetRequestId()
		}
	}
	if len(subscriptionId) > 0 {
		transactionId += `,"subscriptionId":"` + subscriptionId + `"`
	}
	if len(requestId) > 0 {
		transactionId += `,"requestId":"` + requestId + `"`
	}
	return transactionId
}

func getJsonData(protoMessage *pb.ProtobufMessage, mMethod pb.MessageMethod) string {
	data := ""
	var dataPack []*pb.DataPackages_DataPackage
	switch mMethod {
	case 0: // GET
		dataPack = protoMessage.GetGet().GetResponse().GetSuccessResponse().GetDataPack().GetData()
	case 2: // SUBSCRIBE
		dataPack = protoMessage.GetSubscribe().GetNotification().GetSuccessResponse().GetDataPack().GetData()
	}
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
		dp := getJsonDp_pb(dataPack[i])
		data += `{"path":"` + path + `","dp":` + dp + `},`
	}
	data = data[:len(data)-1]
	if len(dataPack) > 1 {
		data += "]"
	}
	return `,"data":` + data
}

func getJsonDp_pb(dataPack *pb.DataPackages_DataPackage) string {
	dpPack := dataPack.GetDp()
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

func getJsonError_pb(protoMessage *pb.ProtobufMessage, mMethod pb.MessageMethod) string {
	var errorResponse *pb.ErrorResponseMessage
	switch mMethod {
	case 0: // GET
		errorResponse = protoMessage.GetGet().GetResponse().GetErrorResponse()
	case 1: // SET
		errorResponse = protoMessage.GetSet().GetResponse().GetErrorResponse()
	case 2: // SUBSCRIBE
		errorResponse = protoMessage.GetSubscribe().GetResponse().GetErrorResponse()
	case 3: // UNSUBSCRIBE
		errorResponse = protoMessage.GetUnSubscribe().GetResponse().GetErrorResponse()
	}
	number := errorResponse.GetNumber()
	reason := errorResponse.GetReason()
	message := errorResponse.GetMessage()
	return `,"error":{"number":"` + number + `","reason":"` + reason + `","message":"` + message + `"}`
}

// *******************************Only for testing during dev ***************************************
func testPrintProtoMessage(protoMessage *pb.ProtobufMessage) {
	switch protoMessage.GetMethod() {
	case 0: // GET
		switch protoMessage.GetGet().GetMType() {
		case 0: //REQUEST
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetGet().GetMType())
			Info.Printf("protoMessage.Get.Request.Path = %s", protoMessage.GetGet().GetRequest().GetPath())
			Info.Printf("protoMessage.Get.Request.RequestId = %s", protoMessage.GetGet().GetRequest().GetRequestId())
		case 1: // RESPONSE
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetGet().GetMType())
			Info.Printf("protoMessage.Get.Response.Status=%d", protoMessage.GetGet().GetResponse().GetStatus())
			Info.Printf("protoMessage.Get.Response.RequestId = %s", protoMessage.GetGet().GetResponse().GetRequestId())
			Info.Printf("protoMessage.Get.Response.Ts = %s", protoMessage.GetGet().GetResponse().GetTs())
		}
	case 1: // SET
		switch protoMessage.GetSet().GetMType() {
		case 0: //REQUEST
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetSet().GetMType())
			Info.Printf("protoMessage.Set.Request.Path = %s", protoMessage.GetSet().GetRequest().GetPath())
			Info.Printf("protoMessage.Set.Request.RequestId = %s", protoMessage.GetSet().GetRequest().GetRequestId())
		case 1: // RESPONSE
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetSet().GetMType())
			Info.Printf("protoMessage.Set.Response.Status=%d", protoMessage.GetSet().GetResponse().GetStatus())
			Info.Printf("protoMessage.Set.Response.RequestId = %s", protoMessage.GetSet().GetResponse().GetRequestId())
			Info.Printf("protoMessage.Set.Response.Ts = %s", protoMessage.GetSet().GetResponse().GetTs())
		}
	case 2: // SUBSCRIBE
		switch protoMessage.GetSubscribe().GetMType() {
		case 0: //REQUEST
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetSubscribe().GetMType())
			Info.Printf("protoMessage.Subscribe.Request.Path = %s", protoMessage.GetSubscribe().GetRequest().GetPath())
			Info.Printf("protoMessage.Subscribe.Request.RequestId = %s", protoMessage.GetSubscribe().GetRequest().GetRequestId())
		case 1: // RESPONSE
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetSubscribe().GetMType())
			Info.Printf("protoMessage.Subscribe.Response.Status=%d", protoMessage.GetSubscribe().GetResponse().GetStatus())
			Info.Printf("protoMessage.Subscribe.Response.RequestId = %s", protoMessage.GetSubscribe().GetResponse().GetRequestId())
			Info.Printf("protoMessage.Subscribe.Response.SubscriptionId = %s", protoMessage.GetSubscribe().GetResponse().GetSubscriptionId())
			Info.Printf("protoMessage.Subscribe.Response.Ts = %s", protoMessage.GetSubscribe().GetResponse().GetTs())
		case 2: // NOTIFICATION
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetSubscribe().GetMType())
			Info.Printf("protoMessage.Subscribe.Notification.Status=%d", protoMessage.GetSubscribe().GetNotification().GetStatus())
			Info.Printf("protoMessage.Subscribe.Notification.SubscriptionId = %s", protoMessage.GetSubscribe().GetNotification().GetSubscriptionId())
			Info.Printf("protoMessage.Subscribe.Notification.Ts = %s", protoMessage.GetSubscribe().GetNotification().GetTs())
		}
	case 3: // UNSUBSCRIBE
		switch protoMessage.GetUnSubscribe().GetMType() {
		case 0: //REQUEST
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetUnSubscribe().GetMType())
			Info.Printf("protoMessage.UnSubscribe.Request.SubscriptionId = %s", protoMessage.GetUnSubscribe().GetRequest().GetSubscriptionId())
			Info.Printf("protoMessage.UnSubscribe.Request.RequestId = %s", protoMessage.GetUnSubscribe().GetRequest().GetRequestId())
		case 1: // RESPONSE
			Info.Printf("protoMessage.Method = %d, protoMessage.Get.MType=%d", protoMessage.GetMethod(), protoMessage.GetUnSubscribe().GetMType())
			Info.Printf("protoMessage.UnSubscribe.Response.Status=%d", protoMessage.GetUnSubscribe().GetResponse().GetStatus())
			Info.Printf("protoMessage.UnSubscribe.Response.SubscriptionId = %s", protoMessage.GetUnSubscribe().GetResponse().GetSubscriptionId())
			Info.Printf("protoMessage.UnSubscribe.Response.RequestId = %s", protoMessage.GetUnSubscribe().GetResponse().GetRequestId())
			Info.Printf("protoMessage.UnSubscribe.Response.Ts = %s", protoMessage.GetUnSubscribe().GetResponse().GetTs())
		}
	}
}
