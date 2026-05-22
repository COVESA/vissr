/**
* (C) 2020 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/google/uuid"
)

const LT_DURATION = 4 * 24 * 60 * 60 // 4 days
const ST_DURATION = 4 * 60 * 60      // 4 hours
const PRIV_KEY_DIRECTORY = "agt_private_key.rsa"
const PUB_KEY_DIRECTORY = "agt_public_key.rsa"
const GAP = 3           // Used for PoP Checking
const LIFETIME = 5 * 60 // Used for PoP Checking
const PORT = 7500

// MAX_REQUEST_BODY caps the size of an incoming POST body so a
// single huge request can't OOM the AGT server. 16 KiB is far more
// than a legitimate AGT request (a PoP is ~1 KiB at most).
const MAX_REQUEST_BODY = 16 * 1024

// MAX_JTI_CACHE_SIZE caps the number of in-flight JTIs kept in
// memory. JTIs expire after GAP+LIFETIME+5 = 308 seconds, so even
// at line rate an attacker would only need this many writes to OOM
// the process if the cache were unbounded. When full, the oldest
// entry is evicted.
const MAX_JTI_CACHE_SIZE = 10_000

var privKey *rsa.PrivateKey

// jtiCache stores PoP JWT IDs we've seen, to reject replay. Both
// the cache and its eviction queue are guarded by jtiCacheMu — the
// previous code mutated an unsynchronized map from the main loop
// and from `go deleteJti(...)` goroutines, which is a `concurrent
// map writes` panic waiting to happen under any meaningful load.
var jtiCache = make(map[string]struct{})
var jtiCacheOrder []string // FIFO of JTIs for size-cap eviction
var jtiCacheMu sync.Mutex

// agtsHandlerMu serializes the HTTP handler's send-then-receive
// against the shared unbuffered serverChannel. Two concurrent
// POSTs would otherwise race: both write request bodies + PoPs
// onto the channel, and both read responses, with no guarantee
// that handler 1 reads response 1 (instead of response 2 and
// vice versa). AGT cross-delivery is a security issue.
var agtsHandlerMu sync.Mutex

// ----------------------------------------------------------------------------
// Protocol-hardening configuration (set at init() from env vars)
//
// Each of these gates a security-critical behaviour behind an env
// var so dev / CI workflows keep working with loud startup
// warnings; production deployments set the vars to harden.
//
//   VISSR_AGT_DEV_KEY        Replaces the hardcoded "ABC" proof
//                             value. AGT requests are refused
//                             entirely when this is unset, so
//                             deployments cannot accidentally run
//                             with the public-knowledge default.
//
//   VISSR_AGT_ALLOWED_ORIGIN Comma-separated list of Origin
//                             header values accepted by the AGT
//                             POST endpoint. Unset = warn + accept
//                             any origin (preserves the previous
//                             Access-Control-Allow-Origin: * shape).
// ----------------------------------------------------------------------------

var agtDevKey string
var agtAllowedOrigins []string

func init() {
	agtDevKey = os.Getenv("VISSR_AGT_DEV_KEY")
	if agtDevKey == "" {
		log.Printf("WARNING: agt_server: VISSR_AGT_DEV_KEY not set; AGT requests will be REFUSED. The previous code accepted the hardcoded public-knowledge value \"ABC\" — set VISSR_AGT_DEV_KEY to a long random value to restore issuance while real client attestation is being built.")
	}
	if origins := os.Getenv("VISSR_AGT_ALLOWED_ORIGIN"); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				agtAllowedOrigins = append(agtAllowedOrigins, trimmed)
			}
		}
	} else {
		log.Printf("WARNING: agt_server: VISSR_AGT_ALLOWED_ORIGIN not set; AGT endpoint will accept any Origin header. Set a comma-separated allow-list in production.")
	}
}

// allowedOriginHeader returns the value to put in the
// Access-Control-Allow-Origin response header. With an allow-list
// configured, it echoes the request Origin if it matches, otherwise
// returns "" (no CORS header). With no allow-list (compat mode), it
// returns "*" as the old code did.
func allowedOriginHeader(req *http.Request) string {
	if len(agtAllowedOrigins) == 0 {
		return "*"
	}
	origin := req.Header.Get("Origin")
	for _, allowed := range agtAllowedOrigins {
		if origin == allowed {
			return origin
		}
	}
	return ""
}

type Payload struct {
	// Action  string `json:"action"`
	Vin     string `json:"vin"`
	Context string `json:"context"`
	Proof   string `json:"proof"`
	//Key     utils.JsonWebKey `json:"key"`
	Key string `json:"key"`
}

// Handles the request depending on the url and the method for the request
func makeAgtServerHandler(serverChannel chan string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		utils.Info.Printf("agtServer:url=%s", req.URL.Path)
		if req.URL.Path != "/agts" {
			http.Error(w, "404 url path not found.", 404)
			return
		}
		if req.Method != "POST" {
			//CORS POLICY, necessary for web client
			if req.Method == "OPTIONS" {
				if origin := allowedOriginHeader(req); origin != "" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
				w.Header().Set("Access-Control-Allow-Headers", "PoP")
				w.Header().Set("Access-Control-Allow-Methods", "POST")
				w.Header().Set("Access-Control-Max-Age", "57600")
			} else {
				http.Error(w, "400 bad request method.", 400)
			}
			return
		}
		// Bound the request body to avoid OOM on a single huge POST.
		req.Body = http.MaxBytesReader(w, req.Body, MAX_REQUEST_BODY)
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "Request Entity Too Large.", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "400 request unreadable.", http.StatusBadRequest)
			}
			return
		}
		utils.Info.Printf("agtServer:received POST request=%s\n", string(bodyBytes))
		pop := req.Header.Get("PoP")
		if pop != "" {
			utils.Info.Printf("agtServer: received PoP = %s", pop)
		}
		// Serialize the send-then-receive triple against
		// serverChannel. Without this lock two concurrent POSTs can
		// interleave their body/pop/response triples, cross-delivering
		// AGTs between principals.
		agtsHandlerMu.Lock()
		serverChannel <- string(bodyBytes)
		serverChannel <- pop
		response := <-serverChannel
		agtsHandlerMu.Unlock()
		utils.Info.Printf("agtServer:POST response=%s", response)
		// Response generation
		if len(response) == 0 {
			http.Error(w, "400 bad input.", 400)
			return
		}
		if origin := allowedOriginHeader(req); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201) // USE 201 when responding to succesful POST requests
		w.Write([]byte(response))
	}
}

// Initializes the AGT Server to work on the port desired
func initAgtServer(serverChannel chan string, muxServer *http.ServeMux) {
	utils.Info.Printf("initAgtServer(): Starting AGT server")
	utils.ReadTransportSecConfig()                          // loads the secure configuration file
	agtServerHandler := makeAgtServerHandler(serverChannel) // Generates handlers for the AGT server
	muxServer.HandleFunc("/agts", agtServerHandler)
	// Initializes the AGT Server depending on sec configuration
	if utils.SecureConfiguration.TransportSec == "yes" {
		server := http.Server{
			Addr:    ":" + utils.SecureConfiguration.AgtsSecPort,
			Handler: muxServer,
			/*TLSConfig: utils.GetTLSConfig("localhost", "../transport_sec/"+utils.SecureConfiguration.CaSecPath+"Root.CA.crt",
			tls.ClientAuthType(utils.CertOptToInt(utils.SecureConfiguration.ServerCertOpt))),*/
		}
		utils.Info.Printf("initAgtServer():Starting AGT Server with TLS on %s/agts", utils.SecureConfiguration.AgtsSecPort)
		utils.Info.Printf("initAgtServer():HTTPS:CerOpt=%s", utils.SecureConfiguration.ServerCertOpt)
		utils.Error.Fatal(server.ListenAndServeTLS("../transport_sec/"+utils.SecureConfiguration.ServerSecPath+"server.crt",
			"../transport_sec/"+utils.SecureConfiguration.ServerSecPath+"server.key"))
	} else { // No TLS
		utils.Info.Printf("initAgtServer():Starting AGT Server without TLS on %d/agts", PORT)
		utils.Error.Fatal(http.ListenAndServe(":"+strconv.Itoa(PORT), muxServer))
	}
}

