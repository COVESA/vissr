/************
*	File implementing data types for an easier developing of the implementation
*
*	Author: Jose Jesus Sanchez Gomez (sanchezg@lcc.uma.es)
*	2021, NICS Lab (University of Malaga)
*
*************/

package utils

import (
	"os"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const UIDLEN = 4
type FileTransferCache struct {
	Uid [UIDLEN]byte
	UploadTransfer bool //true=upload, false=download
	Path string  // from statestorage
	Name string  // incl file ext. Download from client, upload from statestorage.
	FileDescriptor *os.File
	FileOffset int
	ChunkSize int
	Hash string  // hex format. SHA-1 ger 20 bytes output vilet i hex blir 40 chars + 2 för 0x
	MessageNo int32
	PreviousChunksize int32
	Timestamp uint64  //time of creation
	Status int  //zero=ok, non-zero=nok
}

// Gets Json string (or nothing) and adds received key and value, if it doesnt
// receive a value or key, it does nothing. If the value starts with `{` or `[`
// it is treated as already-encoded JSON; otherwise it is treated as a string
// literal and is JSON-encoded (which escapes `"`, `\`, and control chars)
// before being inserted. The key is encoded the same way.
func JsonRecursiveMarshall(key string, value string, jplain *string) {
	if key == "" || value == "" || jplain == nil {
		return
	}
	encodedKey := mustJsonString(key)
	var encodedValue string
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		// Caller asserts this is already a JSON object/array literal.
		encodedValue = value
	} else {
		encodedValue = mustJsonString(value)
	}
	if *jplain == "" {
		*jplain = "{" + encodedKey + ":" + encodedValue + "}"
	} else if strings.HasSuffix(*jplain, "}") {
		*jplain = (*jplain)[:len(*jplain)-1] + "," + encodedKey + ":" + encodedValue + "}"
	} else {
		// Malformed accumulator; refuse to corrupt it further.
		Error.Printf("JsonRecursiveMarshall: accumulator does not end in '}' (got %q); skipping key=%q", *jplain, key)
	}
}

// mustJsonString returns the JSON-encoded representation of s (surrounded by
// quotes and with metacharacters escaped). Failure is impossible for a Go
// string but we guard for safety.
func mustJsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string can only fail on extremely pathological
		// invalid-UTF8 input. Fall back to a defensive double-quote that
		// preserves length so downstream length checks don't go wild.
		Error.Printf("mustJsonString: json.Marshal failed for %q: %v", s, err)
		return `""`
	}
	return string(b)
}

// *********				JSON WEB TOKEN 										***********
// *********	Basic JWT including Header, Payload and encoded parts.
// *********	Methods for decoding and signature check avaliable
type JsonWebToken struct {
	Header           string
	Payload          string
	EncodedHeader    string
	EncodedPayload   string
	EncodedSignature string
}

// Sets the algorithm used
func (token *JsonWebToken) SetHeader(algorithm string) {
	token.Header = `{"alg":"` + algorithm + `","typ":"JWT"}`
}

// Adds a claim to the header
func (token *JsonWebToken) AddHeader(key string, value string) {
	JsonRecursiveMarshall(key, value, &token.Header)
}

// Adds a claim to the payload
func (token *JsonWebToken) AddClaim(key string, value string) {
	JsonRecursiveMarshall(key, value, &token.Payload)
}

// Encodes the Token
func (token *JsonWebToken) Encode() {
	token.EncodedHeader = base64.RawURLEncoding.EncodeToString([]byte(token.Header))
	token.EncodedPayload = base64.RawURLEncoding.EncodeToString([]byte(token.Payload))
}

