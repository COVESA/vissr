/**
* (C) 2023 Ford Motor Company
* (C) 2020 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/w3c/automotive-viss2
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package atServer

import (
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	gomodel "github.com/COVESA/vss-tools/binary/go_parser/datamodel"
	golib "github.com/COVESA/vss-tools/binary/go_parser/parserlib"
	"github.com/google/uuid"
	"github.com/w3c/automotive-viss2/utils"
)

var VSSTreeRoot *gomodel.Node_t

// set to MAXFOUNDNODES in cparserlib.h
const MAXFOUNDNODES = 1500

const GAP = 3      // Used for PoP check
const LIFETIME = 5 // Used for PoP check

const theAtSecret = "averysecretkeyvalue2" //not shared
const AGT_PUB_KEY_DIRECTORY = "agt_public_key.rsa"
const PORT = 8600
const AT_DURATION = 1 * 60 * 60 // 1 hour

var agtKey *rsa.PublicKey

var jtiCache map[string]struct{} // PoPs JTIs that must be refused to not be reused

var muxServer = []*http.ServeMux{
	http.NewServeMux(), // HTTP
	http.NewServeMux(), // Websocket
}

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type NoScopePayload struct {
	Context string `json:"context"`
}

type AtValidatePayload struct {
	Token      string   `json:"token"`
	Paths      []string `json:"paths"`
	Action     string   `json:"action"`
	Validation string   `json:"validation"`
}

type AtGenPayload struct {
	Token   string `json:"agToken"`
	Purpose string `json:"purpose"`
	Pop     string `json:"pop"`
	Agt     utils.ExtendedJwt
	PopTk   utils.PopToken
}

var purposeList map[string]interface{}

var pList []PurposeElement

type PurposeElement struct {
	Short   string
	Long    string
	Context []ContextElement
	Access  []AccessElement
}

type ContextElement struct {
	Actor [3]RoleElement // User, App, Device
}

type RoleElement struct {
	Role []string
}

type AccessElement struct {
	Path       string
	Permission string
}

var scopeList map[string]interface{}

var sList []ScopeElement

type ScopeElement struct {
	Context  []ContextElement
	NoAccess []string
}

type PendingListElem struct {
	GatingId      int
	Consent       string
	AtGenData     AtGenPayload
	AgtExpiryTime string
}

type ActiveListElem struct {
	GatingId     int
	Atoken       string
	AtokenHandle string
	AtExpiryTime string
}

const LISTSIZE = 100

var pendingList []PendingListElem
var activeList []ActiveListElem
var expiryTicker *time.Ticker

// Initializes AGT Server public key for AGT checking
func initAgtKey() {
	err := utils.ImportRsaPubKey(AGT_PUB_KEY_DIRECTORY, &agtKey)
	if err != nil {
		utils.Error.Printf("Error importing AGT key: %s", fmt.Sprintf("%v", err))
		return
	}
	utils.Info.Printf("AGT key imported correctly.")
}

func makeAtServerHandler(atsChannel chan string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		utils.Info.Printf("atServer:url=%s", req.URL.Path)
		if req.URL.Path != "/ats" {
			http.Error(w, "404 url path not found.", 404)
		} else if req.Method != "POST" {
			//CORS POLICY, necessary for web client
			if req.Method == "OPTIONS" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Headers", "PoP")
				w.Header().Set("Access-Control-Allow-Methods", "POST")
				w.Header().Set("Access-Control-Max-Age", "57600")
			} else {
				http.Error(w, "400 bad request method.", 400)
			}
		} else {
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				http.Error(w, "400 request unreadable.", 400)
			} else {
				utils.Info.Printf("atServer:received POST request=%s", string(bodyBytes))
				atsChannel <- string(bodyBytes) // Sends request to server channel
				response := <-atsChannel
				utils.Info.Printf("atServer:POST response=%s", response)
				if len(response) == 0 {
					http.Error(w, "400 bad input.", 400)
				} else {
					w.Header().Set("Access-Control-Allow-Origin", "*")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(201) // USE 201 when responding to succesful POST requests
					w.Write([]byte(response))
				}
			}
		}
	}
}

func initClientComm(atsChannel chan string, muxServer *http.ServeMux) {
	utils.Info.Printf("initClientComm(): Initializing AT Client server")
	utils.ReadTransportSecConfig()                     // loads the secure configuration file
	atServerHandler := makeAtServerHandler(atsChannel) // Generates handlers for the AT server
	muxServer.HandleFunc("/ats", atServerHandler)
	// Initializes the AT Server depending on sec configuration
	if utils.SecureConfiguration.TransportSec == "yes" {
		server := http.Server{
			Addr:    ":" + utils.SecureConfiguration.AtsSecPort,
			Handler: muxServer,
			TLSConfig: utils.GetTLSConfig("localhost", "../transport_sec/"+utils.SecureConfiguration.CaSecPath+"Root.CA.crt",
				tls.ClientAuthType(utils.CertOptToInt(utils.SecureConfiguration.ServerCertOpt)), nil),
		}
		utils.Info.Printf("initClientComm():Starting AT Server with TLS on %s/ats", utils.SecureConfiguration.AtsSecPort)
		utils.Info.Printf("HTTPS:CerOpt=%s", utils.SecureConfiguration.ServerCertOpt)
		utils.Error.Fatal(server.ListenAndServeTLS("../transport_sec/"+utils.SecureConfiguration.ServerSecPath+"server.crt",
			"../transport_sec/"+utils.SecureConfiguration.ServerSecPath+"server.key"))
	} else { // No TLSmtvacuc14uma
		//utils.Info.Printf("initAtServer():Starting AT Server without TLS on %s/ats", PORT)
		utils.Error.Fatal(http.ListenAndServe(":"+strconv.Itoa(PORT), muxServer))
	}
}

func initEcfComm(ecfReceiveChan chan string, ecfSendChan chan string, muxServer *http.ServeMux) {
	ecfHandler := makeEcfHandler(ecfReceiveChan, ecfSendChan)
	muxServer.HandleFunc("/", ecfHandler)
	utils.Info.Print(http.ListenAndServe(":8445", muxServer))
}

func makeEcfHandler(receiveChan chan string, sendChan chan string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Upgrade") == "websocket" {
			utils.Info.Printf("Received websocket request: we are upgrading to a websocket connection.\n")
			Upgrader.CheckOrigin = func(r *http.Request) bool { return true }
			h := http.Header{}
			conn, err := Upgrader.Upgrade(w, req, h)
			if err != nil {
				utils.Error.Print("upgrade error:", err)
				return
			}
			go ecfReceiver(conn, receiveChan)
			go ecfSender(conn, sendChan)
			receiveChan <- "internal-ecfAvailable"
		} else {
			utils.Info.Printf("Client must set up a Websocket session.\n")
		}
	}
}

func ecfReceiver(conn *websocket.Conn, receiveChan chan string) {
	defer conn.Close()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			utils.Error.Printf("ECF server read error: %s\n", err)
			break
		}
		request := string(msg)
//		utils.Info.Printf("ecfReceiver: request: %s\n", request)
		receiveChan <- request
	}
}

func ecfSender(conn *websocket.Conn, sendChan chan string) {
	defer conn.Close()
	for {
		response := <- sendChan
		err := conn.WriteMessage(websocket.TextMessage, []byte(response))
		if err != nil {
			utils.Error.Printf("ecfSender: write error: %s\n", err)
			break
		}
	}
}

func generateParentResponse(input string) string {
	if strings.Contains(input, "context") { // No scope request
		return noScopeResponse(input)
	} else { // AT validation request
		return tokenValidationResponse(input)
	}
}

func generateClientResponse(input string, ecfSendChan chan string, ecfAvailable bool) string {
	if strings.Contains(input, "at-request") {
		return accessTokenResponse(input, ecfSendChan, ecfAvailable)
	} else if strings.Contains(input, "at-inquiry") {
		return consentInquiryResponse(input)
	} else {
		return `{"action":"unknown","status":"401-Bad request"}`
	}
}

func generateEcfResponse(input string, vissChan chan string) string {
	if strings.Contains(input, "consent-reply") {
		return consentReplyResponse(input)
	} else if strings.Contains(input, "consent-cancel") {
		return consentCancelResponse(input, vissChan)
	} else {
		return `{"action":"unknown","status":"401-Bad request"}`
	}
}

func consentReplyResponse(request string) string {
	var requestMap map[string]interface{}
	err := json.Unmarshal([]byte(request), &requestMap)
	if err != nil {
		utils.Error.Printf("consentReplyResponse:error request=%s", request)
		return `{"action":"consent-reply", "status":"401-Bad request"}`
	}
	if requestMap["messageId"] != nil {
		gatingId, err := strconv.Atoi(requestMap["messageId"].(string))
		if err != nil {
			utils.Error.Printf("consentReplyResponse:error converting id=%s", err)
			return `{"action":"consent-reply", "status":"401-Bad request"}`
		}
		for i := 0; i < LISTSIZE; i++ {
			if pendingList[i].GatingId == gatingId {
				pendingList[i].Consent = requestMap["consent"].(string)
				return `{"action":"consent-reply", "status":"200-OK"}`
			}
		}
	}
	return `{"action":"consent-reply", "status":"404-Not found"}`
}

func consentCancelResponse(request string, vissChan chan string) string {
	var requestMap map[string]interface{}
	err := json.Unmarshal([]byte(request), &requestMap)
	if err != nil {
		utils.Error.Printf("consentCancelResponse:error request=%s", request)
		return `{"action":"consent-cancel", "status":"401-Bad request"}`
	}
	if requestMap["messageId"] != nil {
		gatingId, err := strconv.Atoi(requestMap["messageId"].(string))
		if err != nil {
			utils.Error.Printf("consentCancelResponse:error converting id=%s", err)
			return `{"action":"consent-cancel", "status":"401-Bad request"}`
		}
		for i := 0; i < LISTSIZE; i++ {
			if pendingList[i].GatingId == gatingId {
				removeFromPendingList(i)
				return `{"action":"consent-cancel", "status":"200-OK"}`
			}
		}
		for i := 0; i < LISTSIZE; i++ {
			if activeList[i].GatingId == gatingId {
				removeFromActiveList(i)
				vissChan <- requestMap["messageId"].(string) // remove eventual subscription
				return `{"action":"consent-cancel", "status":"200-OK"}`
			}
		}
	}
	return `{"action":"consent-cancel", "status":"404-Not found"}`
}

func getPathLen(path string) int {
	for i := 0; i < len(path); i++ {
		if path[i] == 0x00 { // the path buffer defined in searchData_t is initiated with all zeros
			return i
		}
	}
	return len(path)
}

// Receives a purpose, an action and a set of paths, returns an error code (0 ok)
func validateRequestAccess(purpose string, action string, paths []string) int {
	numOfPaths := len(paths)
	var pathSubList []string
	for i := 0; i < numOfPaths; i++ {
		var searchData []golib.SearchData_t
		numOfWildcardPaths := 1
		if strings.Contains(paths[i], "*") {
			searchData, numOfWildcardPaths = golib.VSSsearchNodes(paths[i], VSSTreeRoot, MAXFOUNDNODES, true, true, 0, nil, nil)
			pathSubList = make([]string, numOfWildcardPaths)
			for j := 0; j < numOfWildcardPaths; j++ {
				pathLen := getPathLen(string(searchData[j].NodePath[:]))
				pathSubList[j] = string(searchData[j].NodePath[:pathLen])
			}
		} else {
			pathSubList = make([]string, 1)
			pathSubList[0] = paths[i]
		}
		for j := 0; j < numOfWildcardPaths; j++ {
			status := validatePurposeAndAccessPermission(purpose, action, pathSubList[j])
			if status != 0 {
				return status
			}
		}
	}
	return 0
}

// Receives a purpose, action and path and checks if the access is allowed
// Returns error code
func validatePurposeAndAccessPermission(purpose string, action string, path string) int {
	for i := 0; i < len(pList); i++ {
		if pList[i].Short == purpose {
			for j := 0; j < len(pList[i].Access); j++ {
				if pList[i].Access[j].Path == path {
					if action == "set" && pList[i].Access[j].Permission == "read-only" {
						return 61
					} else {
						return 0
					}
				}
			}
		}
	}
	return 60
}

func matchingContext(index int, context string) bool { // identical to checkAuthorization(), using sList instead of pList
	for i := 0; i < len(sList[index].Context); i++ {
		actorValid := [3]bool{false, false, false}
		for j := 0; j < len(sList[index].Context[i].Actor); j++ {
			if j > 2 {
				return false // only three subactors supported
			}
			for k := 0; k < len(sList[index].Context[i].Actor[j].Role); k++ {
				if getActorRole(j, context) == sList[index].Context[i].Actor[j].Role[k] {
					actorValid[j] = true
					break
				}
			}
		}
		if actorValid[0] && actorValid[1] && actorValid[2] {
			return true
		}
	}
	return false
}

func synthesizeNoScope(index int) string {
	if len(sList[index].NoAccess) == 1 {
		return `"` + sList[index].NoAccess[0] + `"`
	}
	noScope := "["
	for i := 0; i < len(sList[index].NoAccess); i++ {
		noScope += `"` + sList[index].NoAccess[i] + `", `
	}
	return noScope[:len(noScope)-2] + "]"
}

func getNoAccessScope(context string) string {
	for i := 0; i < len(sList); i++ {
		if matchingContext(i, context) {
			return synthesizeNoScope(i)
		}
	}
	return `""`
}

func noScopeResponse(input string) string {
	var payload NoScopePayload
	err := json.Unmarshal([]byte(input), &payload)
	if err != nil {
		utils.Error.Printf("noScopeResponse:error input=%s", input)
		return `{"no_access":""}`
	}
	res := getNoAccessScope(payload.Context)
	utils.Info.Printf("getNoAccessScope result=%s", res)
	return `{"no_access":` + res + `}`
}

func tokenValidationResponse(input string) string {
	var inputMap map[string]interface{}
	err := json.Unmarshal([]byte(input), &inputMap)
	if err != nil {
		utils.Error.Printf("tokenValidationResponse:error input=%s", input)
		return `{"validation":"1"}`
	}
	var atValidatePayload AtValidatePayload
	extractAtValidatePayloadLevel1(inputMap, &atValidatePayload)
	atValidatePayload.Token = getCompleteToken(atValidatePayload.Token)
	err = utils.VerifyTokenSignature(atValidatePayload.Token, theAtSecret)
	if err != nil {
		utils.Info.Printf("tokenValidationResponse:invalid signature, error= %s, token=%s", err, atValidatePayload.Token)
		return `{"validation":"5"}`
	}
	purpose := utils.ExtractFromToken(atValidatePayload.Token, "scp")
	res := validateRequestAccess(purpose, atValidatePayload.Action, atValidatePayload.Paths)
	if res != 0 {
		utils.Info.Printf("validateRequestAccess fails with result=%d", res)
		return `{"validation":"` + strconv.Itoa(res) + `"}`
	}
	res = validateTokenExpiry(atValidatePayload.Token)
	if res != 0 {
		utils.Info.Printf("validateTokenExpiry fails with result=%d", res)
		return `{"validation":"` + strconv.Itoa(res) + `"}`
	}
	gatingId, tokenHandle := getGatingIdAndTokenHandle(atValidatePayload.Token)
	if tokenHandle != "" {
		return `{"validation":"0", "gatingId":"` + gatingId + `", "handle":"` + tokenHandle + `"}`
	} else {
		return `{"validation":"0", "gatingId":"` + gatingId + `"}`
	}
}

func getCompleteToken(token string) string { //input token may be handle or complete token. Return complete token.
	for i := 0; i < LISTSIZE; i++ {
		if token == activeList[i].Atoken || token == activeList[i].AtokenHandle {
			return activeList[i].Atoken
		}
	}
	return ""
}

func getGatingIdAndTokenHandle(token string) (string, string) {
	for i := 0; i < LISTSIZE; i++ {
		if token == activeList[i].Atoken {
			return strconv.Itoa(activeList[i].GatingId), activeList[i].AtokenHandle
		}
	}
	return "", ""
}

func extractSignature(token string) string {
	lastDotIndex := strings.LastIndex(token, ".")
	if lastDotIndex != -1 {
		return token[lastDotIndex+1:]
	}
	utils.Error.Printf("extractSignature:Signature not found in token=%s", token)
	return ""
}

func validateTokenExpiry(token string) int {
	// Validates iat
	iatStr := utils.ExtractFromToken(token, "iat")
	iat, err := strconv.Atoi(iatStr)
	if err != nil {
		utils.Error.Print("Error reading iat. ", err)
		return 10
	}
	now := time.Now()
	if now.Before(time.Unix(int64(iat), 0)) {
		utils.Error.Print("Invalid issued at time. Issued at time is in the future. ")
		return 11
	}
	// Validates exp
	expStr := utils.ExtractFromToken(token, "exp")
	exp, err := strconv.Atoi(expStr)
	if err != nil {
		utils.Error.Print("Error reading exp. ", err)
		return 15
	}
	if now.After(time.Unix(int64(exp), 0)) {
		utils.Error.Print("Token expired. ")
		return 16
	}
	return 0
}

func extractAtValidatePayloadLevel1(atValidateMap map[string]interface{}, atValidatePayload *AtValidatePayload) {
	for k, v := range atValidateMap {
		switch vv := v.(type) {
		case []interface{}:
			//			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			extractAtValidatePayloadLevel2(vv, atValidatePayload)
		case string:
			//			utils.Info.Println(k, "is a string:")
			if k == "token" {
				atValidatePayload.Token = v.(string)
			} else if k == "action" {
				atValidatePayload.Action = v.(string)
			} else if k == "validation" {
				atValidatePayload.Validation = v.(string)
			} else if k == "paths" {
				atValidatePayload.Paths = make([]string, 1)
				atValidatePayload.Paths[0] = v.(string)
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractAtValidatePayloadLevel2(pathList []interface{}, atValidatePayload *AtValidatePayload) {
	atValidatePayload.Paths = make([]string, len(pathList))
	i := 0
	for k, v := range pathList {
		switch typ := v.(type) {
		case string:
			//			utils.Info.Println(k, "is a string:")
			atValidatePayload.Paths[i] = v.(string)
		default:
			utils.Info.Printf("%d is of an unknown type: %T", k, typ)
		}
		i++
	}
}

// Calls method to check a correct AT request. If all ok, calls AT generator and returns the AT
func accessTokenResponse(request string, ecfSendChan chan string, ecfAvailable bool) string {
	var payload AtGenPayload
	err := json.Unmarshal([]byte(request), &payload) // Unmarshalls the request
	if err != nil {
		utils.Error.Printf("accessTokenResponse:error request=%s", request)
		return `{"action": "at-request", "error": "Client request malformed"}`
	}
	utils.Info.Printf("agt token=%s", payload.Token)
	err = payload.Agt.DecodeFromFull(payload.Token) // Decodes the AGT included in the request
	if err != nil {
		utils.Error.Printf("accessTokenResponse: error decoding token=%s", payload.Token)
		return `{"action": "at-request", "error":"AGT Malformed"}`
	}
	if payload.Pop != "" { // Checks for POP token and decodes if exists
		err = payload.PopTk.Unmarshal(payload.Pop)
		if err != nil {
			utils.Error.Printf("accessTokenResponse: error decoding pop, error=%s, pop=%s", err, payload.Agt.PayloadClaims["pop"])
			return `{"action": "at-request", "error":"POP malformed"}`
		}
	}
	valid, errResponse := validateRequest(payload) // Validates the request
	if valid {
		gatingId := newGatingId()
		requiresConsent := checkifConsent(payload.Purpose)
		if requiresConsent {
			if ecfAvailable {
				writeToPendingList(gatingId, payload)
				utils.Info.Printf("requesting ECF about consent")
				//				ecfSendChan<-`{"action": "consent-ask", "purpose": "`+ payload.Purpose + `", "user-roles": "`+ payload.Agt.PayloadClaims["clx"] +
				ecfSendChan <- `{"action": "consent-ask", "user-roles": "` + payload.Agt.PayloadClaims["clx"] + `", "purpose": "` + payload.Purpose +
					`", "signal_access":` + getSignalAccess(payload.Purpose) + `, "messageId": "` + strconv.Itoa(gatingId) + `"}`
				return `{"action": "at-request", "sessionId":"` + strconv.Itoa(gatingId) + `", "consent":"NOT_SET"}`
			} else {
				return `{"action": "at-request", "error":"consent framework not accessible"}`
			}
		} else {
			at := generateAt(payload)
			writeToActiveList(gatingId, at)
			return `{"action": "at-request", "aToken":"` + at + `"}`
		}
	}
	return errResponse
}

func getSignalAccess(purpose string) string {
	for i := 0; i < len(pList); i++ {
		if pList[i].Short == purpose {
			signalAccess, err := json.Marshal(pList[i].Access)
			if err != nil {
				utils.Error.Printf("getSignalAccess:Marshall error=%s", err)
				return ""
			}
			return string(signalAccess)
		}
	}
	return ""
}

func checkifConsent(purpose string) bool {
	for i := 0; i < len(pList); i++ {
		//utils.Info.Printf("validatePurpose:purposeList[%d].Short=%s", i, pList[i].Short)
		if pList[i].Short == purpose {
			for j := 0; j < len(pList[i].Access); j++ {
				validation := -1
				golib.VSSsearchNodes(pList[i].Access[j].Path, VSSTreeRoot, MAXFOUNDNODES, true, true, 0, nil, &validation)
				if validation/10 == 1 {
					return true
				}
			}
		}
	}
	return false
}

var GatingId int

func initGatingId() {
	GatingId = 666 + rand.Intn(9999-666)
}

func newGatingId() int {
	gatingId := (GatingId + 1) % 9999
	return gatingId
}

func consentInquiryResponse(input string) string {
	gatingId := extractGatingId(input)
	for i := 0; i < LISTSIZE; i++ {
		if pendingList[i].GatingId == gatingId {
			if pendingList[i].Consent == "NOT_SET" {
				return `{"action": "at-inquiry", "sessionId":` + strconv.Itoa(gatingId) + `", "consent":"NOT_SET"}`
			} else if pendingList[i].Consent == "NO" {
				removeFromPendingList(i)
				return `{"action": "at-inquiry", "consent":"NO"}`
			} else { // YES or IN_VEHICLE
				atGenData := removeFromPendingList(i)
				at := generateAt(atGenData)
				writeToActiveList(gatingId, at)
				return `{"action": "at-inquiry", "aToken":"` + at + `", "consent":"` + pendingList[i].Consent + `"}`
			}
		}
	}
	return `{"action": "at-inquiry", "error":"404 - Not-found"}`
}

func extractGatingId(input string) int {
	gatingIdStr := extractKeyValue("sessionId", input)
	gatingId, err := strconv.Atoi(gatingIdStr)
	if err != nil {
		utils.Error.Printf("extractGatingId:error converting id=%s", err)
		return -1
	}
	return gatingId
}

func extractKeyValue(key string, input string) string {
	var inputMap map[string]interface{}
	err := json.Unmarshal([]byte(input), &inputMap)
	if err != nil {
		utils.Error.Printf("extractKeyValue:error input=%s", err)
		return ""
	}
	return inputMap[key].(string)
}

func validateTokenTimestamps(iat int, exp int) bool {
	now := time.Now()
	if now.Before(time.Unix(int64(iat), 0)) {
		return false
	}
	if now.After(time.Unix(int64(exp), 0)) {
		return false
	}
	return true
}

// *** PURPOSE VALIDATION ***
func validatePurpose(purpose string, context string) bool {
	for i := 0; i < len(pList); i++ {
		//utils.Info.Printf("validatePurpose:purposeList[%d].Short=%s", i, pList[i].Short)
		if pList[i].Short == purpose {
			//utils.Info.Printf("validatePurpose:purpose match=%s", pList[i].Short)
			if checkAuthorization(i, context) {
				return true
			}
		}
	}
	return false
}

// Validates the purpose with the context of the client given an index of a purpose in the purpose list
func checkAuthorization(index int, context string) bool {
	//utils.Info.Printf("checkAuthorization:context=%s, len(pList[index].Context)=%d", context, len(pList[index].Context))
	for i := 0; i < len(pList[index].Context); i++ { // Iterates over the different contexts
		actorValid := [3]bool{false, false, false}
		//utils.Info.Printf("checkAuthorization:len(pList[index].Context[%d].Actor)=%d", i, len(pList[index].Context[i].Actor))
		for j := 0; j < len(pList[index].Context[i].Actor); j++ { // Iterates over the actors
			if j > 2 {
				return false // Only three subactors supported
			}
			for k := 0; k < len(pList[index].Context[i].Actor[j].Role); k++ { // Iterates over the roles of the actors
				//utils.Info.Printf("checkAuthorization:getActorRole(%d, context)=%s vs pList[index].Context[%d].Actor[%d].Role[%d])=%s", j, getActorRole(j, context), i, j, k, pList[index].Context[i].Actor[j].Role[k])
				if getActorRole(j, context) == pList[index].Context[i].Actor[j].Role[k] {
					actorValid[j] = true
					break
				}
			}
		}
		if actorValid[0] && actorValid[1] && actorValid[2] {
			return true
		}
	}
	return false
}

// Returns the role of the actor in the context depending on the index (user, app, device)
func getActorRole(actorIndex int, context string) string {
	delimiter1 := strings.Index(context, "+")
	if actorIndex == 0 {
		return context[:delimiter1]
	}
	delimiter2 := strings.Index(context[delimiter1+1:], "+")
	if actorIndex == 1 {
		return context[delimiter1+1 : delimiter1+1+delimiter2]
	}
	return context[delimiter1+1+delimiter2+1:]
}

// *** END PURPOSE VALIDATION ***

// Checks vin is included in the list of valid vins: Not implemented
func checkVin(vin string) bool {
	if len(vin) == 0 {
		return true // this can only happen if AG token does not contain VIN, which is OK according to spec
	}
	return true // TODO:should be checked with VIN in tree
}

// Checks if jwt id exist in cache, if it does, return false. If not, it adds it and automatically clear it from cache when it expires
func addCheckJti(jti string) bool {
	if jtiCache == nil { // If map is empty (first time), it doesnt even check, initializes and add
		jtiCache = make(map[string]struct{})
		jtiCache[jti] = struct{}{}
		go deleteJti(jti)
		return true
	}
	if _, ok := jtiCache[jti]; ok { // Check if jti exist in cache
		return false
	}
	// If we get here, it does not exist in cache
	jtiCache[jti] = struct{}{}
	go deleteJti(jti)
	return true
}

func deleteJti(jti string) {
	time.Sleep((GAP + LIFETIME + 5) * time.Second)
	delete(jtiCache, jti)
}

// Validates the Proof of Possession of the client key
func validatePop(payload AtGenPayload) (bool, string) {
	// Check jti
	if !addCheckJti(payload.PopTk.PayloadClaims["jti"]) {
		utils.Error.Printf("validatePop: JTI used")
		return false, `{"error": "Repeated JTI"}`
	}
	// Check signature
	if err := payload.PopTk.CheckSignature(); err != nil {
		utils.Info.Printf("validatePop: Invalid POP signature: %s", err)
		return false, `{"error": "Cannot validate POP signature"}`
	}
	// Check exp: no need, iat will be used instead
	// Check iat
	if ok, cause := payload.PopTk.CheckIat(GAP, LIFETIME); !ok {
		utils.Info.Printf("validatePop: Invalid POP iat: %s", cause)
		return false, `{"error": "Cannot validate POP iat"}`
	}
	// Check that pub (thumprint) corresponds with pop key
	if ok, _ := payload.PopTk.CheckThumb(payload.Agt.PayloadClaims["pub"]); !ok {
		utils.Info.Printf("validatePop: PubKey in POP is not same as in AGT")
		return false, `{"error": "Keys in POP and AGToken are not matching"}`
	}
	// Check aud
	if ok, _ := payload.PopTk.CheckAud("vissv2/agts"); !ok {
		utils.Info.Printf("validatePop: Aud in POP not valid")
		return false, `{"error": "Invalid aud"}`
	}
	//utils.Info.Printf("validatePop:Proof of possession of key pair failed")
	//return false, `{"error": "Proof of possession of key pair failed"}`
	return true, ""
}

func validateRequest(payload AtGenPayload) (bool, string) {
	if !checkVin(payload.Agt.HeaderClaims["vin"]) {
		utils.Info.Printf("validateRequest:incorrect VIN=%s", payload.Agt.HeaderClaims["vin"])
		return false, `{"error": "Incorrect vehicle identifiction"}`
	}
	// To verify the AG Token signature
	err := payload.Agt.Token.CheckAssymSignature(agtKey)
	if err != nil {
		utils.Info.Printf("validateRequest:invalid signature, error: %s, token:%s", err, payload.Token)
		return false, `{"error": "AG token signature validation failed"}`
	}
	iat, err := strconv.Atoi(payload.Agt.PayloadClaims["iat"])
	if err != nil {
		return false, `{"error": AG token iat timestamp malformed"}`
	}
	exp, err := strconv.Atoi(payload.Agt.PayloadClaims["exp"])
	if err != nil {
		return false, `{"error": "AG token exp timestamp malformed"}`
	}
	if !validateTokenTimestamps(iat, exp) {
		//utils.Info.Printf("validateRequest:invalid token timestamps, iat=%d, exp=%d", payload.Agt.PayloadClaims["iat"], payload.Agt.PayloadClaims["exp"])
		return false, `{"error": "AG token timestamp validation failed"}`
	}
	// POP Checking
	if payload.Agt.PayloadClaims["pub"] != "" { // That means the agt is associated with a public key
		if ok, errmsj := validatePop(payload); !ok {
			return ok, errmsj
		}
	}
	if !validatePurpose(payload.Purpose, payload.Agt.PayloadClaims["clx"]) {
		utils.Info.Printf("validateRequest:invalid purpose=%s, context=%s", payload.Purpose, payload.Agt.PayloadClaims["clx"])
		return false, `{"error": "Purpose validation failed"}`
	}
	return true, ""
}

func generateAt(payload AtGenPayload) string {
	unparsedId, err := uuid.NewRandom()
	if err != nil { // Better way to generate uuid than calling an ext program
		utils.Error.Printf("generateAgt:Error generating uuid, err=%s", err)
		return `{"error": "Internal error"}`
	}
	iat := int(time.Now().Unix())
	exp := iat + AT_DURATION // 1 hour
	var jwtoken utils.JsonWebToken
	jwtoken.SetHeader("HS256")
	//jwtoken.AddClaim("vin", AtGenPayload.Agt.Vin)
	jwtoken.AddClaim("iat", strconv.Itoa(iat))
	jwtoken.AddClaim("exp", strconv.Itoa(exp))
	jwtoken.AddClaim("scp", payload.Purpose)
	jwtoken.AddClaim("clx", payload.Agt.PayloadClaims["clx"])
	jwtoken.AddClaim("aud", "w3org/gen2")
	jwtoken.AddClaim("jti", unparsedId.String())
	utils.Info.Printf("generateAt:jwtHeader=%s", jwtoken.GetHeader())
	utils.Info.Printf("generateAt:jwtPayload=%s", jwtoken.GetPayload())
	jwtoken.Encode()
	jwtoken.SymmSign(theAtSecret)
	return jwtoken.GetFullToken()
}

func initPurposelist() {
	data, err := os.ReadFile("atServer/purposelist.json")
	if err != nil {
		utils.Error.Printf("Error reading purposelist.json\n")
		os.Exit(-1)
	}
	err = json.Unmarshal([]byte(data), &purposeList)
	if err != nil {
		utils.Error.Printf("initPurposelist:error data=%s, err=%s", data, err)
		os.Exit(-1)
	}
	extractPurposeElementsLevel1(purposeList)
}

func extractPurposeElementsLevel1(purposeList map[string]interface{}) {
	for k, v := range purposeList {
		switch vv := v.(type) {
		case []interface{}:
			//			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			extractPurposeElementsLevel2(vv)
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractPurposeElementsLevel3(0, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractPurposeElementsLevel2(purposeList []interface{}) {
	pList = make([]PurposeElement, len(purposeList))
	i := 0
	for k, v := range purposeList {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractPurposeElementsLevel3(i, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
		i++
	}
}

func extractPurposeElementsLevel3(index int, purposeElem map[string]interface{}) {
	for k, v := range purposeElem {
		switch vv := v.(type) {
		case string:
			//			utils.Info.Println(k, "is string", vv)
			if k == "short" {
				pList[index].Short = vv
			} else {
				pList[index].Long = vv
			}
		case []interface{}:
			//			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			if k == "contexts" {
				pList[index].Context = make([]ContextElement, len(vv))
				extractPurposeElementsL4ContextL1(index, vv)
			} else {
				pList[index].Access = make([]AccessElement, len(vv))
				extractPurposeElementsL4SignalAccessL1(index, vv)
			}
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			if k == "contexts" {
				pList[index].Context = make([]ContextElement, 1)
				extractPurposeElementsL4ContextL2(0, index, vv)
			} else {
				pList[index].Access = make([]AccessElement, 1)
				extractPurposeElementsL4SignalAccessL2(0, index, vv)
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractPurposeElementsL4ContextL1(index int, contextElem []interface{}) {
	for k, v := range contextElem {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractPurposeElementsL4ContextL2(k, index, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractPurposeElementsL4ContextL2(k int, index int, contextElem map[string]interface{}) {
	for i, u := range contextElem {
		//		utils.Info.Println(i, u)
		switch vvv := u.(type) {
		case string:
			if i == "user" {
				pList[index].Context[k].Actor[0].Role = make([]string, 1)
				pList[index].Context[k].Actor[0].Role[0] = u.(string)
			} else if i == "app" {
				pList[index].Context[k].Actor[1].Role = make([]string, 1)
				pList[index].Context[k].Actor[1].Role[0] = u.(string)
			} else {
				pList[index].Context[k].Actor[2].Role = make([]string, 1)
				pList[index].Context[k].Actor[2].Role[0] = u.(string)
			}
		case []interface{}:
			m := 0
			for _, t := range vvv {
				//				utils.Info.Println(l, t)
				switch t.(type) {
				case string:
					if i == "user" {
						if m == 0 {
							pList[index].Context[k].Actor[0].Role = make([]string, len(vvv))
						}
						pList[index].Context[k].Actor[0].Role[m] = t.(string)
					} else if i == "app" {
						if m == 0 {
							pList[index].Context[k].Actor[1].Role = make([]string, len(vvv))
						}
						pList[index].Context[k].Actor[1].Role[m] = t.(string)
					} else {
						if m == 0 {
							pList[index].Context[k].Actor[2].Role = make([]string, len(vvv))
						}
						pList[index].Context[k].Actor[2].Role[m] = t.(string)
					}
				default:
					//					utils.Info.Println(k, "is of an unknown type")
				}
				m++
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractPurposeElementsL4SignalAccessL1(index int, accessElem []interface{}) {
	for k, v := range accessElem {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractPurposeElementsL4SignalAccessL2(k, index, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractPurposeElementsL4SignalAccessL2(k int, index int, accessElem map[string]interface{}) {
	for i, u := range accessElem {
		//		utils.Info.Println(i, u)
		if i == "path" {
			pList[index].Access[k].Path = u.(string)
		} else {
			pList[index].Access[k].Permission = u.(string)
		}
	}
}

func initScopeList() {
	data, err := os.ReadFile("atServer/scopelist.json")
	if err != nil {
		utils.Info.Printf("scopelist.json not found")
		return
	}
	err = json.Unmarshal([]byte(data), &scopeList)
	if err != nil {
		utils.Error.Printf("initScopeList:error data=%s, err=%s", data, err)
		os.Exit(-1)
	}
	extractScopeElementsLevel1(scopeList)
}

func extractScopeElementsLevel1(scopeList map[string]interface{}) {
	for k, v := range scopeList {
		switch vv := v.(type) {
		case []interface{}:
			//			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			extractScopeElementsLevel2(vv)
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractScopeElementsLevel3(0, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractScopeElementsLevel2(scopeList []interface{}) {
	sList = make([]ScopeElement, len(scopeList))
	i := 0
	for k, v := range scopeList {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractScopeElementsLevel3(i, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
		i++
	}
}

func extractScopeElementsLevel3(index int, scopeElem map[string]interface{}) {
	for k, v := range scopeElem {
		switch vv := v.(type) {
		case string:
			sList[index].NoAccess = make([]string, 1)
			sList[index].NoAccess[0] = vv
		case []interface{}:
			//			utils.Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			if k == "contexts" {
				sList[index].Context = make([]ContextElement, len(vv))
				extractScopeElementsL4ContextL1(index, vv)
			} else {
				sList[index].NoAccess = make([]string, len(vv))
				extractScopeElementsL4NoAccessL1(index, vv)
			}
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			sList[index].Context = make([]ContextElement, 1)
			extractScopeElementsL4ContextL2(0, index, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractScopeElementsL4ContextL1(index int, contextElem []interface{}) {
	for k, v := range contextElem {
		switch vv := v.(type) {
		case map[string]interface{}:
			//			utils.Info.Println(k, "is a map:")
			extractScopeElementsL4ContextL2(k, index, vv)
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractScopeElementsL4ContextL2(k int, index int, contextElem map[string]interface{}) {
	for i, u := range contextElem {
		//		utils.Info.Println(i, u)
		switch vvv := u.(type) {
		case string:
			if i == "user" {
				sList[index].Context[k].Actor[0].Role = make([]string, 1)
				sList[index].Context[k].Actor[0].Role[0] = u.(string)
			} else if i == "app" {
				sList[index].Context[k].Actor[1].Role = make([]string, 1)
				sList[index].Context[k].Actor[1].Role[0] = u.(string)
			} else {
				sList[index].Context[k].Actor[2].Role = make([]string, 1)
				sList[index].Context[k].Actor[2].Role[0] = u.(string)
			}
		case []interface{}:
			m := 0
			for _, t := range vvv {
				//				utils.Info.Println(l, t)
				switch t.(type) {
				case string:
					if i == "user" {
						if m == 0 {
							sList[index].Context[k].Actor[0].Role = make([]string, len(vvv))
						}
						sList[index].Context[k].Actor[0].Role[m] = t.(string)
					} else if i == "app" {
						if m == 0 {
							sList[index].Context[k].Actor[1].Role = make([]string, len(vvv))
						}
						sList[index].Context[k].Actor[1].Role[m] = t.(string)
					} else {
						if m == 0 {
							sList[index].Context[k].Actor[2].Role = make([]string, len(vvv))
						}
						sList[index].Context[k].Actor[2].Role[m] = t.(string)
					}
				default:
					utils.Info.Println(k, "is of an unknown type")
				}
				m++
			}
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func extractScopeElementsL4NoAccessL1(index int, noAccessElem []interface{}) {
	for k, v := range noAccessElem {
		switch vv := v.(type) {
		case string:
			//			utils.Info.Println(vv)
			sList[index].NoAccess[k] = vv
		default:
			utils.Info.Println(k, "is of an unknown type")
		}
	}
}

func initLists() {
	pendingList = make([]PendingListElem, LISTSIZE)
	activeList = make([]ActiveListElem, LISTSIZE)
	for i := 0; i < LISTSIZE; i++ {
		pendingList[i].GatingId = -1
		pendingList[i].Consent = "NOT_SET"
		activeList[i].GatingId = -1
	}
}

func writeToPendingList(gatingId int, payload AtGenPayload) {
	for i := 0; i < LISTSIZE; i++ {
		if pendingList[i].GatingId == -1 {
			pendingList[i].GatingId = gatingId
			pendingList[i].AtGenData = payload
			pendingList[i].AgtExpiryTime = utils.ExtractFromToken(payload.Token, "exp")
			setExpiryTicker()
			return
		}
	}
	utils.Error.Printf("writeToPendingList: No empty element found")
}

func writeToActiveList(gatingId int, at string) {
	for i := 0; i < LISTSIZE; i++ {
		if activeList[i].GatingId == -1 {
			activeList[i].GatingId = gatingId
			activeList[i].Atoken = at
			activeList[i].AtokenHandle = extractSignature(activeList[i].Atoken)
			activeList[i].AtExpiryTime = utils.ExtractFromToken(at, "exp")
			setExpiryTicker()
			return
		}
	}
	utils.Error.Printf("writeToActiveList: No empty element found")
}

func removeFromPendingList(index int) AtGenPayload {
	atGenData := pendingList[index].AtGenData
	pendingList[index].GatingId = -1
	pendingList[index].Consent = "NOT_SET"
	return atGenData
}

func removeFromActiveList(index int) {
	activeList[index].GatingId = -1
}

func purgeLists() string {
	var listExpiryStr string
	now := time.Now()
	for i := 0; i < LISTSIZE; i++ {
		if pendingList[i].GatingId == -1 {
			continue
		}
		listExpiryStr = pendingList[i].AgtExpiryTime
		listExpiry, err := strconv.Atoi(listExpiryStr)
		if err != nil {
			utils.Error.Print("Error reading expiry time. ", err)
			return ""
		}
		if now.After(time.Unix(int64(listExpiry), 0)) {
			removeFromPendingList(i)
			setExpiryTicker()
			return "" // no need for subscription cancel
		}
	}
	for i := 0; i < LISTSIZE; i++ {
		if activeList[i].GatingId == -1 {
			continue
		}
		listExpiryStr = activeList[i].AtExpiryTime
		listExpiry, err := strconv.Atoi(listExpiryStr)
		if err != nil {
			utils.Error.Print("Error reading expiry time. ", err)
			return ""
		}
		if now.After(time.Unix(int64(listExpiry), 0)) {
			gatingId := activeList[i].GatingId
			removeFromActiveList(i)
			setExpiryTicker()
			return strconv.Itoa(gatingId)
		}
	}
	return ""
}

func setExpiryTicker() {
	var listExpiryStr string
	isUpdated := false
	minExpiry := time.Now().Add(10 * 8760 * time.Hour) // listExpiry times should be less than 10 years from now...
	for i := 0; i < LISTSIZE; i++ {
		if pendingList[i].GatingId == -1 {
			continue
		}
		listExpiryStr = pendingList[i].AgtExpiryTime
		listExpiry, err := strconv.Atoi(listExpiryStr)
		if err != nil {
			utils.Error.Print("Error reading expiry time.", err)
			return
		}
		if minExpiry.After(time.Unix(int64(listExpiry), 0)) {
			minExpiry = time.Unix(int64(listExpiry), 0)
			isUpdated = true
		}
	}
	for i := 0; i < LISTSIZE; i++ {
		if activeList[i].GatingId == -1 {
			continue
		}
		listExpiryStr = activeList[i].AtExpiryTime
		listExpiry, err := strconv.Atoi(listExpiryStr)
		if err != nil {
			utils.Error.Print("Error reading expiry time.", err)
			return
		}
		if minExpiry.After(time.Unix(int64(listExpiry), 0)) {
			minExpiry = time.Unix(int64(listExpiry), 0)
			isUpdated = true
		}
	}
	tickerValue := minExpiry.Sub(time.Now())
	//	utils.Info.Print("Expiry ticker period=%s", tickerValue)
	if tickerValue > 0 && isUpdated {
		expiryTicker.Reset(tickerValue)
	} else {
		expiryTicker.Stop()
	}
}

func AtServerInit(viss2Chan chan string, viss2CancelChan chan string, vssRootReference *gomodel.Node_t, consentSupport bool) {
	VSSTreeRoot = vssRootReference
	clientChan := make(chan string)
	ecfReceiveChan := make(chan string)
	ecfSendChan := make(chan string)
	ecfAvailable := false

	initPurposelist()
	initScopeList()
	initAgtKey()
	initLists()
	initGatingId()
	expiryTicker = time.NewTicker(24 * time.Hour)

	go initClientComm(clientChan, muxServer[0]) //HTTP to client
	if consentSupport {
		go initEcfComm(ecfReceiveChan, ecfSendChan, muxServer[1]) // websocket client to ECF
	}
	utils.Info.Printf("atServer started...")

	for {
		select {
		case request := <-clientChan:
			utils.Info.Printf("atServer client request=%s", request)
			response := generateClientResponse(request, ecfSendChan, ecfAvailable)
			utils.Info.Printf("atServer client response=%s", response)
			clientChan <- response
		case request := <-viss2Chan:
			utils.Info.Printf("VISSv2 server request=%s", request)
			response := generateParentResponse(request)
			utils.Info.Printf("VISSv2 server response=%s", response)
			viss2Chan <- response
		case message := <-ecfReceiveChan:
			utils.Info.Printf("atServer ECF message=%s", message)
			if message == "internal-ecfAvailable" {
				ecfAvailable = true
			} else if strings.Contains(message, "status") {
				//TODO: if not OK then take action on that...
			} else {
				response := generateEcfResponse(message, viss2CancelChan)
				utils.Info.Printf("atServer ECF response=%s", response)
				ecfSendChan <- response
			}
		case <-expiryTicker.C:
			utils.Info.Printf("atServer expiryTicker triggered")
			gatingId := purgeLists()
			if gatingId != "" {
				viss2CancelChan <- gatingId
			}
		}
	}
}