// Load key from file, if not, creates new key file
func initKey() {
	if err := utils.ImportRsaKey(PRIV_KEY_DIRECTORY, &privKey); err != nil {
		utils.Error.Printf("Error importing private key: %s, generating one.", err)
		if err := utils.GenRsaKey(256, &privKey); err != nil {
			utils.Error.Printf("Error generating private key: %s. Signature not avaliable", err)
			return
		}
		// Key generated, must export it
		utils.Info.Printf("RSA key generated correctly")
		if err := os.Remove(PRIV_KEY_DIRECTORY); err != nil && !errors.Is(err, fs.ErrNotExist) {
			utils.Error.Printf("Error exporting private key, cannot remove previous file: %s", err)
		} else if err := utils.ExportKeyPair(privKey, PRIV_KEY_DIRECTORY, PUB_KEY_DIRECTORY); err != nil {
			utils.Error.Printf("Error exporting private key: %s", err)
		}
		utils.Info.Printf("RSA key exported")
		return
	}
	utils.Info.Printf("RSA key imported correctly")
}

// GenerateResponse must unmarshall the payload, then ask for AGT Generation
func generateResponse(input string, pop string) string {
	var payload Payload
	err := json.Unmarshal([]byte(input), &payload)
	if err != nil {
		utils.Error.Printf("generateResponse:error input=%s", input)
		return `{"action": "agt-request", "error": "Client request malformed"}`
	}
	if authenticateClient(payload) {
		if pop != "" {
			return generateLTAgt(payload, pop) // In case a pop claim appears, a LT agt must be generated
		}
		return generateAgt(payload) // In case no pop claim appears, an ST AGT is issued
	}
	return `{"action": "agt-request", "error": "Client authentication failed"}`
}

