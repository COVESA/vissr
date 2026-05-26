/**
* (C) 2022 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package utils

import (
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// WsClientIndexMu protects the WsClientIndexList read-modify-write in
// getWsClientIndex / ReturnWsClientIndex from concurrent WS upgrade
// goroutines. Without it, two simultaneous upgrades could both observe
// the same slot as free and both claim it, causing request/response
// cross-talk between unrelated clients.
var WsClientIndexMu sync.Mutex

var requestTag int

// wsClientIndexMu protects WsClientIndexList from a multi-goroutine race.
// Pre-fix, getWsClientIndex and ReturnWsClientIndex were called from each
// frontend WS handler goroutine concurrently and both read+wrote the slice
// with no synchronisation. Two simultaneous Accept()s could hand out the
// same slot and clobber each other's reservation. Same shape of bug as the
// UdsClientIndexList race fixed in PR #149.
var wsClientIndexMu sync.Mutex

var TrSecConfigPath string = "../transport_sec/" // relative path to the directory containing the transportSec.json file
type SecConfig struct {
	TransportSec  string `json:"transportSec"`  // "yes" or "no"
	HttpSecPort   string `json:"httpSecPort"`   // HTTPS port number
	WsSecPort     string `json:"wsSecPort"`     // WSS port number
	MqttSecPort   string `json:"mqttSecPort"`   // MQTTS port number
	GrpcSecPort   string `json:"grpcSecPort"`   // MQTTS port number
	AgtsSecPort   string `json:"agtsSecPort"`   // AGTS port number
	AtsSecPort    string `json:"atsSecPort"`    // ATS port number
	CaSecPath     string `json:"caSecPath"`     // relative path from the directory containing the transportSec.json file
	ServerSecPath string `json:"serverSecPath"` // relative path from the directory containing the transportSec.json file
	ServerCertOpt string `json:"serverCertOpt"` // one of  "NoClientCert"/"ClientCertNoVerification"/"ClientCertVerification"
	ClientSecPath string `json:"clientSecPath"` // relative path from the directory containing the transportSec.json file
	// ServerName is the SNI/ServerName presented in the TLS handshake. Pre-fix
	// all call sites hardcoded "localhost", which failed for any deployment
	// fronted by a real DNS name. If absent, ReadTransportSecConfig defaults
	// to "localhost" and emits a loud warning so the operator knows to set it.
	ServerName    string `json:"serverName"`
}

var SecureConfiguration SecConfig // name change to caps allowing to export outside utils

type Encoding int
const (
	NONE        Encoding = 0
	PROTOBUF             = 1
)

type ErrorInformation struct {
	Number  string
	Reason  string
	Message string
}

var ErrorInfoList [8]ErrorInformation = [8]ErrorInformation{
	{"400", "bad_request", "The request is malformed."},
	{"400", "invalid_data", "Data present in the request is invalid."},
	{"401", "expired_token", "Access token has expired."},
	{"401", "invalid_token", "Access token is invalid."},
	{"401", "missing_token", "Access token is missing."},
	{"403", "forbidden_request", "The server refuses to carry out the request."},
	{"404", "unavailable_data", "The requested data was not found."},
	{"503", "service_unavailable", "The server is temporarily unable to handle the request."}}

var MuxServer = []*http.ServeMux{
	http.NewServeMux(), // for app client HTTP sessions
	http.NewServeMux(), // for app client WS sessions
	http.NewServeMux(), // for history control HTTP sessions
	//	http.NewServeMux(), // for X transport sessions
}

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// len of WsClientIndexList must match the number of select cases in wsMgr
var WsClientIndexList = []bool{ // true = free to use, false = occupied by client; number of elements = max no of simultaneous WS sessions
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
	true,
}

var HostIP string

/************ Client response handlers ********************************************************************************/
type ClientHandler interface {
	makeappClientHandler(appClientChannel []chan string) func(http.ResponseWriter, *http.Request)
}

type HttpChannel struct {
}

type WsChannel struct {
	clientBackendChannel []chan string
	mgrIndex             int
	clientIndex          *int
}

/**********Client server initialization *******************************************************************************/

type ClientServer interface {
	InitClientServer(muxServer *http.ServeMux)
}

type HttpServer struct {
}
type WsServer struct {
	ClientBackendChannel []chan string
}