// Signs the token using an assymetric key
func (token *JsonWebToken) AssymSign(privKey crypto.PrivateKey) error {
	token.Encode()
	var signature []byte
	var err error
	hashed := sha256.Sum256([]byte(token.EncodedHeader + "." + token.EncodedPayload)) //SHA 256 HASH
	switch typ := privKey.(type) {
	case *rsa.PrivateKey:
		rsaPriv, _ := privKey.(*rsa.PrivateKey)
		signature, err = rsa.SignPKCS1v15(rand.Reader, rsaPriv, crypto.SHA256, hashed[:])
		if err != nil {
			return err
		}
	case *ecdsa.PrivateKey: // https://datatracker.ietf.org/doc/html/rfc7518#section-3.4
		ecdsaPriv, _ := privKey.(*ecdsa.PrivateKey)
		rSign, sSign, err := ecdsa.Sign(rand.Reader, ecdsaPriv, hashed[:])
		if err != nil {
			return err
		}
		// RFC 7518 §3.4 requires R and S to each be exactly the curve byte
		// length, zero-padded on the left. Big.Int.Bytes() returns the
		// minimum number of bytes, so we must pad explicitly.
		curveByteLen := (ecdsaPriv.Curve.Params().BitSize + 7) / 8
		signature = make([]byte, 2*curveByteLen)
		rBytes := rSign.Bytes()
		sBytes := sSign.Bytes()
		copy(signature[curveByteLen-len(rBytes):curveByteLen], rBytes)
		copy(signature[2*curveByteLen-len(sBytes):], sBytes)
	default:
		return fmt.Errorf("error: can not sign jwt: invalid key type: %T", typ)
	}
	token.EncodedSignature = base64.RawURLEncoding.EncodeToString(signature)
	return nil
}

// Signs the token using a symmetric key
func (token *JsonWebToken) SymmSign(key string) {
	token.Encode()
	token.EncodedSignature = base64.RawURLEncoding.EncodeToString([]byte(GenerateHmac(token.EncodedHeader+"."+token.EncodedPayload, key)))
}

// Returns the full token
func (token JsonWebToken) GetFullToken() string {
	return token.EncodedHeader + "." + token.EncodedPayload + "." + token.EncodedSignature
}

// Returns the header of the token
func (token JsonWebToken) GetHeader() string {
	return token.Header
}

// Returns the payload of the token
func (token JsonWebToken) GetPayload() string {
	return token.Payload
}

// From a signed jwt received, gets header and payload
func (token *JsonWebToken) DecodeFromFull(input string) error {
	parts := strings.Split(input, ".")
	if len(parts) != 3 {
		return errors.New("JWT not composed by 3 parts")
	}
	token.EncodedHeader = parts[0]
	token.EncodedPayload = parts[1]
	token.EncodedSignature = parts[2]
	header, err := base64.RawURLEncoding.DecodeString(token.EncodedHeader)
	if err != nil {
		return err
	}
	token.Header = string(header)
	payload, err := base64.RawURLEncoding.DecodeString(token.EncodedPayload)
	if err != nil {
		return err
	}
	token.Payload = string(payload)
	return nil
}

// extractAlg parses the JWT header JSON and returns the value of the
// `alg` claim. Used by CheckSignature to dispatch on the algorithm
// *as declared in the token's header JSON* rather than a substring
// match on the raw header bytes.
//
// The previous implementation used strings.Contains(header, "HS256")
// which had two security holes:
//   1. "HS256" appearing in any other claim (jku, typ, kid, ...)
//      would force HMAC verification with a caller-supplied symmetric
//      key, even though the token was actually RSA/ECDSA-signed.
//   2. Any header that did NOT contain "HS256" (including
//      `"alg":"none"`) fell through to CheckAssymSignature, which
//      attempted asymmetric verification with whatever key was passed
//      — accepting `alg:none` tokens whenever the caller's key
//      check happened to no-op.
func extractAlg(header string) string {
	var hdr struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal([]byte(header), &hdr); err != nil {
		return ""
	}
	return hdr.Alg
}

