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
	"sync"
	"time"
)

// grpcChannelSendTimeout caps how long UnsubscribeRequest waits when forwarding a kill message to the
// SubscribeRequest stream that owns the subscription. If that stream's goroutine is gone or stuck
// (e.g. in a slow stream.Send) the wait is bounded instead of blocking the unsubscribing RPC forever.
const grpcChannelSendTimeout = 5 * time.Second

var grpcCompression utils.Encoding
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
	pb.UnimplementedVISSServer
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

// grpcStateMu serialises access to grpcRoutingDataList and
// grpcClientIndexList. Per-RPC SubscribeRequest goroutines call
// resetGrpcRoutingData on stream.Context().Done() and on send errors,
// while the manager loop concurrently calls getClientId,
// setGrpcRoutingData, updateGrpcRoutingData, getGrpcRoutingData, and
// getSubscribeRoutingData on the same slices. Without the lock, a
// disconnecting subscriber concurrent with a new client produces slot
// leaks, cross-talk to the wrong client, or a runtime panic on
// concurrent slice mutation. Mirrors the WsClientIndexMu /
// udsClientIndexMu / sessionListMu mutexes added in PR #119 / batch 3.
var grpcStateMu sync.Mutex

func getClientId() int {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcClientIndexList[i] == false {
			grpcClientIndexList[i] = true
			return i
		}
	}
	return -1
}

func getGrpcRoutingData(clientId int) (chan string, bool) {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			return grpcRoutingDataList[i].GrpcRespChannel, grpcRoutingDataList[i].IsMultipleEvents
		}
	}
	return nil, false
}

func updateGrpcRoutingData(clientId int, subscriptionId string) {
	//utils.Info.Printf("updateGrpcRoutingData:clientId=%d, subscriptionId=%s", clientId, subscriptionId)
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			grpcRoutingDataList[i].SubscriptionId = subscriptionId
			break
		}
	}
}

// getSubscribeRoutingData looks up the clientId and response channel of the SubscribeRequest stream
// that owns subscriptionId. It returns (-1, nil) for an empty id: every unused or freshly reset slot
// has SubscriptionId "" (the zero value), so an unguarded lookup of "" would "find" an unrelated
// slot, either an unallocated one (nil channel, a send to it blocks forever) or a live client that
// has not subscribed to anything.
func getSubscribeRoutingData(subscriptionId string) (int, chan string) {
	if subscriptionId == "" {
		return -1, nil
	}
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].SubscriptionId == subscriptionId {
			return grpcRoutingDataList[i].ClientId, grpcRoutingDataList[i].GrpcRespChannel
		}
	}
	return -1, nil
}

// resetClientIdLocked clears a client-id slot. Caller must hold
// grpcStateMu. Used internally by resetGrpcRoutingData to avoid
// double-locking.
func resetClientIdLocked(clientId int) {
	grpcClientIndexList[clientId] = false
}

func resetClientId(clientId int) {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	resetClientIdLocked(clientId)
}

func initClientIdList() {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		grpcClientIndexList[i] = false
	}
}

func setGrpcRoutingData(clientId int, grpcRespChan chan string, isMultipleEvent bool) bool {
	//utils.Info.Printf("setGrpcRoutingData:clientId=%d, isMultipleEvent=%d", clientId, isMultipleEvent)
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
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

// resetGrpcRoutingData frees the routing slot of clientId. clientId < 0 is the "no client assigned
// yet" value (SubscribeRequest's subscribeClientId before the first response arrives) and is ignored:
// every unallocated slot also has ClientId -1, so it would match the first free slot and then index
// grpcClientIndexList[-1], a panic that takes down the whole server.
func resetGrpcRoutingData(clientId int) {
	if clientId < 0 {
		return
	}
	utils.Info.Printf("resetGrpcRoutingData:clientId=%d", clientId)
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			grpcRoutingDataList[i].ClientId = -1
			grpcRoutingDataList[i].SubscriptionId = "" // so that a freed slot can never match a later lookup
			resetClientIdLocked(clientId)
			break
		}
	}
}

