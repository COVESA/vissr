/**
* (C) 2023 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package grpcMgr

import (
	"context"
	"crypto/tls"
	"encoding/json"
	pb "github.com/covesa/vissr/grpc_pb"
	utils "github.com/covesa/vissr/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"net"
	"strconv"
	"strings"
)

var grpcCompression utils.Compression
var grpcMgrId int
var grpcMgrChan chan string

type GrpcRequestMessage struct {
	VssReq       string
	GrpcRespChan chan string
}

var grpcClientChan = []chan GrpcRequestMessage{
	make(chan GrpcRequestMessage),
}

// array size same as for grpcClientChan
var clientBackendChan = []chan string{
	make(chan string),
}

type Server struct {
	pb.UnimplementedVISSv2Server
}

type GrpcRoutingData struct {
	ClientId         int
	SubscriptionId   string
	GrpcRespChannel  chan string
	IsMultipleEvents bool
}

var grpcRoutingDataList []GrpcRoutingData

const KILL_MESSAGE = "kill subscription"
const MAXGRPCCLIENTS = 50

var grpcClientIndexList []bool

func getClientId() int {
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcClientIndexList[i] == false {
			grpcClientIndexList[i] = true
			return i
		}
	}
	return -1
}

func getGrpcRoutingData(clientId int) (chan string, bool) {
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			return grpcRoutingDataList[i].GrpcRespChannel, grpcRoutingDataList[i].IsMultipleEvents
		}
	}
	return nil, false
}

func updateGrpcRoutingData(clientId int, subscriptionId string) {
	//utils.Info.Printf("updateGrpcRoutingData:clientId=%d, subscriptionId=%s", clientId, subscriptionId)
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			grpcRoutingDataList[i].SubscriptionId = subscriptionId
			break
		}
	}
}

func getSubscribeRoutingData(unsubResp string) (int, chan string) {
	subscriptionId := getSubscriptionId(unsubResp)
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].SubscriptionId == subscriptionId {
			return grpcRoutingDataList[i].ClientId, grpcRoutingDataList[i].GrpcRespChannel
		}
	}
	return -1, nil
}

func resetClientId(clientId int) {
	grpcClientIndexList[clientId] = false
}

func initClientIdList() {
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = false
	}
}

func setGrpcRoutingData(clientId int, grpcRespChan chan string, isMultipleEvent bool) bool {
	//utils.Info.Printf("setGrpcRoutingData:clientId=%d, isMultipleEvent=%d", clientId, isMultipleEvent)
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == -1 {
			grpcRoutingDataList[i].ClientId = clientId
			grpcRoutingDataList[i].GrpcRespChannel = grpcRespChan
			grpcRoutingDataList[i].IsMultipleEvents = isMultipleEvent
			return true
		}
	}
	return false
}

func resetGrpcRoutingData(clientId int) {
	utils.Info.Printf("resetGrpcRoutingData:clientId=%d", clientId)
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			grpcRoutingDataList[i].ClientId = -1
			resetClientId(clientId)
			break
		}
	}
}

func iniGrpcRoutingDataList() {
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcRoutingDataList[i].ClientId = -1
	}
}

func RemoveRoutingForwardResponse(response string) {
	trimmedResponse, clientId := utils.RemoveInternalData(response)
	grpcRespChan, isMultipleEvent := getGrpcRoutingData(clientId)
	if grpcRespChan != nil {
		updateRoutingList(response, clientId, isMultipleEvent)
		grpcRespChan <- trimmedResponse
	} else {
		utils.Error.Printf("Missing clientId=%d entry in gRPC routing data for response=%s", clientId, response)
	}
}

func updateRoutingList(resp string, clientId int, isMultipleEvent bool) {
	utils.Info.Printf("updateRoutingList:message=%s", resp)
	if strings.Contains(resp, "unsubscribe") {
		subscribeClientId, subscribeChan := getSubscribeRoutingData(resp)
		subscribeChan <- KILL_MESSAGE + " clientId:" + strconv.Itoa(subscribeClientId)
		resetGrpcRoutingData(clientId)
		//utils.Info.Printf("updateRoutingList:unsubscribe clientid=%s, subscription clientid=%s", clientId, subscribeClientId)
	} else if !isMultipleEvent { // get and set
		resetGrpcRoutingData(clientId)
	} else if strings.Contains(resp, "subscribe") { // update routing info with subscriptionId
		if !strings.Contains(resp, "subscriptionId") { // error
			resetGrpcRoutingData(clientId)
			return
		}
		updateGrpcRoutingData(clientId, getSubscriptionId(resp))
	}
}

func getSubscriptionId(resp string) string {
	var respMap map[string]interface{}
	err := json.Unmarshal([]byte(resp), &respMap)
	if err != nil {
		utils.Error.Printf("getSubscriptionId:Unmarshal error data=%s, err=%s", resp, err)
		return ""
	}
	return respMap["subscriptionId"].(string)
}

func initGrpcServer() {
	var server *grpc.Server
	var portNo string
	if utils.SecureConfiguration.TransportSec == "yes" {
		cert, err := tls.LoadX509KeyPair(utils.TrSecConfigPath+utils.SecureConfiguration.ServerSecPath+"server.crt", utils.TrSecConfigPath+utils.SecureConfiguration.ServerSecPath+"server.key")
		if err != nil {
			utils.Error.Printf("initGrpcServer:Cannot load server credentials, err=%s", err)
			return
		}

		config := utils.GetTLSConfig("localhost", utils.TrSecConfigPath+utils.SecureConfiguration.CaSecPath+"Root.CA.crt",
			tls.ClientAuthType(utils.CertOptToInt(utils.SecureConfiguration.ServerCertOpt)), &cert)
		tlsCredentials := credentials.NewTLS(config)

		opts := []grpc.ServerOption{
			//		grpc.Creds(credentials.NewServerTLSFromCert(&cert)),
			grpc.Creds(tlsCredentials),
		}
		server = grpc.NewServer(opts...)
		portNo = utils.SecureConfiguration.GrpcSecPort
		utils.Info.Printf("initGrpcServer:port number=%s", portNo)
	} else {
		server = grpc.NewServer(grpc.StatsHandler(&Handler{}))
		portNo = "8887"
		utils.Info.Printf("portNo =%s", portNo)
	}
	pb.RegisterVISSv2Server(server, &Server{})
	for {
		lis, err := net.Listen("tcp", "0.0.0.0:"+portNo)
		if err != nil {
			utils.Error.Printf("failed to listen: " + err.Error())
			break
		}
		err = server.Serve(lis)
		if err != nil {
			utils.Error.Printf("failed to start grpc: " + err.Error())
			break
		}
	}
}

func (s *Server) GetRequest(ctx context.Context, in *pb.GetRequestMessage) (*pb.GetResponseMessage, error) {
	vssReq := utils.GetRequestPbToJson(in, grpcCompression)
	grpcResponseChan := make(chan string)
	var grpcRequestMessage = GrpcRequestMessage{vssReq, grpcResponseChan}
	utils.Info.Println(grpcRequestMessage.VssReq)
	grpcClientChan[0] <- grpcRequestMessage // forward to mgr hub,
	vssResp := <-grpcResponseChan           //  and wait for response
	pbResp := utils.GetResponseJsonToPb(vssResp, grpcCompression)
	return pbResp, nil
}

func (s *Server) SetRequest(ctx context.Context, in *pb.SetRequestMessage) (*pb.SetResponseMessage, error) {
	vssReq := utils.SetRequestPbToJson(in, grpcCompression)
	grpcResponseChan := make(chan string)
	var grpcRequestMessage = GrpcRequestMessage{vssReq, grpcResponseChan}
	grpcClientChan[0] <- grpcRequestMessage // forward to mgr hub,
	vssResp := <-grpcResponseChan           //  and wait for response
	pbResp := utils.SetResponseJsonToPb(vssResp, grpcCompression)
	return pbResp, nil
}

func (s *Server) UnsubscribeRequest(ctx context.Context, in *pb.UnsubscribeRequestMessage) (*pb.UnsubscribeResponseMessage, error) {
	vssReq := utils.UnsubscribeRequestPbToJson(in, grpcCompression)
	grpcResponseChan := make(chan string)
	var grpcRequestMessage = GrpcRequestMessage{vssReq, grpcResponseChan}
	grpcClientChan[0] <- grpcRequestMessage // forward to mgr hub,
	vssResp := <-grpcResponseChan           //  and wait for response
	pbResp := utils.UnsubscribeResponseJsonToPb(vssResp, grpcCompression)
	return pbResp, nil
}

func (s *Server) SubscribeRequest(in *pb.SubscribeRequestMessage, stream pb.VISSv2_SubscribeRequestServer) error {
	vssReq := utils.SubscribeRequestPbToJson(in, grpcCompression)
	grpcResponseChan := make(chan string)
	var grpcRequestMessage = GrpcRequestMessage{vssReq, grpcResponseChan}
	grpcClientChan[0] <- grpcRequestMessage // forward to mgr hub
	subscribeClientId := -1
	for {
		select {
		case <-stream.Context().Done():
			utils.Info.Printf("gRPC subscribe session terminated by client")
			// issue message to servicemgr about subscription termination
			utils.AddRoutingForwardRequest(`{"action":"internal-killsubscriptions"}`, grpcMgrId, subscribeClientId, grpcMgrChan)
			resetGrpcRoutingData(subscribeClientId)
			return nil
		case vssResp := <-grpcResponseChan: //  forward subscribe response and following events
			if strings.Contains(vssResp, KILL_MESSAGE) { //issued by unsubscribe thread
				clientId := extractClientId(vssResp)
				resetGrpcRoutingData(clientId)
				return nil
			}
			if subscribeClientId == -1 {
				subscribeClientId, _ = getSubscribeRoutingData(vssResp)
			}
			pbResp := utils.SubscribeStreamJsonToPb(vssResp, grpcCompression)
			if err := stream.Send(pbResp); err != nil {
				resetGrpcRoutingData(subscribeClientId)
				return err
			}
		}
	}
	return nil
}

func extractClientId(killMessage string) int { // mesage contains clientId:xyz
	delimIndex := strings.Index(killMessage, ":")
	clientId, _ := strconv.Atoi(killMessage[delimIndex+1:])
	return clientId
}

func GrpcMgrInit(mgrId int, transportMgrChan chan string) {
	utils.ReadTransportSecConfig()
	grpcMgrId = mgrId
	grpcMgrChan = transportMgrChan
	grpcClientIndexList = make([]bool, MAXGRPCCLIENTS)
	grpcRoutingDataList = make([]GrpcRoutingData, MAXGRPCCLIENTS)
	grpcCompression = utils.PB_LEVEL1 // set via viss2server command line param?
	iniGrpcRoutingDataList()
	go initGrpcServer()

	utils.Info.Println("gRPC manager data session initiated.")

	for {
		select {
		case respMessage := <-transportMgrChan:
			utils.Info.Printf("gRPC mgr hub: Response from server core:%s", respMessage)
			RemoveRoutingForwardResponse(respMessage)
		case reqMessage := <-grpcClientChan[0]:
			clientId := getClientId()
			utils.Info.Print("****************** New gRPC client session ************************: " + reqMessage.VssReq + " clientId=" + strconv.Itoa(clientId))
			if clientId != -1 {
				isMultipleEvents := false
				if !strings.Contains(reqMessage.VssReq, "unsubscribe") && strings.Contains(reqMessage.VssReq, "subscribe") {
					isMultipleEvents = true
				}
				setGrpcRoutingData(clientId, reqMessage.GrpcRespChan, isMultipleEvents)
				utils.AddRoutingForwardRequest(reqMessage.VssReq, mgrId, clientId, transportMgrChan)
			} else {
				utils.Warning.Printf("Max no of gRPC clients reached.")
				reqMessage.GrpcRespChan <- `{"action": "get","requestId": "9999","error": {"number": "404", "reason": "max_client_sessions", "message": "Max no of gRPC client sessions reached."},"ts": "2000-01-01T13:37:00Z"}` // requestId and ts values incorrect
			}
		}
	}
}