// Checks if the token is signed correctly. In case of symm sign, key as string must be passed. In case of assym, a crypto.PublicKey must be passed.
//
// Hardening (security bugs 1 and 2 from the utils/crypto audit):
//   - The HMAC tag comparison is done via hmac.Equal (constant-time)
//     rather than the previous `==` string compare, which was a
//     classic timing-attack vector.
//   - The algorithm is parsed from the header JSON's `alg` field, not
//     substring-matched on the raw header. The `none` algorithm is
//     refused outright; unknown algorithms are refused.
func (token JsonWebToken) CheckSignature(key interface{}) error {
	alg := extractAlg(token.Header)
	switch alg {
	case "HS256":
		strKey, ok := key.(string)
		if !ok {
			return errors.New("HS256: key must be a string")
		}
		expected := []byte(base64.RawURLEncoding.EncodeToString([]byte(GenerateHmac(token.EncodedHeader+"."+token.EncodedPayload, strKey))))
		presented := []byte(token.EncodedSignature)
		if !hmac.Equal(expected, presented) {
			return errors.New("invalid hs256 signature")
		}
		return nil
	case "RS256", "ES256":
		return token.CheckAssymSignature(key)
	case "none", "":
		return errors.New("refusing token with alg=none or missing alg")
	default:
		return fmt.Errorf("unsupported alg: %q", alg)
	}
}

// Checks the assymetric signature of the token
func (token JsonWebToken) CheckAssymSignature(key crypto.PublicKey) (err error) {
	signature, err := base64.RawURLEncoding.DecodeString(token.EncodedSignature)
	if err != nil {
		return err
	}
	switch typ := key.(type) {
	case *rsa.PublicKey:
		pubKey := key.(*rsa.PublicKey)
		//Checks signature ParsePKIXPublicKey
		msgHasher := sha256.New()
		msgHasher.Write([]byte(token.EncodedHeader + "." + token.EncodedPayload))
		msgHash := msgHasher.Sum(nil)
		err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, msgHash, signature)
		return err
	case *ecdsa.PublicKey:
		pubKey := key.(*ecdsa.PublicKey)
		// https://datatracker.ietf.org/doc/html/rfc7518#section-3.4
		if pubKey.Curve != elliptic.P256() {
			return errors.New("elliptic curve type not supported")
		}
		// RFC 7518 §3.4: signature is the concatenation of R and S, each
		// the curve's byte length. For P-256 that's exactly 64 bytes.
		curveByteLen := (pubKey.Curve.Params().BitSize + 7) / 8
		if len(signature) != 2*curveByteLen {
			return fmt.Errorf("invalid ecdsa signature length: got %d bytes, want %d", len(signature), 2*curveByteLen)
		}
		r := new(big.Int).SetBytes(signature[:curveByteLen])
		s := new(big.Int).SetBytes(signature[curveByteLen:])
		// We have to hash the token to check it
		hashed := sha256.Sum256([]byte(token.EncodedHeader + "." + token.EncodedPayload))
		if !ecdsa.Verify(pubKey, hashed[:], r, s) {
			err = errors.New("invalid ecdsa signature")
		}
		return err
	default:
		return fmt.Errorf("public key alg not supported: %T", typ)
	}
}

// *********				EXTENDED JSON WEB TOKEN								***********
// *********	Extends the JsonWebToken type, including a map with the claims in header
// *********	and a map with the claims in payload
type ExtendedJwt struct {
	Token         JsonWebToken
	HeaderClaims  map[string]string
	PayloadClaims map[string]string
}

func (ext *ExtendedJwt) DecodeFromFull(input string) error {
	err := ext.Token.DecodeFromFull(input)
	if err != nil {
		return err
	}
	err = json.Unmarshal([]byte(ext.Token.Header), &ext.HeaderClaims)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(ext.Token.Payload), &ext.PayloadClaims)
}

