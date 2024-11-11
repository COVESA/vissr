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

// Gets Json string (or nothing) and adds received key and value, if it doesnt receive a value or key, it does nothing
func JsonRecursiveMarshall(key string, value string, jplain *string) {
	if key == "" || value == "" {
		return
	}
	if !strings.HasPrefix(value, "{") { // If the value of the claim starts with "{", that means the claim has another json inside, wich must not be included between commas
		value = `"` + value + `"`
	}
	if *jplain == "" {
		*jplain = `{"` + key + `":` + value + `}`
	} else {
		*jplain = (*jplain)[:len(*jplain)-1] + `,"` + key + `":` + value + `}`
	}
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
		signature = rSign.Bytes() // APPENDS r,s in big endian
		signature = append(signature, sSign.Bytes()...)
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

// Checks if the token is signed correctly. In case of symm sign, key as string must be passed. In case of assym, a crypto.PublicKey must be passed
func (token JsonWebToken) CheckSignature(key interface{}) error {
	if strings.Contains(token.Header, `HS256`) {
		strKey, ok := key.(string)
		if ok && base64.RawURLEncoding.EncodeToString([]byte(GenerateHmac(token.EncodedHeader+"."+token.EncodedPayload, strKey))) == token.EncodedSignature {
			return nil
		} else {
			return errors.New("invalid hs256 signature")
		}
	} else {
		return token.CheckAssymSignature(key)
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
		var r, s *big.Int
		r = new(big.Int)
		s = new(big.Int)
		r.SetBytes(signature[:32])
		s.SetBytes(signature[32:])
		// We have to hash the token to check it
		hashed := sha256.Sum256([]byte(token.EncodedHeader + "." + token.EncodedPayload))
		if !ecdsa.Verify(pubKey, hashed[:], r, s) {
			err = errors.New("invalid ecdsa signature")
		}
		return err
	default:
		return fmt.Errorf("public key alg not supported: %t", typ)
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

// Gets the received PoP token as string, and unmarshalls it. JWK, JWT and claims fields are all filled
func (popToken *PopToken) Unmarshal(token string) error {
	popToken.HeaderClaims = make(map[string]string)
	popToken.PayloadClaims = make(map[string]string)
	// Decodes full token into header and payload
	popToken.Jwt.DecodeFromFull(token)
	// Starting with header
	var headerMap map[string]json.RawMessage
	err := json.Unmarshal([]byte(popToken.Jwt.Header), &headerMap)
	if err != nil {
		return err
	}
	for key, value := range headerMap {
		popToken.HeaderClaims[key] = string(value[1 : len(value)-1])
	}
	popToken.HeaderClaims["jwk"] = string(headerMap["jwk"]) // Key must be unmarshalled
	// Then we decode the key
	if err := popToken.Jwk.Unmarshall(popToken.HeaderClaims["jwk"]); err != nil {
		return errors.New("can not decode key in poptoken")
	}
	// Continue with payload
	var payloadMap map[string]json.RawMessage
	err = json.Unmarshal([]byte(popToken.Jwt.Payload), &payloadMap)
	for key, value := range payloadMap {
		popToken.PayloadClaims[key] = string(value[1 : len(value)-1])
	}
	return err
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
	for key, value := range payloadMap {
		popToken.PayloadClaims[key] = value
	}
	// Sets header alg
	switch pubKey.(type) {
	case *rsa.PublicKey:
		popToken.HeaderClaims["alg"] = "RS256"
	case *ecdsa.PublicKey:
		popToken.HeaderClaims["alg"] = "ES256"
	}
	// Initializes jwk var + sets header jwk
	if err := popToken.Jwk.Initialize(pubKey, "sign"); err != nil {
		return err
	}
	popToken.HeaderClaims["jwk"] = popToken.Jwk.Marshal()
	// Copy payload
	for key, value := range payloadMap {
		popToken.PayloadClaims[key] = value
	}
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
	popToken.PayloadClaims["aud"] = "vissv2/agts"
	// popToken.PayloadClaims[""]
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
	exp, err := strconv.Atoi(popToken.PayloadClaims["exp"])
	if err != nil {
		return false, "No exp claim"
	}
	act := int(time.Now().Unix())
	if act > exp {
		return false, "Expired"
	}
	return true, "OK"
}

// Check iats. Gap is the possible error between clocks. lifetime is the maximum time after is creation that the token can be used
func (popToken PopToken) CheckIat(gap int, lifetime int) (bool, string) {
	act := int(time.Now().Unix())
	iat, err := strconv.Atoi(popToken.PayloadClaims["iat"])
	if err != nil {
		return false, "Bad iat claim"
	}
	if !(act < iat+gap+lifetime) {
		return false, fmt.Sprintf("Expired, act time: %d", act)
	}
	Info.Printf("\n\n %d ", act)
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
	//if valid, info = popToken.CheckExp(); !valid {
	//	return
	//}
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
	return valid, info
}
