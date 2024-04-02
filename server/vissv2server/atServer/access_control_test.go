/******** Peter Winzell  11/27/23 *********************************************/
/******** (C) Volvo Cars, 2023 **********/

package atServer

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	pb "github.com/covesa/vissr/grpc_pb"
	"github.com/covesa/vissr/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	_ "log"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// See Readme for prerequisites to run tests succesfully

type TResponseAT struct {
	Action string `json:"action"`
	Token  string `json:"aToken"`
}

type TResponseAGT struct {
	Action string `json:"action"`
	Token  string `json:"token"`
}

const agt_posttesturl = "http://0.0.0.0:7500/agts" // AGT server
const at_url = "http://0.0.0.0:8600/ats"           // AT server
const SHORT_TERM_TOKEN_LENGTH = 14400
const LONG_TERM_TOKEN_LENGTH = 345600

const PRIV_KEY_DIRECTORY = "../../agt_server/agt_private_key.rsa"

// TODO this types should reside in utils somewhere , duplicate code.
type agtPayload struct {
	Action  string `json:"action"`
	Vin     string `json:"vin"`
	Context string `json:"context"`
	Proof   string `json:"proof"`
	//Key     utils.JsonWebKey `json:"key"`
	Key string `json:"key"`
}

type atRequest struct {
	Action  string `json:"action"`
	Token   string `json:"agToken"`
	Purpose string `json:"purpose"`
	Pop     string `json:"pop"`
}

/******* TEST HELPER FUNCTIONS *****************/

func getAtRequestLT(agttoken string) *atRequest {
	var privKey *rsa.PrivateKey
	err := utils.ImportRsaKey(PRIV_KEY_DIRECTORY, &privKey)
	if err != nil {
		return nil
	}

	popToken := utils.PopToken{}
	token, _ := popToken.GenerateToken(privKey)
	return &atRequest{
		Action:  "at-request",
		Token:   agttoken,
		Purpose: "fuel-status",
		Pop:     token,
	}

}