// *********				POP TOKEN 											***********
// *********	POP Token is used by the client to attest its possession of a private key
// *********	More info in the README of the repo
type PopToken struct {
	HeaderClaims  map[string]string // TYP, ALG, JWK
	PayloadClaims map[string]string // IAT, JTI
	Jwk           JsonWebKey
	Jwt           JsonWebToken
}

// stripJsonQuotes returns the contents of a RawMessage with the
// surrounding double-quotes removed when the value looks like a
// JSON string. If the value is shorter than 2 bytes or doesn't
// start+end with a double-quote, the original bytes are returned
// as a string.
//
// Bug fix: the previous inline expression `string(value[1:len(value)-1])`
// panicked with "slice bounds out of range" when the JSON value
// had fewer than 2 raw bytes — easily reachable via a PoP claim
// like {"foo": 0} (a numeric literal). Since PoP token contents
// are attacker-controlled, this was an unauthenticated DoS panic
// in the AGT server.
func stripJsonQuotes(value json.RawMessage) string {
	if len(value) < 2 {
		return string(value)
	}
	if value[0] == '"' && value[len(value)-1] == '"' {
		return string(value[1 : len(value)-1])
	}
	return string(value)
}

// Gets the received PoP token as string, and unmarshalls it. JWK, JWT and claims fields are all filled
func (popToken *PopToken) Unmarshal(token string) error {
	popToken.HeaderClaims = make(map[string]string)
	popToken.PayloadClaims = make(map[string]string)
	// Decodes full token into header and payload; propagate parse failures.
	if err := popToken.Jwt.DecodeFromFull(token); err != nil {
		return err
	}
	// Starting with header
	var headerMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(popToken.Jwt.Header), &headerMap); err != nil {
		return err
	}
	for key, value := range headerMap {
		popToken.HeaderClaims[key] = rawMessageToClaim(value)
	}
	if rawJwk, present := headerMap["jwk"]; present {
		popToken.HeaderClaims["jwk"] = string(rawJwk) // Key must be unmarshalled as-is
	}
	// Then we decode the key
	if popToken.HeaderClaims["jwk"] != "" {
		if err := popToken.Jwk.Unmarshall(popToken.HeaderClaims["jwk"]); err != nil {
			return errors.New("can not decode key in poptoken")
		}
	}
	// Continue with payload. Bug fix: check the Unmarshal error
	// BEFORE iterating payloadMap. The previous code ignored the
	// error and continued, which could leave callers thinking a
	// PoP was well-formed when it wasn't (e.g. AGT server cached
	// the JTI before signature check — bug 4 in agt_server.go).
	var payloadMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(popToken.Jwt.Payload), &payloadMap); err != nil {
		return err
	}
	for key, value := range payloadMap {
		popToken.PayloadClaims[key] = rawMessageToClaim(value)
	}
	return nil
}

// rawMessageToClaim normalizes a JWT claim value into a plain Go string for
// storage in the *Claims maps. JSON strings are unquoted; numbers/bools/null
// keep their JSON textual form; objects and arrays keep their full JSON
// (so the caller can re-parse if needed).
func rawMessageToClaim(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try to decode as a string first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Otherwise return the JSON literal as-is (number, bool, null, object, array).
	return string(raw)
}

// Initializes popToken from claims and public key. Make sure the private key used to sign is the same used to initialize
func (popToken *PopToken) Initialize(headerMap, payloadMap map[string]string, pubKey crypto.PublicKey) error {
	popToken.HeaderClaims = make(map[string]string)
	popToken.PayloadClaims = make(map[string]string)
	// Copy header
	for key, value := range headerMap {
		popToken.HeaderClaims[key] = value
	}
	// Sets header typ
	popToken.HeaderClaims["typ"] = "dpop+jwt"
	// Copy payload (only once - the duplicate copy below was harmless but
	// confusing).
	for key, value := range payloadMap {
		popToken.PayloadClaims[key] = value
	}
	// Sets header alg
	switch pubKey.(type) {
	case *rsa.PublicKey:
		popToken.HeaderClaims["alg"] = "RS256"
	case *ecdsa.PublicKey:
		popToken.HeaderClaims["alg"] = "ES256"
	default:
		return fmt.Errorf("PopToken.Initialize: unsupported public key type: %T", pubKey)
	}
	// Initializes jwk var + sets header jwk
	if err := popToken.Jwk.Initialize(pubKey, "sign"); err != nil {
		return err
	}
	popToken.HeaderClaims["jwk"] = popToken.Jwk.Marshal()
	return nil
}

