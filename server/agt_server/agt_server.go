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
	"net/http"
	"os"
	"strconv"
	"strings"
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

var privKey *rsa.PrivateKey

// Stores a cache of the jwt ids received to not be reused
var jtiCache map[string]struct{}

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
			} else { // POST REQUEST TO /agts
				utils.Info.Printf("agtServer:received POST request=%s\n", string(bodyBytes))
				serverChannel <- string(bodyBytes) // Sends to serverChannel the body of the request
				pop := string(req.Header.Get("PoP"))
				if pop != "" {
					utils.Info.Printf("agtServer: received PoP = %s", pop)
				}
				serverChannel <- pop // Sends the pop to serverChannel
				response := <-serverChannel
				utils.Info.Printf("agtServer:POST response=%s", response)
				// Response generation
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
		utils.Info.Printf("initAgtServer():Starting AGT Server without TLS on %s/agts", PORT)
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
	delimiter2 := strings.Index(context[delimiter1+1:], "+")
	if !checkUserRole(context[:delimiter1]) || !checkAppRole(context[delimiter1+1:delimiter1+1+delimiter2]) || !checkDeviceRole(context[delimiter1+1+delimiter2+1:]) {
		return false
	}
	return true

}

// Checks client context + Proof of context => Not implemented a complex validation
func authenticateClient(payload Payload) bool {
	if checkRoles(payload.Context) && payload.Proof == "ABC" { // a bit too simple validation...
		return true
	}
	return false
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

// Deletes the JTI from cache
func deleteJti(jti string) {
	time.Sleep((GAP + LIFETIME + 5) * time.Second)
	delete(jtiCache, jti)
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

// Generates Long Term AGT after doing all the checks related to it
func generateLTAgt(payload Payload, pop string) string {
	var popToken utils.PopToken
	err := popToken.Unmarshal(pop)
	if err != nil {
		utils.Error.Printf("generateLTAgt: Error unmarshalling pop, err = %s", err)
		return `{"action": "agt-request", "error": "Client request malformed"}`
	}
	if !addCheckJti(popToken.PayloadClaims["jti"]) {
		utils.Error.Printf("generateLTAgt: JTI used")
		return `{"action": "agt-request", "error": "Repeated JTI"}`
	}
	err = popToken.CheckSignature()
	if err != nil {
		utils.Info.Printf("generateLTAgt: Invalid POP signature")
		return `{"action": "agt-request", "error": "Invalid POP signature"}`
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
	jwtoken.AddClaim("jti", getUUID())
	//utils.Info.Printf("generateAgt:jwtHeader=%s", jwtoken.GetHeader())
	//utils.Info.Printf("generateAgt:jwtPayload=%s", jwtoken.GetPayload())
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
