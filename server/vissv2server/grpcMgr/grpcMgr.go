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

// grpcChannelSendTimeout caps how long UnsubscribeRequest will wait when
// forwarding a kill message to the originally-subscribing SubscribeRequest
// stream's per-call channel. If that stream's goroutine is gone or stuck
// (e.g. mid stream.Send on a slow connection), this bounds the wait rather
// than blocking the unsubscribe caller's own RPC indefinitely. Mirrors
// udsMgr.go's channelSendTimeout convention.
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
	ServiceId        string // VISSv3.2 Service profile: invoke/monitor session id, mirrors SubscriptionId's role
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

// getSubscribeRoutingData looks up the clientId/response-channel pair for
// the SubscribeRequest stream that owns subscriptionId. Takes the plain
// subscriptionId string directly (mirroring getServiceRoutingData's
// shape) rather than a JSON blob to parse - callers that have a JSON
// response to extract it from should call getSubscriptionId first.
//
// Returns (-1, nil) when subscriptionId is empty. This guard matters:
// every never-yet-subscribed or freshly-reset grpcRoutingDataList slot has
// SubscriptionId at its Go zero-value "" (see resetGrpcRoutingData), so an
// unguarded empty-string lookup would spuriously "find" the first such
// unrelated slot - either an unallocated one (whose GrpcRespChannel is
// also nil, so a caller sending into it deadlocks) or a live client that
// simply hasn't subscribed to anything yet. This was previously reachable
// via killSubscribeStream's predecessor code path, which looked up the
// subscribe-side channel using the *response*'s subscriptionId field -
// but the real unsubscribe ACK never carries one (see killSubscribeStream
// for the full explanation), making that empty-string lookup a real,
// reachable, un-guarded bug.
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

// getServiceId extracts the "serviceId" field from a Service profile
// invoke/monitor response or monitoring event JSON payload. Used by the
// streaming InvokeRequest/MonitorRequest handlers to remember their own
// session id so it can be cancelled on stream disconnect.
func getServiceId(resp string) string {
	var respMap map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &respMap); err != nil {
		utils.Error.Printf("getServiceId:Unmarshal error data=%s, err=%s", resp, err)
		return ""
	}
	sid, _ := respMap["serviceId"].(string)
	return sid
}

// updateGrpcServiceRoutingData records the VISSv3.2 Service profile session
// id (returned in the invoke/monitor ACK's "serviceId" field, when a
// monitoring session was created) against clientId's routing slot,
// mirroring updateGrpcRoutingData's role for subscribe/subscriptionId.
// Enables getServiceRoutingData's reverse lookup below.
func updateGrpcServiceRoutingData(clientId int, serviceId string) {
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ClientId == clientId {
			grpcRoutingDataList[i].ServiceId = serviceId
			break
		}
	}
}

