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
	pb "github.com/covesa/vissr/grpc_pb"
	"github.com/golang/protobuf/proto"
)

func ProtobufToJson(serialisedMessage []byte) string {
	protoMessage := &pb.ProtobufMessage{}
	err := proto.Unmarshal(serialisedMessage, protoMessage)
	if err != nil {
		Error.Printf("Unmarshaling error:%s, message=%s", err, serialisedMessage)
		return ""
	}
	jsonMessage := populateJsonFromProto(protoMessage)
	return jsonMessage
}

func JsonToProtobuf(jsonMessage string) []byte {
	var protoMessage *pb.ProtobufMessage
	protoMessage = populateProtoFromJson(jsonMessage)
	serialisedMessage, err := proto.Marshal(protoMessage)
	if err != nil {
		Error.Printf("Marshaling error: ", err)
		return nil
	}
	return serialisedMessage
}

func populateJsonFromProto(protoMessage *pb.ProtobufMessage) string {
	switch protoMessage.GetMethod() {
	case pb.MessageMethod_GET:
		switch protoMessage.GetGet().GetMType() {
		case pb.MessageType_REQUEST: return populateJsonFromProtoGetReq(protoMessage.Get.Request)
		case pb.MessageType_RESPONSE: return populateJsonFromProtoGetResp(protoMessage.Get.Response)
		}
	case pb.MessageMethod_SET:
		switch protoMessage.GetSet().GetMType() {
		case pb.MessageType_REQUEST: return populateJsonFromProtoSetReq(protoMessage.Set.Request)
		case pb.MessageType_RESPONSE: return populateJsonFromProtoSetResp(protoMessage.Set.Response)
		}
	case pb.MessageMethod_SUBSCRIBE:
		switch protoMessage.GetSubscribe().GetMType() {
		case pb.MessageType_REQUEST: return populateJsonFromProtoSubscribeReq(protoMessage.Subscribe.Request)
		case pb.MessageType_STREAM:
			switch protoMessage.GetSubscribe().GetStream().GetMType() {
			case pb.SubscribeResponseType_SUB_RESPONSE: return populateJsonFromProtoSubscribeStream(protoMessage.Subscribe.Stream)
			case pb.SubscribeResponseType_SUB_EVENT: return populateJsonFromProtoSubscribeStream(protoMessage.Subscribe.Stream)
			}
		}
	case pb.MessageMethod_UNSUBSCRIBE:
		switch protoMessage.GetUnsubscribe().GetMType() {
		case pb.MessageType_REQUEST: return populateJsonFromProtoUnsubscribeReq(protoMessage.Unsubscribe.Request)
		case pb.MessageType_RESPONSE: return populateJsonFromProtoUnsubscribeResp(protoMessage.Unsubscribe.Response)
		}
	}
	return ""
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
		protoMessage.Get = &pb.GetMessage{}
		createGetPb(protoMessage, messageMap, mType)
	case pb.MessageMethod_SET:
		protoMessage.Set = &pb.SetMessage{}
		createSetPb(protoMessage, messageMap, mType)
	case pb.MessageMethod_SUBSCRIBE:
		protoMessage.Subscribe = &pb.SubscribeMessage{}
		createSubscribePb(protoMessage, messageMap, mType)
	case pb.MessageMethod_UNSUBSCRIBE:
		protoMessage.Unsubscribe = &pb.UnsubscribeMessage{}
		createUnsubscribePb(protoMessage, messageMap, mType)
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
			mType = pb.MessageType_STREAM
		}
		return pb.MessageMethod_SUBSCRIBE, mType
	case "unsubscribe":
		if messageMap["ts"] != nil {
			mType = pb.MessageType_RESPONSE
		}
		return pb.MessageMethod_UNSUBSCRIBE, mType
	case "subscription":
		return pb.MessageMethod_SUBSCRIBE, pb.MessageType_STREAM
	}
	return -1, -1
}

func createGetPb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Get.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		protoMessage.Get.Request = &pb.GetRequestMessage{}
		createGetRequestPb(protoMessage.Get.Request, messageMap)
	case pb.MessageType_RESPONSE:
		protoMessage.Get.Response = &pb.GetResponseMessage{}
		createGetResponsePb(protoMessage.Get.Response, messageMap)
	}
}

func createSetPb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Set.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		protoMessage.Set.Request = &pb.SetRequestMessage{}
		createSetRequestPb(protoMessage.Set.Request, messageMap)
	case pb.MessageType_RESPONSE:
		protoMessage.Set.Response = &pb.SetResponseMessage{}
		createSetResponsePb(protoMessage.Set.Response, messageMap)
	}
}

func createSubscribePb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Subscribe.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		protoMessage.Subscribe.Request = &pb.SubscribeRequestMessage{}
		createSubscribeRequestPb(protoMessage.Subscribe.Request, messageMap)
	case pb.MessageType_STREAM:
		protoMessage.Subscribe.Stream = &pb.SubscribeStreamMessage{}
		createSubscribeStreamPb(protoMessage.Subscribe.Stream, messageMap)
	}
}

func createUnsubscribePb(protoMessage *pb.ProtobufMessage, messageMap map[string]interface{}, mType pb.MessageType) {
	protoMessage.Unsubscribe.MType = mType
	switch mType {
	case pb.MessageType_REQUEST:
		protoMessage.Unsubscribe.Request = &pb.UnsubscribeRequestMessage{}
		createUnsubscribeRequestPb(protoMessage.Unsubscribe.Request, messageMap)
	case pb.MessageType_RESPONSE:
		protoMessage.Unsubscribe.Response = &pb.UnsubscribeResponseMessage{}
		createUnsubscribeResponsePb(protoMessage.Unsubscribe.Response, messageMap)
	}
}
