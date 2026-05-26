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
	"fmt"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
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
	ni := translateToMatrixIndex(newValidation)
	ci := translateToMatrixIndex(currentMaxValidation)
	// translateToMatrixIndex returns 0 for any value not in {0,1,2,11,12}.
	// When that happens for a non-zero input the matrix lookup would silently
	// return 0 instead of the correct max, so fall back to plain integer max.
	if (ni == 0 && newValidation != 0) || (ci == 0 && currentMaxValidation != 0) {
		if newValidation > currentMaxValidation {
			return newValidation
		}
		return currentMaxValidation
	}
	return validationMatrix[ni][ci]
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

// udsRegList is the in-memory copy of the UDS registration file.
// udsRegListMu guards writes from ReadUdsRegistrations against
// concurrent reads from GetUdsConn / GetUdsPath in other goroutines
// (bug-9 fix).
var udsRegList []UdsReg
var udsRegListMu sync.RWMutex

func ReadUdsRegistrations(sockFile string) []UdsReg {
	data, err := os.ReadFile(sockFile)
	if err != nil {
		Error.Printf("readUdsRegistrations():%s error=%s", sockFile, err)
		return nil
	}
	var parsed []UdsReg
	if err := json.Unmarshal(data, &parsed); err != nil {
		Error.Printf("readUdsRegistrations():unmarshal error=%s", err)
		return nil
	}
	udsRegListMu.Lock()
	udsRegList = parsed
	udsRegListMu.Unlock()
	return parsed
}

func GetUdsConn(path string, connectionName string) net.Conn {
	root := ExtractRootName(path)
	udsRegListMu.RLock()
	defer udsRegListMu.RUnlock()
	for i := 0; i < len(udsRegList); i++ {
		if root == udsRegList[i].RootName || udsRegList[i].RootName == "*" {
			// getSocketPath also reads udsRegList — but RLock is
			// re-entrant on the same goroutine's read.
			return connectViaUds(getSocketPathLocked(i, connectionName))
		}
	}
	return nil
}

func GetUdsPath(path string, connectionName string) string {
	root := ExtractRootName(path)
	udsRegListMu.RLock()
	defer udsRegListMu.RUnlock()
	for i := 0; i < len(udsRegList); i++ {
		if root == udsRegList[i].RootName {
			return getSocketPathLocked(i, connectionName)
		}
	}
	Info.Printf("could not find root name")
	return ""
}

// getSocketPath is the public entry point — takes the RLock itself.
// Internal call sites that already hold the lock use getSocketPathLocked.
func getSocketPath(listIndex int, connectionName string) string {
	udsRegListMu.RLock()
	defer udsRegListMu.RUnlock()
	return getSocketPathLocked(listIndex, connectionName)
}