// getServiceRoutingData reverse-looks-up the clientId that owns serviceId,
// mirroring getSubscribeRoutingData's role for subscriptionId. The
// InvokeRequest/MonitorRequest stream handlers use it to learn their own
// clientId - assigned asynchronously by the manager hub - from the ACK
// response they just received, so it can be used later to cancel the
// session on stream disconnect.
//
// Returns -1 when serviceId is empty, for the same reason
// getSubscribeRoutingData guards against an empty subscriptionId: every
// never-yet-assigned or freshly-reset slot also has ServiceId=="" (Go zero
// value), so an unguarded empty lookup would spuriously match an unrelated
// slot.
func getServiceRoutingData(serviceId string) int {
	if serviceId == "" {
		return -1
	}
	grpcStateMu.Lock()
	defer grpcStateMu.Unlock()
	for i := 0; i < MAXGRPCCLIENTS; i++ {
		if grpcRoutingDataList[i].ServiceId == serviceId {
			return grpcRoutingDataList[i].ClientId
		}
	}
	return -1
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

// resetGrpcRoutingData frees clientId's routing slot. clientId == -1 is
// the "no client assigned yet" sentinel (e.g. SubscribeRequest's
// subscribeClientId before any response has arrived) and must never be
// looked up here: since every unallocated slot's own ClientId is also -1
// (see iniGrpcRoutingDataList), an unguarded call would match the first
// unallocated slot and then call resetClientIdLocked(-1), which panics on
// grpcClientIndexList[-1] - an unrecovered panic in this single-threaded
// hub goroutine crashes the entire gRPC server process. This was
// previously reachable simply by a client opening a SubscribeRequest
// stream and disconnecting before the manager hub's first response
// arrived (stream.Context().Done() fires with subscribeClientId still at
// its initial -1).
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
			// Clear stale SubscriptionId/ServiceId too, so a future lookup
			// with an empty string (guarded against above/in
			// getSubscribeRoutingData/getServiceRoutingData, but defense in
			// depth) can never match this now-freed slot by leftover data
			// from a previous occupant.
			grpcRoutingDataList[i].SubscriptionId = ""
			grpcRoutingDataList[i].ServiceId = ""
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
		// The unsubscribe ACK is addressed back to whoever sent the
		// unsubscribe request (clientId here) via the ordinary RouterId
		// routing RemoveRoutingForwardResponse already performed - there is
		// nothing left to do beyond freeing this caller's own routing slot.
		// (Terminating the SIBLING SubscribeRequest stream that owned the
		// subscription is handled synchronously by the UnsubscribeRequest
		// RPC handler itself, via killSubscribeStream, using the
		// subscriptionId from the client's own unsubscribe REQUEST - not
		// from this response, which never carries one; see
		// killSubscribeStream's doc comment.)
		resetGrpcRoutingData(clientId)
	} else if strings.Contains(resp, `"action":"monitoring"`) {
		// VISSv3.2 Service profile monitoring event: keep the routing entry
		// alive while ONGOING (further events for this invoke/monitor
		// session may follow); reset it once a terminal status arrives, same
		// as isMultipleEvent's role for the subscribe/subscription pair.
		if !strings.Contains(resp, `"ONGOING"`) {
			resetGrpcRoutingData(clientId)
		}
	} else if strings.Contains(resp, `"action":"cancel"`) {
		// Cancel ACK. Always terminal regardless of isMultipleEvent, since a
		// cancel forwarded by serveServiceStream's disconnect handler reuses
		// the invoke/monitor session's own (isMultipleEvent=true) routing
		// slot - see serveServiceStream. An ordinary client-issued cancel
		// (its own, isMultipleEvent=false, slot) also lands here.
		resetGrpcRoutingData(clientId)
	} else if strings.Contains(resp, `"action":"invoke"`) || strings.Contains(resp, `"action":"monitor"`) {
		// Synchronous invoke/monitor ACK. If ONGOING, a monitoring session
		// was created: remember its serviceId against this clientId so the
		// streaming RPC handler can look up its own clientId later (mirrors
		// updateGrpcRoutingData/getSubscribeRoutingData for subscribe). If
		// not ONGOING, this is the final response - reset now, since no
		// asynchronous "monitoring" events will follow.
		if strings.Contains(resp, `"ONGOING"`) {
			if serviceId := getServiceId(resp); serviceId != "" {
				updateGrpcServiceRoutingData(clientId, serviceId)
			}
		} else {
			resetGrpcRoutingData(clientId)
		}
	} else if !isMultipleEvent { // get, set, discover
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
			utils.Error.Printf("failed to listen: %v", err)
			break
		}
		err = server.Serve(lis)
		if err != nil {
			utils.Error.Printf("failed to start grpc: %v", err)
			break
		}
	}
}

// dispatchGrpcUnaryRequest sends a JSON request payload to the manager
// hub via grpcClientChan[0], waits for the response on a freshly
// allocated channel, and returns it. Used by the unary RPC stubs
// (GetRequest, SetRequest, UnsubscribeRequest, CancelRequest,
// DiscoverRequest) which all share the same per-message handshake.
// Extracted in PR #127 so the handshake can be unit-tested without a
// live gRPC server. See grpcMgr_dispatch_test.go.
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

func (s *Server) SetRequest(ctx context.Context, in *pb.SetRequestMessage) (*pb.SetResponseMessage, error) {
	vssResp := dispatchGrpcUnaryRequest(utils.SetRequestPbToJson(in))
	return utils.SetResponseJsonToPb(vssResp), nil
}

func (s *Server) UnsubscribeRequest(ctx context.Context, in *pb.UnsubscribeRequestMessage) (*pb.UnsubscribeResponseMessage, error) {
	// Terminate the sibling SubscribeRequest stream that owns this
	// subscription, using the subscriptionId carried on THIS request - see
	// killSubscribeStream's doc comment for why the response-based
	// cross-routing this replaced was broken.
	killSubscribeStream(in.GetSubscriptionId())
	vssResp := dispatchGrpcUnaryRequest(utils.UnsubscribeRequestPbToJson(in))
	return utils.UnsubscribeResponseJsonToPb(vssResp), nil
}

