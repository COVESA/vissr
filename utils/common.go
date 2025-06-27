/**
* (C) 2023 Ford Motor Company
* (C) 2021 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
	"context"
	"github.com/qri-io/jsonschema"
)

const IpModel = 0 // IpModel = [0,1,2] = [localhost,extIP,envVarIP]
const IpEnvVarName = "GEN2MODULEIP"

var jsonSchema *jsonschema.Schema

// Access control values: none=0, write-only=1. read-write=2, consent +=10
// matrix preserving inherited value with read-write having priority over write-only and consent over no consent
var validationMatrix [5][5]int = [5][5]int{{0, 1, 2, 11, 12}, {1, 1, 2, 11, 12}, {2, 2, 2, 12, 12}, {11, 11, 12, 11, 12}, {12, 12, 12, 12, 12}}

func GetMaxValidation(newValidation int, currentMaxValidation int) int {
	return validationMatrix[translateToMatrixIndex(newValidation)][translateToMatrixIndex(currentMaxValidation)]
}

func translateToMatrixIndex(index int) int {
	switch index {
	case 0:
		return 0
	case 1:
		return 1
	case 2:
		return 2
	case 11:
		return 3
	case 12:
		return 4
	}
	return 0
}

type UdsReg struct {
	RootName     string `json:"root"`
	ServerFeeder string `json:"serverFeeder"`
	Redis        string `json:"redis"`
	Memcache     string `json:"memcache"`
	History      string `json:"history"`
}

var udsRegList []UdsReg

func ReadUdsRegistrations(sockFile string) []UdsReg {
	data, err := os.ReadFile(sockFile)
	if err != nil {
		Error.Printf("readUdsRegistrations():%s error=%s", sockFile, err)
		return nil
	}
	err = json.Unmarshal(data, &udsRegList)
	if err != nil {
		Error.Printf("readUdsRegistrations():unmarshal error=%s", err)
		return nil
	}
	return udsRegList
}

func GetUdsConn(path string, connectionName string) net.Conn {
	root := ExtractRootName(path)
	for i := 0; i < len(udsRegList); i++ {
		if root == udsRegList[i].RootName || udsRegList[i].RootName == "*" {
			return connectViaUds(getSocketPath(i, connectionName))
		}
	}
	return nil
}

func GetUdsPath(path string, connectionName string) string {
	root := ExtractRootName(path)
	//	Info.Printf("GetUdsPath:root=%s, connectionName=%s", root, connectionName)
	for i := 0; i < len(udsRegList); i++ {
		if root == udsRegList[i].RootName {
			return getSocketPath(i, connectionName)
		}
	}
	Info.Printf("could not find root name")
	return ""
}

func getSocketPath(listIndex int, connectionName string) string {
	switch connectionName {
	case "serverFeeder":
		return udsRegList[listIndex].ServerFeeder
	case "redis":
		return udsRegList[listIndex].Redis
	case "memcache":
		return udsRegList[listIndex].Memcache
	case "history":
		return udsRegList[listIndex].History
	default:
		Error.Printf("getSocketPath:Unknown connection name = %s", connectionName)
		return ""
	}
}

func connectViaUds(sockFile string) net.Conn {
	udsConn, err := net.Dial("unix", sockFile)
	if err != nil {
//		Error.Printf("connectViaUds:UDS Dial failed, err = %s", err)
		return nil
	}
	return udsConn
}

func GetServerIP() string {
	if value, ok := os.LookupEnv(IpEnvVarName); ok {
		Info.Println("ServerIP:", value)
		return value
	}
	Error.Printf("Environment variable %s is not set defaulting to localhost.", IpEnvVarName)
	return "localhost" //fallback
}

func GetModelIP(ipModel int) string {
	if ipModel == 0 {
		return "localhost"
	}
	if ipModel == 2 {
		if value, ok := os.LookupEnv(IpEnvVarName); ok {
			Info.Println("Host IP:", value)
			return value
		}
		Error.Printf("Environment variable %s error.", IpEnvVarName)
		return "localhost" //fallback
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		Error.Fatal(err.Error())
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	Info.Println("Host IP:", localAddr.IP)

	return localAddr.IP.String()
}

func MapRequest(request string, rMap *map[string]interface{}) int {
	decoder := json.NewDecoder(strings.NewReader(request))
	err := decoder.Decode(rMap)
	if err != nil {
		Error.Printf("extractPayload: JSON decode error=%s for request:%s", err, request)
		return -1
	}
	return 0
}

func UrlToPath(url string) string {
	var path string = strings.TrimPrefix(strings.Replace(url, "/", ".", -1), ".")
	return path[:]
}

func PathToUrl(path string) string {
	var url string = strings.Replace(path, ".", "/", -1)
	return "/" + url
}

func GenerateHmac(input string, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(input))
	return string(mac.Sum(nil))
}

func VerifyTokenSignature(token string, key string) error { // compatible with result from generateHmac()
	var jwt JsonWebToken
	err := jwt.DecodeFromFull(token)
	if err != nil {
		return err
	}
	return jwt.CheckSignature(key)

}

func ExtractFromToken(token string, claim string) string { // TODO remove white space sensitivity
	delimiter1 := strings.Index(token, ".")
	delimiter2 := strings.Index(token[delimiter1+1:], ".") + delimiter1 + 1
	header := token[:delimiter1]
	payload := token[delimiter1+1 : delimiter2]
	decodedHeaderByte, _ := base64.RawURLEncoding.DecodeString(header)
	decodedHeader := string(decodedHeaderByte)
	claimIndex := strings.Index(decodedHeader, claim)
	if claimIndex != -1 {
		startIndex := claimIndex + len(claim) + 2
		endIndex := strings.Index(decodedHeader[startIndex:], ",") + startIndex // ...claim":abc,...  or ...claim":"abc",... or See next line
		if endIndex == startIndex-1 {                                           // ...claim":abc}  or ...claim":"abc"}
			endIndex = len(decodedHeader) - 1
		}
		if string(decodedHeader[endIndex-1]) == `"` {
			endIndex--
		}
		if string(decodedHeader[startIndex]) == `"` {
			startIndex++
		}
		return decodedHeader[startIndex:endIndex]
	}
	decodedPayloadByte, _ := base64.RawURLEncoding.DecodeString(payload)
	decodedPayload := string(decodedPayloadByte)
	claimIndex = strings.Index(decodedPayload, claim)
	if claimIndex != -1 {
		startIndex := claimIndex + len(claim) + 2
		endIndex := strings.Index(decodedPayload[startIndex:], ",") + startIndex // ...claim":abc,...  or ...claim":"abc",... or See next line
		if endIndex == startIndex-1 {                                            // ...claim":abc}  or ...claim":"abc"}
			endIndex = len(decodedPayload) - 1
		}
		if string(decodedPayload[endIndex-1]) == `"` {
			endIndex--
		}
		if string(decodedPayload[startIndex]) == `"` {
			startIndex++
		}
		return decodedPayload[startIndex:endIndex]
	}
	return ""
}

func ExtractFromRequest(request string, parameterKey string) string {
	keyIndex := strings.Index(request, "\"" + parameterKey + "\":")
	if keyIndex != -1 {
		valueIndex1 := strings.Index(request[keyIndex+len(parameterKey)+3:], "\"")
		if valueIndex1 != -1 {
			valueIndex2 := strings.Index(request[keyIndex+len(parameterKey)+3+valueIndex1+1:], "\"")
			if valueIndex2 != -1 {
Info.Printf("ExtractFromRequest(%s):%s", parameterKey, request[keyIndex+len(parameterKey)+3+valueIndex1+1:keyIndex+len(parameterKey)+3+valueIndex1+1+valueIndex2])
				return request[keyIndex+len(parameterKey)+3+valueIndex1+1:keyIndex+len(parameterKey)+3+valueIndex1+1+valueIndex2]
			}
		}
	}
	return ""
}

func SetErrorResponse(reqMap map[string]interface{}, errRespMap map[string]interface{}, errorListIndex int, altErrorMessage string) {
	if reqMap["RouterId"] != nil {
		errRespMap["RouterId"] = reqMap["RouterId"]
	}
	if reqMap["action"] != nil {
		errRespMap["action"] = reqMap["action"]
	}
	if reqMap["requestId"] != nil {
		errRespMap["requestId"] = reqMap["requestId"]
	} else {
		delete(errRespMap, "requestId")
	}
	if reqMap["subscriptionId"] != nil {
		errRespMap["subscriptionId"] = reqMap["subscriptionId"]
	}
	errorMessage := ErrorInfoList[errorListIndex].Message
	if len(altErrorMessage) > 0 {
		errorMessage = altErrorMessage
	}
	errMap := map[string]interface{}{
		"number":  ErrorInfoList[errorListIndex].Number,
		"reason":  ErrorInfoList[errorListIndex].Reason,
		"description": errorMessage,
	}
	errRespMap["error"] = errMap
	errRespMap["ts"] = GetRfcTime()
}

func FinalizeMessage(responseMap map[string]interface{}) string {
	delete(responseMap, "origin")
	response, err := json.Marshal(responseMap)
	if err != nil {
		Error.Print("Server core-FinalizeMessage: JSON encode failed. ", err)
		return `{"error":{"number":400,"reason":"JSON marshal error","description":""}}` //???
	}
	return string(response)
}

func AddKeyValue(message string, key string, value string) string { // to avoid Marshal() to reformat using \"
	if len(value) > 0 {
		if value[0] == '{' {
			return message[:len(message)-1] + ", \"" + key + "\":" + value + "}"
		}
		return message[:len(message)-1] + ", \"" + key + "\":\"" + value + "\"}"
	}
	return message
}

func GetTimeInMilliSecs() string {
	return strconv.FormatInt(time.Now().UnixNano()/int64(time.Millisecond), 10)
}

func GetRfcTime() string {
	time.Now()
	rfcTime := time.Now().Format(time.RFC3339Nano) // 2020-05-01T15:34:35.123456789+02:00
	dotIndex := strings.Index(rfcTime, ".")
	if dotIndex != -1 {
		rfcTime = rfcTime[:dotIndex+4]
	} else if rfcTime[len(rfcTime)-6] == '+' {
		rfcTime = rfcTime[:len(rfcTime)-6]
	}
	return rfcTime + "Z"
}

func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func ExtractRootName(path string) string {
	dotDelimiter := strings.Index(path, ".")
	if dotDelimiter == -1 {
		return path
	}
	return path[:dotDelimiter]
}

type FilterObject struct {
	Type      string
	Parameter string
}

func UnpackFilter(filter interface{}, fList *[]FilterObject) { // See VISSv CORE, Filtering chapter for filter structure
	switch vv := filter.(type) {
	case []interface{}:
		Info.Println(filter, "is an array:, len=", strconv.Itoa(len(vv)))
		*fList = make([]FilterObject, len(vv))
		unpackFilterLevel1(vv, fList)
	case map[string]interface{}:
		Info.Println(filter, "is a map:")
		*fList = make([]FilterObject, 1)
		unpackFilterLevel2(0, vv, fList)
	default:
		Info.Println(filter, "is of an unknown type")
	}
}

func unpackFilterLevel1(filterArray []interface{}, fList *[]FilterObject) {
	i := 0
	for k, v := range filterArray {
		switch vv := v.(type) {
		case map[string]interface{}:
			Info.Println(k, "is a map:")
			unpackFilterLevel2(i, vv, fList)
		default:
			Info.Println(k, "is of an unknown type")
		}
		i++
	}
}

func unpackFilterLevel2(index int, filterExpression map[string]interface{}, fList *[]FilterObject) {
	for k, v := range filterExpression {
		switch vv := v.(type) {
		case string:
//			Info.Println(k, "is string", vv)
			if k == "variant" {
				(*fList)[index].Type = vv
			} else if k == "parameter" {
				(*fList)[index].Parameter = vv
			}
		case []interface{}:
//			Info.Println(k, "is an array:, len=", strconv.Itoa(len(vv)))
			arrayVal, err := json.Marshal(vv)
			if err != nil {
				Error.Print("UnpackFilter(): JSON array encode failed. ", err)
			} else if k == "parameter" {
				(*fList)[index].Parameter = string(arrayVal)
			}
		case map[string]interface{}:
//			Info.Println(k, "is a map:")
			opValue, err := json.Marshal(vv)
			if err != nil {
				Error.Print("UnpackFilter(): JSON map encode failed. ", err)
			} else {
				(*fList)[index].Parameter = string(opValue)
			}
		default:
			Info.Println(k, "is of an unknown type")
		}
	}
}

func IsNumber(value string) bool {
	_, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return false
	}
	return true
}

func IsBoolean(value string) bool {
	if value == "true" || value == "false" {
		return true
	}
	return false
}

func NextQuoteMark(message []byte, offset int) int {
	for i := offset; i < len(message); i++ {
		if message[i] == '"' {
			return i
		}
	}
	return offset
}

func JsonSchemaInit() {
	if jsonSchema != nil {
	Info.Printf("JSON schema already initiated")
		return
	}
	jsonSchemaStr := readSchema()
	if len(jsonSchemaStr) > 0{
        	jsonSchema = jsonschema.Must(jsonSchemaStr) // jsonSchema string read from file
		Info.Printf("JSON schema initiated")
        }
}

func readSchema() string {
	data, err := os.ReadFile("vissv3.0-schema.json")
	if err != nil {
		Error.Printf("JSON schema could not be read, error=%s", err)
		return ""
	}
	return string(data)
}

func JsonSchemaValidate(request string) string {
	errs, err := jsonSchema.ValidateBytes(context.Background(), []byte(request))
	if err != nil {
		return fixSyntax(err.Error())
	}
	if len(errs) > 0 {
		return fixSyntax(errs[0].Error())
	}
	return ""
}

func fixSyntax(errMessage string) string {
	errMessage = strings.Replace(errMessage, "/", "", -1)
	return errMessage
}