// Client roles checking
func checkUserRole(userRole string) bool {
	if userRole != "OEM" && userRole != "Dealer" && userRole != "Independent" && userRole != "Owner" && userRole != "Driver" && userRole != "Passenger" {
		return false
	}
	return true
}
func checkAppRole(appRole string) bool {
	if appRole != "OEM" && appRole != "Third party" {
		return false
	}
	return true
}
func checkDeviceRole(deviceRole string) bool {
	if deviceRole != "Vehicle" && deviceRole != "Nomadic" && deviceRole != "Cloud" {
		return false
	}
	return true
}
func checkRoles(context string) bool {
	if strings.Count(context, "+") != 2 {
		return false
	}
	delimiter1 := strings.Index(context, "+")
	if delimiter1 == -1 { // defensive: strings.Count gate already guarantees this, but be explicit
		return false
	}
	delimiter2 := strings.Index(context[delimiter1+1:], "+")
	if delimiter2 == -1 {
		return false
	}
	if !checkUserRole(context[:delimiter1]) || !checkAppRole(context[delimiter1+1:delimiter1+1+delimiter2]) || !checkDeviceRole(context[delimiter1+1+delimiter2+1:]) {
		return false
	}
	return true

}

// authenticateClient checks the client's claimed context and proof.
// Bug 1 fix: the original code accepted a hardcoded literal "ABC"
// as the proof value, allowing anyone who knew the public-knowledge
// string to obtain a 4-day OEM AGT. The proof is now compared
// against whatever VISSR_AGT_DEV_KEY contains; when unset, AGT
// requests are refused entirely.
//
// This is NOT a real authentication scheme — real client
// attestation (mTLS, device cert, hardware-backed key registration)
// is what should ultimately gate AGT issuance. The env-var gate
// just stops the previous public-knowledge default from being
// trivially exploitable while that's built.
func authenticateClient(payload Payload) bool {
	if agtDevKey == "" {
		utils.Error.Printf("authenticateClient: VISSR_AGT_DEV_KEY not set; refusing to issue AGT")
		return false
	}
	if !checkRoles(payload.Context) {
		return false
	}
	if payload.Proof != agtDevKey {
		return false
	}
	return true
}

// addCheckJti records a freshly-seen JTI in the replay cache and
// reports false if it was already there.
//
// Three bugs fixed here vs the original:
//   - #5: the cache used to be mutated without a mutex from
//     concurrent main-loop + deleteJti goroutines, which panics
//     under load (`fatal error: concurrent map writes`). All
//     accesses now go through jtiCacheMu.
//   - #6: the cache had no size cap, so an attacker could fill it
//     with bogus JTIs until the process OOMed. We now evict the
//     oldest entry when the cache hits MAX_JTI_CACHE_SIZE.
//   - #4 (in the caller): the caller now invokes this AFTER the
//     PoP signature has been verified, so an unauthenticated
//     attacker can no longer pollute the cache at line rate.
func addCheckJti(jti string) bool {
	jtiCacheMu.Lock()
	defer jtiCacheMu.Unlock()
	if jtiCache == nil {
		jtiCache = make(map[string]struct{})
	}
	if _, ok := jtiCache[jti]; ok {
		return false
	}
	// Evict oldest if we're at the cap.
	if len(jtiCache) >= MAX_JTI_CACHE_SIZE && len(jtiCacheOrder) > 0 {
		oldest := jtiCacheOrder[0]
		jtiCacheOrder = jtiCacheOrder[1:]
		delete(jtiCache, oldest)
	}
	jtiCache[jti] = struct{}{}
	jtiCacheOrder = append(jtiCacheOrder, jti)
	go deleteJti(jti)
	return true
}

// Deletes the JTI from cache after its natural lifetime expires.
func deleteJti(jti string) {
	time.Sleep((GAP + LIFETIME + 5) * time.Second)
	deleteJtiNow(jti)
}

// deleteJtiNow performs the actual cache removal — extracted so
// tests can exercise the eviction logic without waiting 308 seconds
// for the sleep in deleteJti.
func deleteJtiNow(jti string) {
	jtiCacheMu.Lock()
	delete(jtiCache, jti)
	// Lazy removal from the order list — O(n) but only when the
	// JTI expires naturally, which is bounded by MAX_JTI_CACHE_SIZE.
	for i, q := range jtiCacheOrder {
		if q == jti {
			jtiCacheOrder = append(jtiCacheOrder[:i], jtiCacheOrder[i+1:]...)
			break
		}
	}
	jtiCacheMu.Unlock()
}