func iniGrpcRoutingDataList() {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
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
		// The unsubscribe response goes back to the client that sent the unsubscribe request
		// (clientId) by the ordinary RouterId routing, so only that client's own slot is freed here.
		// Ending the SubscribeRequest stream that owns the subscription is done by the
		// UnsubscribeRequest handler, with killSubscribeStream: this response has no subscriptionId
		// to find that stream by (see killSubscribeStream).
		resetGrpcRoutingData(clientId)
	} else if !isMultipleEvent { // get, set and discover
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
	if respMap["subscriptionId"] == nil {
		return""
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

		config := utils.GetTLSConfig(utils.SecureConfiguration.ServerName, utils.TrSecConfigPath+utils.SecureConfiguration.CaSecPath+"Root.CA.crt",
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
//		server = grpc.NewServer(grpc.StatsHandler(&Handler{}))
		var opts []grpc.ServerOption
		server = grpc.NewServer(opts...)
		portNo = "8887"
		utils.Info.Printf("portNo =%s", portNo)
	}
	pb.RegisterVISSServer(server, &Server{})
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

// dispatchGrpcUnaryRequest sends a JSON request payload to the manager
// hub via grpcClientChan[0], waits for the response on a freshly
// allocated channel, and returns it. Used by the three unary RPC
// stubs (GetRequest, SetRequest, UnsubscribeRequest) which all share
// the same per-message handshake. Extracted in PR #127 so the
// handshake can be unit-tested without a live gRPC server. See
// grpcMgr_dispatch_test.go.
func dispatchGrpcUnaryRequest(vssReq string) string {
	grpcResponseChan := make(chan string)
	grpcClientChan[0] <- GrpcRequestMessage{vssReq, grpcResponseChan}
	return <-grpcResponseChan
}

// classifySubscribeResponse inspects a response coming back from the
// manager hub during a streaming subscribe RPC and tells the caller
// whether the response indicates an error (subscribe should
// terminate) or a kill message (the unsubscribe sibling told us to
// stop). Extracted from SubscribeRequest's response arm in PR #127 so
// the classification logic can be table-tested without a live gRPC
// stream. See grpcMgr_dispatch_test.go.
func classifySubscribeResponse(vssResp string) (isError bool, isKill bool) {
	isError = strings.Contains(vssResp, `"error"`)
	isKill = strings.Contains(vssResp, KILL_MESSAGE)
	return
}

func (s *Server) GetRequest(ctx context.Context, in *pb.GetRequestMessage) (*pb.GetResponseMessage, error) {
	vssReq := utils.GetRequestPbToJson(in)
	utils.Info.Println(vssReq)
	vssResp := dispatchGrpcUnaryRequest(vssReq)
	return utils.GetResponseJsonToPb(vssResp), nil
}

// MultiGetRequest implements the VISSv3.2 Multiple Tree Addressability
// Read (CORE section 5.1.1.2) as a separate, additive gRPC RPC, so
// that existing GetRequest clients/servers remain wire compatible.
func (s *Server) MultiGetRequest(ctx context.Context, in *pb.MultiGetRequestMessage) (*pb.MultiGetResponseMessage, error) {
	vssReq := utils.MultiGetRequestPbToJson(in)
	utils.Info.Println(vssReq)
	vssResp := dispatchGrpcUnaryRequest(vssReq)
	return utils.MultiGetResponseJsonToPb(vssResp), nil
}

func (s *Server) SetRequest(ctx context.Context, in *pb.SetRequestMessage) (*pb.SetResponseMessage, error) {
	vssResp := dispatchGrpcUnaryRequest(utils.SetRequestPbToJson(in))
	return utils.SetResponseJsonToPb(vssResp), nil
}

// MultiSetRequest implements the VISSv3.2 Multiple Data Update (CORE
// section 5.2.2) as a separate, additive gRPC RPC, so that existing
// SetRequest clients/servers remain wire compatible. Not supported
// over HTTP, see CORE section 5.2.
func (s *Server) MultiSetRequest(ctx context.Context, in *pb.MultiSetRequestMessage) (*pb.SetResponseMessage, error) {
	vssResp := dispatchGrpcUnaryRequest(utils.MultiSetRequestPbToJson(in))
	return utils.SetResponseJsonToPb(vssResp), nil
}

func (s *Server) UnsubscribeRequest(ctx context.Context, in *pb.UnsubscribeRequestMessage) (*pb.UnsubscribeResponseMessage, error) {
	// End the SubscribeRequest stream that owns the subscription, using the subscriptionId of THIS request.
	killSubscribeStream(in.GetSubscriptionId())
	vssResp := dispatchGrpcUnaryRequest(utils.UnsubscribeRequestPbToJson(in))
	return utils.UnsubscribeResponseJsonToPb(vssResp), nil
}

// killSubscribeStream ends the SubscribeRequest stream that owns subscriptionId by sending it the
// KILL_MESSAGE that its loop already understands (classifySubscribeResponse, extractClientId).
//
// The kill used to be sent from updateRoutingList: it took the subscriptionId from the unsubscribe
// RESPONSE, looked up the subscribing stream with it and forwarded the response to that stream. But the
// response from the server core never has a subscriptionId (only the request does), so the lookup was
// made with "" and matched an unrelated slot. When that slot was unallocated its channel was nil and
// the send blocked forever inside GrpcMgrInit's single hub goroutine, so every gRPC request after the
// first unsubscribe timed out (DeadlineExceeded), not only the unsubscribing one. When the slot belonged
// to another live client, the response was delivered to that client instead.
//
// The subscriptionId of the unsubscribe REQUEST is always there, so it is used instead, and the stream is
// signalled directly, without going through the hub.
func killSubscribeStream(subscriptionId string) {
	if subscriptionId == "" {
		return
	}
	clientId, ch := getSubscribeRoutingData(subscriptionId)
	if ch == nil { // no stream owns this id: already ended, or never valid
		return
	}
	select {
	case ch <- KILL_MESSAGE + " clientId:" + strconv.Itoa(clientId):
	case <-time.After(grpcChannelSendTimeout):
		utils.Error.Printf("killSubscribeStream: send timed out for clientId=%d, subscriptionId=%s", clientId, subscriptionId)
	}
}

// DiscoverRequest implements CORE section 5.5 Discovery (both Signal
// Discovery and Forest Discovery) as a unary gRPC RPC.
func (s *Server) DiscoverRequest(ctx context.Context, in *pb.DiscoverRequestMessage) (*pb.DiscoverResponseMessage, error) {
	vssResp := dispatchGrpcUnaryRequest(utils.DiscoverRequestPbToJson(in))
	return utils.DiscoverResponseJsonToPb(vssResp), nil
}

func (s *Server) SubscribeRequest(in *pb.SubscribeRequestMessage, stream pb.VISS_SubscribeRequestServer) error {
	vssReq := utils.SubscribeRequestPbToJson(in)
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
			isError, isKill := classifySubscribeResponse(vssResp)
			if isError { // error message
				return nil
			}
			if isKill { // issued by unsubscribe thread
				clientId := extractClientId(vssResp)
				resetGrpcRoutingData(clientId)
				return nil
			}
			if subscribeClientId == -1 {
				subscribeClientId, _ = getSubscribeRoutingData(getSubscriptionId(vssResp))
			}
			pbResp := utils.SubscribeStreamJsonToPb(vssResp)
			if err := stream.Send(pbResp); err != nil {
				resetGrpcRoutingData(subscribeClientId)
				return err
			}
		}
	}
}