// Generates popToken using a PrivateKey, can be used even if popToken is not initialized (claims are auto-fulfilled)
func (popToken *PopToken) GenerateToken(privKey crypto.PrivateKey) (token string, err error) {
	// Initialization if is not
	if popToken.HeaderClaims == nil {
		if rsaPriv, ok := privKey.(*rsa.PrivateKey); ok {
			if err = popToken.Initialize(nil, nil, &rsaPriv.PublicKey); err != nil {
				return
			}
		} else if ecdsaPriv, ok := privKey.(*ecdsa.PrivateKey); ok {
			if err = popToken.Initialize(nil, nil, &ecdsaPriv.PublicKey); err != nil {
				return
			}
		} else {
			err = errors.New("error: invalid key for signature, type not compatible")
			return
		}
	}
	// New payload claims: iat + jti
	iat := int((time.Now().Unix()))
	popToken.PayloadClaims["iat"] = strconv.Itoa(iat)
	//No need to use exp, servers will check iat + jti to check the validity
	//popToken.PayloadClaims["exp"] = strconv.Itoa(iat + 30)
	unparsedId, err := uuid.NewRandom()
	if err != nil { // Better way to generate uuid than calling an ext program
		return
	}
	popToken.PayloadClaims["jti"] = unparsedId.String()
	// Only set the default aud if the caller did not already provide one
	// (the previous code overwrote any caller-set aud).
	if _, present := popToken.PayloadClaims["aud"]; !present {
		popToken.PayloadClaims["aud"] = "vissv2/agts"
	}
	// Marshal header (must be in order)
	iterator := []string{"typ", "alg", "jwk"}
	for _, iter := range iterator {
		popToken.Jwt.AddHeader(iter, popToken.HeaderClaims[iter])
		delete(popToken.HeaderClaims, iter) // Delete so it does not repeat
	}
	for key, value := range popToken.HeaderClaims {
		popToken.Jwt.AddHeader(key, value)
	}
	// Mashal payload
	for key, value := range popToken.PayloadClaims {
		popToken.Jwt.AddClaim(key, value)
	}
	// Sign the token
	if err = popToken.Jwt.AssymSign(privKey); err != nil {
		return
	}
	return popToken.Jwt.GetFullToken(), nil
}

// Obtains Rsa public key included in the PoP token. Returns nil + error if fails
func (popToken PopToken) GetPubRsa() (*rsa.PublicKey, error) {
	pubKey := new(rsa.PublicKey)
	// Decode n and e
	byteN, err := base64.RawURLEncoding.DecodeString(popToken.Jwk.PubMod)
	if err != nil {
		return nil, err
	}
	byteE, err := base64.RawURLEncoding.DecodeString(popToken.Jwk.PubExp)
	if err != nil {
		return nil, err
	}
	// Converts n and e to big int and int
	e := new(big.Int)
	e.SetBytes(byteE)
	pubKey.N = new(big.Int)
	pubKey.N.SetBytes(byteN)
	pubKey.E = int(e.Int64())
	return pubKey, nil
}