// killSubscribeStream terminates the SubscribeRequest stream that owns
// subscriptionId, by sending it the KILL_MESSAGE it already understands
// (see classifySubscribeResponse/extractClientId in SubscribeRequest's
// loop below).
//
// This replaces a previous mechanism that tried to achieve the same thing
// by piggy-backing on the unsubscribe-ACK RESPONSE: updateRoutingList's
// "unsubscribe" branch would extract a "subscriptionId" field from the
// ACK, look up the subscribing stream by it, and forward the ACK itself
// as a makeshift kill signal. That never worked, because the real
// unsubscribe ACK the server core produces (serviceMgr.go's
// buildServiceResponseMap/handleServiceUnsubscribe) - and the gRPC
// UnsubscribeResponseMessage proto itself - never carries a
// "subscriptionId" field at all (only the REQUEST does; a client supplies
// it to say what to unsubscribe from, but the server has no reason to
// echo it back). So the old lookup always resolved subscriptionId="",
// which - since every never-yet-subscribed or freshly-reset
// grpcRoutingDataList slot also has SubscriptionId at its Go zero-value
// "" - matched an unrelated slot. If that slot was unallocated
// (ClientId==-1), its GrpcRespChannel was also nil, and sending into it
// blocked forever inside GrpcMgrInit's single-threaded hub loop,
// deadlocking every gRPC client, not just the one unsubscribing. If the
// matched slot instead belonged to a live but unrelated client, the ACK
// got misrouted into that client's own response channel.
//
// This function sidesteps all of that by using the subscriptionId
// supplied on the UNSUBSCRIBE REQUEST itself (always present and
// reliable - the request schema requires it), rather than trying to
// extract one from a response that structurally never has it, and by
// signalling the target stream directly rather than routing through the
// manager hub's single-threaded response path at all.
func killSubscribeStream(subscriptionId string) {
	if subscriptionId == "" {
		return
	}
	clientId, ch := getSubscribeRoutingData(subscriptionId)
	if ch == nil {
		// No active SubscribeRequest stream owns this id (already
		// terminated, or the id was never valid) - nothing to kill.
		return
	}
	killMsg := KILL_MESSAGE + " clientId:" + strconv.Itoa(clientId)
	select {
	case ch <- killMsg:
	case <-time.After(grpcChannelSendTimeout):
		utils.Error.Printf("killSubscribeStream: send timed out for clientId=%d, subscriptionId=%s", clientId, subscriptionId)
	}
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

func (s *Server) CancelRequest(ctx context.Context, in *pb.CancelRequestMessage) (*pb.CancelResponseMessage, error) {
	vssResp := dispatchGrpcUnaryRequest(utils.CancelRequestPbToJson(in))
	return utils.CancelResponseJsonToPb(vssResp), nil
}

func (s *Server) DiscoverRequest(ctx context.Context, in *pb.DiscoverRequestMessage) (*pb.DiscoverResponseMessage, error) {
	vssResp := dispatchGrpcUnaryRequest(utils.DiscoverRequestPbToJson(in))
	return utils.DiscoverResponseJsonToPb(vssResp), nil
}

// serveServiceStream implements the shared control flow for the Invoke and
// Monitor server-streaming RPCs: forward vssReq to the manager hub and send
// the first (synchronous ACK/response) message via sendFn. If its status is
// ONGOING, keep forwarding subsequent "monitoring" events via sendFn until
// one carries a terminal (non-ONGOING) status, or the client disconnects.
//
// On disconnect (ctx.Done()) while a session is still ONGOING, this issues a
// "cancel" for the session's serviceId so vissServiceMgr tears down the
// invocation/session promptly, instead of leaving it to run until its
// timeout watchdog fires (vissServiceMgr.go's DefaultTimeout, up to 30s by
// default). This mirrors SubscribeRequest's "internal-killsubscriptions" on
// disconnect, using the ordinary client-facing "cancel" action instead
// since, unlike subscriptions, invoke/monitor sessions are torn down via
// their own well-defined action rather than an internal-only one.
func serveServiceStream(vssReq string, ctx context.Context, sendFn func(vssResp string) error) error {
	// Buffered so that a late response arriving after this stream has
	// already returned (e.g. the disconnect/cancel race - see below) does
	// not block the manager hub's single-threaded response loop forever.
	grpcResponseChan := make(chan string, 4)
	grpcClientChan[0] <- GrpcRequestMessage{vssReq, grpcResponseChan}
	clientId := -1
	serviceId := ""
	for {
		select {
		case <-ctx.Done():
			utils.Info.Printf("gRPC invoke/monitor session terminated by client")
			// clientId is only ever set alongside a non-empty serviceId (see
			// the ONGOING-ACK branch below), so clientId != -1 implies we
			// have a session to cancel.
			if clientId != -1 {
				// Forward a cancel on this session's own routing slot so the
				// invocation/session is torn down promptly (vissServiceMgr's
				// HandleCancel) instead of running until the timeout
				// watchdog fires. The slot is released once the cancel ACK
				// comes back (see updateRoutingList's "cancel" branch above)
				// rather than here, so it cannot be reallocated to a new
				// client before that ACK is routed.
				cancelReq := `{"action":"cancel","serviceId":"` + serviceId + `"}`
				utils.AddRoutingForwardRequest(cancelReq, grpcMgrId, clientId, grpcMgrChan)
			}
			return nil
		case vssResp := <-grpcResponseChan:
			if err := sendFn(vssResp); err != nil {
				if clientId != -1 {
					resetGrpcRoutingData(clientId)
				}
				return err
			}
			if !strings.Contains(vssResp, `"ONGOING"`) {
				// Terminal response/event (SUCCESSFUL/CANCELED/FAILED), or a
				// synchronous-only ACK that never started a session. The
				// manager hub has already reset the routing entry (see
				// updateRoutingList).
				return nil
			}
			if strings.Contains(vssResp, `"action":"monitoring"`) {
				continue // ONGOING event; keep waiting for the next one
			}
			// The ONGOING synchronous ACK: remember our own clientId (learned
			// via the serviceId the hub just recorded against it, mirroring
			// SubscribeRequest's subscribeClientId/getSubscribeRoutingData)
			// so a later disconnect can be correlated back to this session.
			serviceId = getServiceId(vssResp)
			if serviceId != "" {
				clientId = getServiceRoutingData(serviceId)
			}
		}
	}
}

func (s *Server) InvokeRequest(in *pb.InvokeRequestMessage, stream pb.VISS_InvokeRequestServer) error {
	vssReq := utils.InvokeRequestPbToJson(in)
	return serveServiceStream(vssReq, stream.Context(), func(vssResp string) error {
		return stream.Send(utils.InvokeStreamJsonToPb(vssResp))
	})
}

func (s *Server) MonitorRequest(in *pb.MonitorRequestMessage, stream pb.VISS_MonitorRequestServer) error {
	vssReq := utils.MonitorRequestPbToJson(in)
	return serveServiceStream(vssReq, stream.Context(), func(vssResp string) error {
		return stream.Send(utils.MonitorStreamJsonToPb(vssResp))
	})
}

// isMultipleEventsRequest classifies a VSS request as one that will
// produce a stream of events (i.e. an active subscribe, or a Service
// profile invoke/monitor that may report an ONGOING invocation followed by
// "monitoring" events) rather than a one-shot response. Used by
// handleGrpcNewClientSession to set up the right routing flag. Extracted in
// PR #127 so the classification can be table-tested.
func isMultipleEventsRequest(vssReq string) bool {
	if strings.Contains(vssReq, `"action":"invoke"`) || strings.Contains(vssReq, `"action":"monitor"`) {
		return true
	}
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

// GrpcMgrInit runs the gRPC transport hub. reqChan carries client requests to
// the server core; respChan carries responses back from the core. They are
// separate channels so a response can never be read back here as a request.
func GrpcMgrInit(mgrId int, reqChan chan string, respChan chan string) {
	utils.ReadTransportSecConfig()
	grpcMgrId = mgrId
	grpcMgrChan = reqChan // request side: used by the subscription-kill forward
	grpcClientIndexList = make([]bool, MAXGRPCCLIENTS)
	grpcRoutingDataList = make([]GrpcRoutingData, MAXGRPCCLIENTS)
	grpcCompression = utils.PROTOBUF // set via viss2server command line param?
	iniGrpcRoutingDataList()
	go initGrpcServer()

	utils.Info.Println("gRPC manager data session initiated.")

	for {
		select {
		case respMessage := <-respChan:
			handleGrpcTransportResponse(respMessage)
		case reqMessage := <-grpcClientChan[0]:
			handleGrpcNewClientSession(reqMessage, mgrId, reqChan)
		}
	}
}