func extractClientId(killMessage string) int { // mesage contains clientId:xyz
	delimIndex := strings.Index(killMessage, ":")
	clientId, _ := strconv.Atoi(killMessage[delimIndex+1:])
	return clientId
}

// isMultipleEventsRequest classifies a VSS request as one that will
// produce a stream of events (i.e. an active subscribe) rather than a
// one-shot response. Used by handleGrpcNewClientSession to set up the
// right routing flag. Extracted in PR #127 so the classification can
// be table-tested.
func isMultipleEventsRequest(vssReq string) bool {
	return !strings.Contains(vssReq, "unsubscribe") && strings.Contains(vssReq, "subscribe")
}

// handleGrpcTransportResponse logs the response coming back from the
// manager hub and routes it back to the original gRPC client via
// RemoveRoutingForwardResponse. Extracted from GrpcMgrInit's
// for/select loop in PR #127.
func handleGrpcTransportResponse(respMessage string) {
	utils.Info.Printf("gRPC mgr hub: Response from server core:%s", respMessage)
	RemoveRoutingForwardResponse(respMessage)
}

// handleGrpcNewClientSession allocates a new gRPC clientId, sets up
// routing data, and either forwards the request to the transport
// manager or short-circuits with a max-clients error response.
// Extracted from GrpcMgrInit's for/select loop in PR #127 so the
// allocation/short-circuit behaviour can be unit-tested.
func handleGrpcNewClientSession(reqMessage GrpcRequestMessage, mgrId int, transportMgrChan chan string) {
	clientId := getClientId()
	utils.Info.Print("****************** New gRPC client session ************************: " + reqMessage.VssReq + " clientId=" + strconv.Itoa(clientId))
	if clientId != -1 {
		isMultipleEvents := isMultipleEventsRequest(reqMessage.VssReq)
		setGrpcRoutingData(clientId, reqMessage.GrpcRespChan, isMultipleEvents)
		utils.AddRoutingForwardRequest(reqMessage.VssReq, mgrId, clientId, transportMgrChan)
		return
	}
	utils.Warning.Printf("Max no of gRPC clients reached.")
	reqMessage.GrpcRespChan <- `{"action": "get","requestId": "9999","error": {"number": "404", "reason": "max_client_sessions", "description": "Max no of gRPC client sessions reached."},"ts": "2000-01-01T13:37:00Z"}` // requestId and ts values incorrect
}

func GrpcMgrInit(mgrId int, transportMgrChan chan string) {
	utils.ReadTransportSecConfig()
	grpcMgrId = mgrId
	grpcMgrChan = transportMgrChan
	grpcClientIndexList = make([]bool, MAXGRPCCLIENTS)
	grpcRoutingDataList = make([]GrpcRoutingData, MAXGRPCCLIENTS)
	grpcCompression = utils.PROTOBUF // set via viss2server command line param?
	iniGrpcRoutingDataList()
	go initGrpcServer()

	utils.Info.Println("gRPC manager data session initiated.")

	for {
		select {
		case respMessage := <-transportMgrChan:
			handleGrpcTransportResponse(respMessage)
		case reqMessage := <-grpcClientChan[0]:
			handleGrpcNewClientSession(reqMessage, mgrId, transportMgrChan)
		}
	}
}