// Obtains ECDSA public ket in the PoP token. Returns nil + error if fails
func (popToken PopToken) GetPubEcdsa() (*ecdsa.PublicKey, error) {
	pubKey := new(ecdsa.PublicKey)
	// Curve. Only P-256 is supported at the moment
	switch popToken.Jwk.Curve {
	case "P-256":
		pubKey.Curve = elliptic.P256()
	default:
		return nil, errors.New("Curve " + popToken.Jwk.Curve + " not supported")
	}
	byteXCoord, err := base64.RawURLEncoding.DecodeString(popToken.Jwk.Xcoord)
	if err != nil {
		return nil, err
	}
	byteYCoord, err := base64.RawURLEncoding.DecodeString(popToken.Jwk.Ycoord)
	if err != nil {
		return nil, err
	}
	pubKey.X = new(big.Int)
	pubKey.X.SetBytes(byteXCoord)
	pubKey.Y = new(big.Int)
	pubKey.Y.SetBytes(byteYCoord)

	return pubKey, nil
}

// Validates keys: same alg, same thumprint...
func (popToken PopToken) CheckThumb(thumprint string) (bool, string) {
	if thumprint == "" || thumprint != popToken.Jwk.Thumb {
		return false, "Invalid Thumbprint: " + popToken.Jwk.Thumb
	}
	return true, "ok"
}

func (popToken *PopToken) CheckAud(aud string) (bool, string) {
	if valid := popToken.PayloadClaims["aud"] == aud; !valid {
		return false, "Aud not valid"
	}
	return true, ""
}

// Checks signature, checks that alg used to sign is the same as in key (to avoid exploits)
func (popToken *PopToken) CheckSignature() error {
	switch popToken.HeaderClaims["alg"] {
	case "RS256":
		rsaPubKey, err := popToken.GetPubRsa()
		if err != nil {
			return err
		}
		return popToken.Jwt.CheckAssymSignature(rsaPubKey)
	case "ES256":
		ecdsaPubKey, err := popToken.GetPubEcdsa()
		if err != nil {
			return err
		}
		return popToken.Jwt.CheckAssymSignature(ecdsaPubKey)
	default:
		return errors.New("Invalid signing algorithm: " + popToken.HeaderClaims["alg"])
	}
}

// Check exp time
func (popToken PopToken) CheckExp() (bool, string) {
	expStr, ok := popToken.PayloadClaims["exp"]
	if !ok || expStr == "" {
		return false, "No exp claim"
	}
	exp, err := strconv.Atoi(expStr)
	if err != nil {
		return false, fmt.Sprintf("Bad exp claim: %q", expStr)
	}
	act := int(time.Now().Unix())
	if act > exp {
		return false, "Expired"
	}
	return true, "OK"
}

// Check iats. Gap is the possible error between clocks. lifetime is the
// maximum time after creation that the token can be used.
func (popToken PopToken) CheckIat(gap int, lifetime int) (bool, string) {
	act := int(time.Now().Unix())
	iatStr, ok := popToken.PayloadClaims["iat"]
	if !ok || iatStr == "" {
		return false, "Missing iat claim"
	}
	iat, err := strconv.Atoi(iatStr)
	if err != nil {
		return false, fmt.Sprintf("Bad iat claim: %q", iatStr)
	}
	if !(act < iat+gap+lifetime) {
		return false, fmt.Sprintf("Expired, act time: %d", act)
	}
	if !(act > iat-gap) { // Check if token is still valid
		return false, fmt.Sprintf("Created in future time, act time: %d", act)
	}
	return true, "OK"
}

// Returns a bool that tells if the pop token is valid.
func (popToken *PopToken) Validate(thumbprint, aud string, gap, lifetime int) (valid bool, info string) {
	// Validates time
	if valid, info = popToken.CheckIat(gap, lifetime); !valid {
		return
	}
	// Makes sure to exist claim "aud"
	if valid, info = popToken.CheckAud(aud); !valid {
		return
	}
	// Checks key
	if valid, info = popToken.CheckThumb(thumbprint); !valid {
		return
	}
	// Checks signature
	if err := popToken.CheckSignature(); err != nil {
		return false, fmt.Sprintf("%v", err)
	}
	return true, "OK"
}