func getAtToken(agttoken string, lt bool) (*http.Response, error) {

	var atReq *atRequest

	if lt {
		atReq = getAtRequestLT(agttoken)
	} else {
		atReq = &atRequest{
			Action:  "at-request",
			Token:   agttoken,
			Purpose: "fuel-status",
			Pop:     "",
		}
	}

	body, err := json.Marshal(atReq)
	if err != nil {
		return nil, err
	}

	r, err := http.NewRequest("POST", at_url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	r.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	return res, err
}

func parseAGTResponse(res http.Response, t *testing.T) TResponseAGT {

	defer res.Body.Close()

	post := TResponseAGT{}
	derr := json.NewDecoder(res.Body).Decode(&post)
	if derr != nil || post.Token == "" {
		t.Error("could not parse http response")
		return post
	}

	if res.StatusCode != http.StatusCreated {
		t.Error("status code expected to be 201 , got: ", res.StatusCode)

	}

	return post
}
func parseATResponse(res http.Response, t *testing.T) *TResponseAT {

	defer res.Body.Close()

	post := &TResponseAT{}
	derr := json.NewDecoder(res.Body).Decode(post)
	if derr != nil || post.Token == "" {
		t.Error("could not parse http response")
		return nil
	}

	if res.StatusCode != http.StatusCreated {
		t.Error("status code expected to be 201 , got: ", res.StatusCode)

	}

	return post
}

func getShToken(t *testing.T) string {
	res_ag, err := getShortTermAGTResponse() // Get Access Grant Token
	res, err := getAccessToken(res_ag, err, t)

	return decodeATPostResponse(t, res)
}

func getLTToken(t *testing.T) string {
	res_agt, err := getLongTermAGTResponse()
	res, err := getAccessToken(res_agt, err, t)

	return decodeATPostResponse(t, res)
}

func getAccessToken(agt *http.Response, err error, t *testing.T) (*http.Response, error) {
	if err != nil {
		t.Error(err)
		return nil, nil
	}
	agtResponse := parseAGTResponse(*agt, t)

	res, err := getAtToken(agtResponse.Token, true)
	if err != nil {
		t.Error(err)
		return nil, nil
	}

	return res, err
}

func decodeATPostResponse(t *testing.T, res *http.Response) string {
	if res.StatusCode != http.StatusCreated {
		t.Error("status code expected to be 201 , got: ", res.StatusCode)
		return ""
	} else {
		attokenpost := &TResponseAT{}
		derr := json.NewDecoder(res.Body).Decode(attokenpost)

		if derr != nil {
			t.Error(derr)
		}
		log.Println(attokenpost.Token)
		return attokenpost.Token
	}
}

func getGRPCServerConnection() (*grpc.ClientConn, error) {
	var connection *grpc.ClientConn
	connection, err := grpc.Dial("0.0.0.0"+":8887", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	return connection, nil
}

func getVISSClient(connection *grpc.ClientConn) pb.VISSv2Client {
	client := pb.NewVISSv2Client(connection)
	return client
}

func infuseTokengRPCGetRequest(at_token string) []string {

	commandList := make([]string, 2)
	str := fmt.Sprintf(`{"action":"get","path":"Vehicle/Body/Lights","filter":{"type":"paths","parameter":"*"},,"authorization":"%s","requestId":"235"}`, at_token)
	commandList[0] = str
	str = fmt.Sprintf(`{"action":"get","path":"Vehicle/Speed","authorization":"%s","requestId":"236"}`, at_token)
	commandList[1] = str

	return commandList
}

func getShortTermAGTResponse() (*http.Response, error) {
	body := []byte(`{"action":"agt-request","vin":"GEO001","context":"Independent+OEM+Cloud","proof":"ABC","key":"DEF"}`)

	r, err := http.NewRequest("POST", agt_posttesturl, bytes.NewBuffer(body))
	if err != nil {
		panic(err)
	}
	r.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(r)
	if err != nil {
		panic(err)
	}
	return res, err
}

func getLongTermAGTResponse() (*http.Response, error) {
	var privKey *rsa.PrivateKey
	err := utils.ImportRsaKey(PRIV_KEY_DIRECTORY, &privKey)
	if err != nil {
		return nil, err
	}

	popToken := utils.PopToken{}
	token, err := popToken.GenerateToken(privKey) // generate a proof-of-possession-token to get a LT token.

	agtP := agtPayload{
		Action:  "agt-request",
		Vin:     "GEO001",
		Context: "Independent+OEM+Cloud",
		Proof:   "ABC",
		Key:     popToken.Jwk.Thumb, // Thumb print need to match.
	}

	body, err := json.Marshal(agtP) // POST a acces grant token request

	r, err := http.NewRequest("POST", agt_posttesturl, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("PoP", token) // add the Pop as a proof of possesion.

	client := &http.Client{}

	return client.Do(r)
}

/**** ACTUAL TESTS, testing AGT short term token , long term token requests ******************************************/

// AGT Server must be running
func TestShortTermTokenAccess(t *testing.T) {

	res, err := getShortTermAGTResponse()
	if err != nil {
		t.Error(err)
	}

	defer res.Body.Close()

	post := &TResponseAGT{}
	derr := json.NewDecoder(res.Body).Decode(post)
	if derr != nil {
		panic(derr)
	}

	if res.StatusCode != http.StatusCreated {
		t.Error("status code expected to be 201 , got: ", res.StatusCode)
	}

	log.Printf("got token = %s", post.Token)
	var Agt utils.ExtendedJwt
	err = Agt.DecodeFromFull(post.Token) // parsing the JWT token
	vin := Agt.PayloadClaims["vin"]
	ctx := Agt.PayloadClaims["clx"]
	iat, err := strconv.Atoi(Agt.PayloadClaims["iat"])
	exp, err := strconv.Atoi(Agt.PayloadClaims["exp"])

	//test that it is correct length
	if (exp - iat) != SHORT_TERM_TOKEN_LENGTH {
		t.Error("short term token error: ", exp-iat)
	}
	//test vin and context
	if vin != "GEO001" {
		t.Error("Vin does not match => ", vin)
	}
	if ctx != "Independent+OEM+Cloud" {
		t.Error("roles fails to match => ", ctx)
	}

}

func TestLongTermTokenAccess(t *testing.T) {

	res, err := getLongTermAGTResponse()
	if err != nil {
		panic(err)
	}

	defer res.Body.Close()

	post := &TResponseAGT{}
	derr := json.NewDecoder(res.Body).Decode(post)
	if derr != nil {
		panic(derr)
	}

	if res.StatusCode != http.StatusCreated {
		t.Error("status code expected to be 201 , got: ", res.StatusCode)
	}

	log.Printf("got token = %s", post.Token)
	var Agt utils.ExtendedJwt
	err = Agt.DecodeFromFull(post.Token) // parsing the JWT tokenU
	vin := Agt.PayloadClaims["vin"]
	ctx := Agt.PayloadClaims["clx"]
	iat, err := strconv.Atoi(Agt.PayloadClaims["iat"])
	exp, err := strconv.Atoi(Agt.PayloadClaims["exp"])

	//test that it is correct length
	if (exp - iat) != LONG_TERM_TOKEN_LENGTH {
		t.Error("long term token error: ", exp-iat)
	}
	//test vin and context
	if vin != "GEO001" {
		t.Error("Vin does not match => ", vin)
	}
	if ctx != "Independent+OEM+Cloud" {
		t.Error("roles fails to match => ", ctx)
	}

}

// Viss server must be up and running, the at server resides in the vissv2server process.
func TestAtTokenAccess_ST(t *testing.T) {
	getShToken(t)
}

func TestAtTokenAccess_LT(t *testing.T) {
	res_ag, err := getLongTermAGTResponse() // Get Access Grant Token
	if err != nil {
		t.Error(err)
	}

	ltAGTResponse := parseAGTResponse(*res_ag, t)
	res, err := getAtToken(ltAGTResponse.Token, true)

	if err != nil {
		t.Error(err)
	}

	if res.StatusCode != http.StatusCreated {
		t.Error("status code expected to be 201 , got: ", res.StatusCode)
	} else {
		attokenpost := &TResponseAT{}
		derr := json.NewDecoder(res.Body).Decode(attokenpost)

		if derr != nil {
			t.Error(derr)
		}
		log.Println(attokenpost.Token)
	}

}

// Test actual requests against server, server and a feeder for south-bound must be running.
// tested with the public demo at Remotivelabs. see Readme for setup.

func TestGetAccessControlST(t *testing.T) {

	utils.InitLog("servercore-log.txt", "./logs", false, "Info")

	AT := getShToken(t)
	if AT != "" {
		grpcConnectiontion, err := getGRPCServerConnection()
		if err != nil {
			t.Error(err)
		}
		defer grpcConnectiontion.Close()
		vissClient := getVISSClient(grpcConnectiontion)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		request := infuseTokengRPCGetRequest(AT)
		pbRequest := utils.GetRequestJsonToPb(request[1], utils.PB_LEVEL1)
		pbResponse, err := vissClient.GetRequest(ctx, pbRequest)

		if err != nil {
			t.Error(err)
			return
		}
		vssResponse := utils.GetResponsePbToJson(pbResponse, utils.PB_LEVEL1)
		t.Log(vssResponse)
	} else {
		t.Error("AT token not delivered")
	}
}

func TestGetAccessControlLT(t *testing.T) {
	AT := getLTToken(t)
	if AT != "" {
		grpcConnectiontion, err := getGRPCServerConnection()
		if err != nil {
			t.Error(err)
		}
		defer grpcConnectiontion.Close()
		vissClient := getVISSClient(grpcConnectiontion)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		request := infuseTokengRPCGetRequest(AT)
		pbRequest := utils.GetRequestJsonToPb(request[1], utils.PB_LEVEL1)
		pbResponse, err := vissClient.GetRequest(ctx, pbRequest)

		if err != nil {
			t.Error(err)
			return
		}
		vssResponse := utils.GetResponsePbToJson(pbResponse, utils.PB_LEVEL1)
		t.Log(vssResponse)
	} else {
		t.Error("AT token not delivered")
	}
}