func getSocketPathLocked(listIndex int, connectionName string) string {
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

// ExtractFromToken returns the value of the named claim in a JWT,
// searching the header first and then the payload. Returns "" when
// the token is malformed or the claim is missing.
//
// Bug fixes (security):
//   - #4: the previous implementation used the raw `strings.Index(..., ".")`
//     return value as a slice bound without checking for -1, which
//     panicked on tokens that didn't have exactly two dots. JWTs are
//     attacker-controlled, so this was an unauthenticated panic-DoS.
//   - #4: base64 decode errors were silently swallowed; the resulting
//     empty string produced negative slice indices and panicked
//     downstream.
//   - #5: the claim name was matched via `strings.Index`, so `aud`
//     would match inside `"audience":` (or `iss` inside `"missing"`,
//     etc.) and the byte-offset math returned garbage that callers
//     compared as if it were the real claim value.
//
// Replaced the hand-rolled offset scanning with `encoding/json`:
// parse the header and payload as proper JSON, look up the claim by
// exact key, and stringify the value. JSON parsing rejects malformed
// inputs and returns errors instead of panicking.
func ExtractFromToken(token string, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	if v, ok := lookupClaim(parts[0], claim); ok {
		return v
	}
	if v, ok := lookupClaim(parts[1], claim); ok {
		return v
	}
	return ""
}

// lookupClaim base64-decodes a JWT segment, parses it as JSON, and
// returns the named claim's stringified value. Returns (_, false) if
// the segment is malformed or the claim is absent.
func lookupClaim(base64Segment, claim string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(base64Segment)
	if err != nil {
		return "", false
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", false
	}
	v, ok := m[claim]
	if !ok {
		return "", false
	}
	switch typed := v.(type) {
	case string:
		return typed, true
	case float64:
		// JSON numbers come back as float64; format losslessly for
		// integer-valued numbers (iat, exp) — which is the common case.
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10), true
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	case nil:
		return "", true
	default:
		// Nested objects / arrays — re-marshal to JSON so callers
		// can introspect further if they want.
		out, err := json.Marshal(typed)
		if err != nil {
			return "", false
		}
		return string(out), true
	}
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
	// Bug-15 fix: bounds-check the error index before subscripting
	// ErrorInfoList. An out-of-range index (programmer mistake at a
	// call site) used to panic the handler. Now we synthesize a
	// "Unknown error" entry instead so the request still gets an
	// error response, and we log the bad index for debugging.
	if errorListIndex < 0 || errorListIndex >= len(ErrorInfoList) {
		Error.Printf("SetErrorResponse: errorListIndex %d out of range [0,%d)", errorListIndex, len(ErrorInfoList))
		errRespMap["error"] = map[string]interface{}{
			"number":      "500",
			"reason":      "internal_error",
			"description": fmt.Sprintf("invalid error code %d", errorListIndex),
		}
		errRespMap["ts"] = GetRfcTime()
		return
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

// GetRfcTime returns a millisecond-precision RFC3339 timestamp in
// UTC. The previous implementation truncated the fractional part to
// 4 chars after the dot when present, and stripped a "+HH:MM"
// offset when absent — but it didn't handle the "Z" (UTC) or
// "-HH:MM" (negative offset) cases, so on a host whose local time
// zone produced "-04:00" you'd get strings like
// "2025-01-01T00:00:00-04:00Z". (Bug-12 fix.)
func GetRfcTime() string {
	// Always work in UTC to make the timestamp canonical regardless
	// of the host's local timezone.
	rfcTime := time.Now().UTC().Format(time.RFC3339Nano) // ...Z or ...+00:00
	dotIndex := strings.Index(rfcTime, ".")
	if dotIndex != -1 && dotIndex+4 <= len(rfcTime) {
		rfcTime = rfcTime[:dotIndex+4]
	} else if strings.HasSuffix(rfcTime, "Z") {
		rfcTime = strings.TrimSuffix(rfcTime, "Z")
	} else if len(rfcTime) >= 6 && (rfcTime[len(rfcTime)-6] == '+' || rfcTime[len(rfcTime)-6] == '-') {
		rfcTime = rfcTime[:len(rfcTime)-6]
	}
	return rfcTime + "Z"
}

// FileExists reports whether filename refers to an existing
// non-directory file. Bug-11 fix: the previous implementation
// returned `!info.IsDir()` even when err was a non-NotExist error
// (e.g. EACCES on a directory we can't stat). That dereferenced a
// nil info pointer. Now any non-nil err returns false.
func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
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
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	return !math.IsNaN(f) && !math.IsInf(f, 0)
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

// jsonSchemaOnce ensures concurrent callers (multiple manager
// subsystems start in parallel) don't race on writing the
// package-global jsonSchema. (Bug-10 fix.)
var jsonSchemaOnce sync.Once

func JsonSchemaInit() {
	jsonSchemaOnce.Do(func() {
		if jsonSchema != nil {
			Info.Printf("JSON schema already initiated")
			return
		}
		jsonSchemaStr := readSchema()
		if len(jsonSchemaStr) > 0 {
			jsonSchema = jsonschema.Must(jsonSchemaStr) // jsonSchema string read from file
			Info.Printf("JSON schema initiated")
		}
	})
}

func readSchema() string {
	data, err := os.ReadFile("vissv3.0-schema.json")
	if err != nil {
		Error.Printf("JSON schema could not be read, error=%s", err)
		return ""
	}
	return string(data)
}

// JsonSchemaValidate validates request against the loaded schema.
// Bug-6 fix: previously the function called jsonSchema.ValidateBytes
// unconditionally, panicking with a nil-pointer dereference when
// schema-file loading had failed at init() (e.g. running from the
// wrong working directory in tests). Now we surface the unavailable
// state as a returned error string so callers can decide how to
// degrade.
func JsonSchemaValidate(request string) string {
	if jsonSchema == nil {
		return "JSON schema not loaded (call JsonSchemaInit and ensure the schema file is reachable)"
	}
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