// generate UUID
func getUUID() string {
	var unparsedId uuid.UUID
	var err error
	if unparsedId, err = uuid.NewRandom(); err != nil { // Generates a new uuid
		utils.Error.Printf("generateAgt:Error generating uuid, err=%s", err)
		return ""
	}
	return unparsedId.String()
}

// AGT_ISSUER is the value emitted in the `iss` claim of every AGT.
// The previous code emitted no `iss` at all, which meant the
// downstream atServer (or any other AGT consumer) could not tell
// AGTs from this issuer apart from any other RS256-signed token
// that happened to share the right structure. It also broke key
// rotation. Pinned to a stable value here; could be made
// env-configurable in a future PR.
const AGT_ISSUER = "vissr-agt-server"

// Generates Long Term AGT after doing all the checks related to it
func generateLTAgt(payload Payload, pop string) string {
	var popToken utils.PopToken
	err := popToken.Unmarshal(pop)
	if err != nil {
		utils.Error.Printf("generateLTAgt: Error unmarshalling pop, err = %s", err)
		return `{"action": "agt-request", "error": "Client request malformed"}`
	}
	// Bug 4 fix: verify the signature BEFORE touching the JTI
	// cache. The previous order let an unauthenticated attacker
	// pollute the cache (and OOM the process — see bug 6) by
	// flooding unsigned PoPs with random JTIs.
	err = popToken.CheckSignature()
	if err != nil {
		utils.Info.Printf("generateLTAgt: Invalid POP signature")
		return `{"action": "agt-request", "error": "Invalid POP signature"}`
	}
	if !addCheckJti(popToken.PayloadClaims["jti"]) {
		utils.Error.Printf("generateLTAgt: JTI used")
		return `{"action": "agt-request", "error": "Repeated JTI"}`
	}
	if ok, info := popToken.Validate(payload.Key, "vissv2/agts", GAP, LIFETIME); !ok {
		utils.Info.Printf("generateLTAgt: Not valid POP Token: %s", info)
		return `{"action": "agt-request", "error": "Invalid POP Token"}`
	}
	// Generates the response token
	var jwtoken utils.JsonWebToken
	iat := int(time.Now().Unix())
	exp := iat + LT_DURATION // defined by const
	jwtoken.SetHeader("RS256")
	jwtoken.AddClaim("vin", payload.Vin) // No need to check if it is filled, if not, it does nothing (new imp makes this claim not mandatory)
	jwtoken.AddClaim("iat", strconv.Itoa(iat))
	jwtoken.AddClaim("exp", strconv.Itoa(exp))
	jwtoken.AddClaim("clx", payload.Context)
	jwtoken.AddClaim("aud", "w3org/gen2")
	jwtoken.AddClaim("iss", AGT_ISSUER) // Bug 15 fix: emit issuer claim
	jwtoken.AddClaim("jti", getUUID())
	jwtoken.AddClaim("pub", payload.Key)
	jwtoken.Encode()
	jwtoken.AssymSign(privKey)
	return `{"action": "agt-request", "token":"` + jwtoken.GetFullToken() + `"}`
}

// Generates an AGT (short term)
func generateAgt(payload Payload) string {
	var jwtoken utils.JsonWebToken

	iat := int(time.Now().Unix())
	exp := iat + ST_DURATION
	// Token generation (used utils.JsonWebToken)
	jwtoken.SetHeader("RS256")
	jwtoken.AddClaim("vin", payload.Vin)
	jwtoken.AddClaim("iat", strconv.Itoa(iat))
	jwtoken.AddClaim("exp", strconv.Itoa(exp))
	jwtoken.AddClaim("clx", payload.Context)
	jwtoken.AddClaim("aud", "w3org/gen2")
	jwtoken.AddClaim("iss", AGT_ISSUER) // Bug 15 fix: emit issuer claim
	jwtoken.AddClaim("jti", getUUID())
	jwtoken.Encode()
	jwtoken.AssymSign(privKey)
	return `{"action": "agt-request", "token":"` + jwtoken.GetFullToken() + `"}`
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("agt_server", "AGT Server")
	// Create string flag
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
	}

	utils.InitLog("agtserver-log.txt", "./logs", *logFile, *logLevel)
	serverChan := make(chan string)
	muxServer := http.NewServeMux()
	initKey()

	go initAgtServer(serverChan, muxServer)

	for {
		request := <-serverChan
		pop := <-serverChan
		response := generateResponse(request, pop)
		utils.Info.Printf("agtServer response=%s", response)
		serverChan <- response
	}
}
