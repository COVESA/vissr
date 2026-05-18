/************
*	File implementing multiple cryptographic support for the implementation
*
*	Author: Jose Jesus Sanchez Gomez (sanchezg@lcc.uma.es)
*	2021, NICS Lab (University of Malaga)
*
*************/

package utils

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
)

// *********				KEY GENERATION 									***********
// Generates RSA private key of given size
func GenRsaKey(size int, privKey **rsa.PrivateKey) error {
	if size%8 != 0 || size < 2048 {
		size = 2048
	}
	auxKey, err := rsa.GenerateKey(rand.Reader, size)
	*privKey = auxKey
	if err != nil {
		return err
	}
	return nil
}

// Generates ECDSA private Key using given curve
func GenEcdsaKey(curve elliptic.Curve, privKey **ecdsa.PrivateKey) error {
	auxKey, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		return err
	}
	*privKey = auxKey
	return nil
}

// *********				KEY ENCODING / DECODING								***********
// Gets rsa key in pem format and decodes it into rsa.privatekey.
//
// Bug-3 fix: the previous unchecked type assertion
// `parsedKey.(*rsa.PrivateKey)` panicked if a PKCS8-formatted file
// happened to contain an ECDSA / Ed25519 key. Now returns an error
// in that case.
func PemDecodeRSA(pemKey string, privKey **rsa.PrivateKey) error {
	pemBlock, _ := pem.Decode([]byte(pemKey)) // Gets pem_block from raw key
	// Checking key type and correct decodification
	if pemBlock == nil {
		return errors.New("private key not found or is not in pem format")
	}
	if pemBlock.Type != "RSA PRIVATE KEY" {
		return fmt.Errorf("invalid private key, wrong type: %T", pemBlock.Type)
	}
	// Parses obtained pem block
	var parsedKey interface{} //Still dont know what key type we need to parse
	parsedKey, err := x509.ParsePKCS1PrivateKey(pemBlock.Bytes)
	if err != nil {
		parsedKey, err = x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
		if err != nil {
			return err //errors.New("Unable to parse RSA private key")
		}
	}
	rsaKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return fmt.Errorf("parsed PKCS8 private key is not RSA: %T", parsedKey)
	}
	*privKey = rsaKey
	return nil
}

// Gets rsa pub key in pem format and decodes it into rsa.publickey.
// See PemDecodeRSA for the bug-3 fix.
func PemDecodeRSAPub(pemKey string, pubKey **rsa.PublicKey) error {
	pemBlock, _ := pem.Decode([]byte(pemKey))
	if pemBlock == nil {
		return errors.New("public Key not found or is not in pem format")
	}
	if (pemBlock.Type != "RSA PUBLIC KEY") && (pemBlock.Type != "PUBLIC KEY") {
		return fmt.Errorf("invalid public key, wrong type: %s", pemBlock.Type)
	}
	var parsedKey interface{}
	parsedKey, err := x509.ParsePKCS1PublicKey(pemBlock.Bytes)
	if err != nil {
		parsedKey, err = x509.ParsePKIXPublicKey(pemBlock.Bytes)
		if err != nil {
			return err
		}
	}
	rsaKey, ok := parsedKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("parsed PKIX public key is not RSA: %T", parsedKey)
	}
	*pubKey = rsaKey
	return nil
}

// Gets ECDSA key in pem format and decodes it into ecdsa.PrivateKey.
// See PemDecodeRSA for the bug-3 fix.
func PemDecodeECDSA(pemKey string, privKey **ecdsa.PrivateKey) error {
	pemBlock, _ := pem.Decode([]byte(pemKey))
	if pemBlock == nil {
		return errors.New("private key not found or is not in pem format")
	}
	if pemBlock.Type != "EC PRIVATE KEY" {
		return fmt.Errorf("invalid private key, wrong type: %T", pemBlock.Type)
	}
	var parsedKey interface{}
	parsedKey, err := x509.ParseECPrivateKey(pemBlock.Bytes)
	if err != nil {
		parsedKey, err = x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
		if err != nil {
			return err
		}
	}
	ecdsaKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return fmt.Errorf("parsed PKCS8 private key is not ECDSA: %T", parsedKey)
	}
	*privKey = ecdsaKey
	return nil
}

// Returns RSA Keys as string in PEM format
func PemEncodeRSA(privKey *rsa.PrivateKey) (strPrivKey string, strPubKey string, err error) {
	// Creates pem block from given key
	privBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}
	// Encodes pem block to byte buffer, then gets the string from it
	privBuf := new(bytes.Buffer)
	err = pem.Encode(privBuf, &privBlock)
	if err != nil {
		return
	}
	strPrivKey = privBuf.String()

	// Same with public key
	pubBlock := pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&privKey.PublicKey),
	}
	pubBuf := new(bytes.Buffer)
	err = pem.Encode(pubBuf, &pubBlock)
	if err != nil {
		return
	}
	strPubKey = pubBuf.String()
	return
}

// Returns ECDSA Keys as string in PEM format.
//
// Bug-14 fix: the previous code discarded the errors from
// x509.MarshalECPrivateKey and x509.MarshalPKIXPublicKey, then
// proceeded to write a PEM block with `Bytes: nil` — producing an
// empty PEM that silently "succeeded". Now both errors are
// propagated.
func PemEncodeECDSA(privKey *ecdsa.PrivateKey) (strPrivKey string, strPubKey string, err error) {
	byteKey, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal EC private key: %w", err)
	}
	privBlock := pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: byteKey,
	}
	buf := bytes.NewBuffer(nil)
	if err = pem.Encode(buf, &privBlock); err != nil {
		return
	}
	strPrivKey = buf.String()
	buf.Reset()
	byteKey, err = x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal PKIX public key: %w", err)
	}
	pubBlock := pem.Block{
		Type:  "EC PUBLIC KEY",
		Bytes: byteKey,
	}
	if err = pem.Encode(buf, &pubBlock); err != nil {
		return
	}
	strPubKey = buf.String()
	return
}

// *********				PEM KEY IMPORT / EXPORT									***********
//
// Bug-7 fix: the previous implementations used
// `bufio.NewReader(file).Read(buf)` which performs a SINGLE Read
// call and discards the byte count. For PEM files larger than the
// bufio default buffer (4 KiB) this produced a short read and a
// truncated PEM that either failed to decode or, worse, decoded
// partial data. Replaced with os.ReadFile.

// Gets rsa private key from pem file
func ImportRsaKey(filename string, privKey **rsa.PrivateKey) error {
	prvBytes, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return PemDecodeRSA(string(prvBytes), privKey)
}

// Gets rsa public key from pem file
func ImportRsaPubKey(filename string, pubKey **rsa.PublicKey) error {
	pubBytes, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return PemDecodeRSAPub(string(pubBytes), pubKey)
}

// Gets ecdsa private key from pem file
func ImportEcdsaKey(filename string, privKey **ecdsa.PrivateKey) error {
	prvBytes, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return PemDecodeECDSA(string(prvBytes), privKey)
}

// createPrivateKeyFile opens (or replaces) a file with mode 0600 —
// read/write for owner only. The previous code used os.Create which
// honours the process umask; on typical systems this produced 0644
// (world-readable), letting any local user exfiltrate the RSA / ECDSA
// signing key. Public-key files keep the default mode since they're
// not sensitive.
func createPrivateKeyFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
}

// Export KeyPair to files named as given (ECDSA and RSA supported, pointers to privKey must be given)
func ExportKeyPair(privKey crypto.PrivateKey, privFileName string, pubFileName string) error {
	switch typ := privKey.(type) {
	case *rsa.PrivateKey:
		rsaPriv, _ := privKey.(*rsa.PrivateKey)
		if privFileName != "" {
			privBlock := pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(rsaPriv),
			}
			privFile, err := createPrivateKeyFile(privFileName) // 0600 (bug-8)
			if err != nil {
				return err
			}
			defer privFile.Close()
			err = pem.Encode(privFile, &privBlock)
			if err != nil {
				return err
			}
		}
		if pubFileName != "" {
			pubBlock := pem.Block{
				Type:  "RSA PUBLIC KEY",
				Bytes: x509.MarshalPKCS1PublicKey(&rsaPriv.PublicKey),
			}
			pubFile, err := os.Create(pubFileName) // public key — default mode is fine
			if err != nil {
				return err
			}
			defer pubFile.Close()
			err = pem.Encode(pubFile, &pubBlock)
			if err != nil {
				return err
			}
		}

	case *ecdsa.PrivateKey:
		ecdsaPriv, _ := privKey.(*ecdsa.PrivateKey)
		if privFileName != "" {
			ecdsaByt, marshalErr := x509.MarshalECPrivateKey(ecdsaPriv)
			if marshalErr != nil {
				return fmt.Errorf("marshal EC private key: %w", marshalErr)
			}
			privBlock := pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: ecdsaByt,
			}
			privFile, err := createPrivateKeyFile(privFileName) // 0600 (bug-8)
			if err != nil {
				return err
			}
			defer privFile.Close()
			err = pem.Encode(privFile, &privBlock)
			if err != nil {
				return err
			}
		}
		if pubFileName != "" {
			ecdsaByt2, marshalErr := x509.MarshalPKIXPublicKey(&ecdsaPriv.PublicKey)
			if marshalErr != nil {
				return fmt.Errorf("marshal PKIX public key: %w", marshalErr)
			}
			pubBlock := pem.Block{
				Type:  "EC PUBLIC KEY",
				Bytes: ecdsaByt2,
			}
			pubFile, err := os.Create(pubFileName) // public key — default mode is fine
			if err != nil {
				return err
			}
			defer pubFile.Close()
			err = pem.Encode(pubFile, &pubBlock)
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("key type not supported: %T", typ)
	}
	return nil
}

// *********				JSON WEB KEY ENCODING								***********
// *********	Contained in PoP, follows RFC7517 standard. Support for RSA and ECDSA keys
type JsonWebKey struct {
	Thumb  string `json:"-"`
	Type   string `json:"kty"`
	Use    string `json:"use,omitempty"`
	PubMod string `json:"n,omitempty"`   // RSA
	PubExp string `json:"e,omitempty"`   // RSA
	Curve  string `json:"crv,omitempty"` //ECDSA
	Xcoord string `json:"x,omitempty"`   //ECDSA
	Ycoord string `json:"y,omitempty"`   //ECDSA
}

// Initializes json web key from public key
func (jkey *JsonWebKey) Initialize(pubKey crypto.PublicKey, use string) error {
	//jkey.Use = "sig"
	jkey.Use = use
	switch typ := pubKey.(type) {
	case *rsa.PublicKey:
		jkey.Type = "RSA"
		rsaPubKey := pubKey.(*rsa.PublicKey)
		jkey.PubExp = base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaPubKey.E)).Bytes()) // To get it as bytes, we first convert to big int, which has a method Bytes()
		jkey.PubMod = base64.RawURLEncoding.EncodeToString(rsaPubKey.N.Bytes())
	case *ecdsa.PublicKey:
		jkey.Type = "EC"
		ecdsaPubKey := pubKey.(*ecdsa.PublicKey)
		jkey.Curve = fmt.Sprintf("P-%v", ecdsaPubKey.Curve.Params().BitSize)
		jkey.Xcoord = base64.RawURLEncoding.EncodeToString(ecdsaPubKey.X.Bytes())
		jkey.Ycoord = base64.RawURLEncoding.EncodeToString(ecdsaPubKey.Y.Bytes())
	default:
		return fmt.Errorf("error: can not initialize jwk with pubkey of type: %T", typ)
	}
	jkey.Thumb = jkey.GenThumbprint()
	return nil
}

// Generates thumbprint of the JWK
func (jkey *JsonWebKey) GenThumbprint() string {
	var thumbprint string
	switch jkey.Type {
	case "RSA":
		JsonRecursiveMarshall("e", jkey.PubExp, &thumbprint)
		JsonRecursiveMarshall("kty", jkey.Type, &thumbprint)
		JsonRecursiveMarshall("n", jkey.PubMod, &thumbprint)
	case "EC":
		JsonRecursiveMarshall("crv", jkey.Curve, &thumbprint)
		JsonRecursiveMarshall("kty", jkey.Type, &thumbprint)
		JsonRecursiveMarshall("x", jkey.Xcoord, &thumbprint)
		JsonRecursiveMarshall("y", jkey.Ycoord, &thumbprint)
	}
	// For the thumbprint, now SHA-256, then encode into Base-64
	sha256Hash := sha256.Sum256([]byte(thumbprint))
	return base64.RawURLEncoding.EncodeToString(sha256Hash[:])
	// Strings in go are UTF-8, so we could get thumbprint (RFC7638) Using MD-5 hash (), RFC7638 recommends SHA256
	//md5hash := md5.Sum([]byte(jkey.Thumb))
	//jkey.Thumb = string(base64.RawURLEncoding.EncodeToString(md5hash[:]))
}

//	Gets the received JWK and unmarshalls it, returns error if fails to unmarshall
// Unmarshall decodes a JsonWebKey from its JSON representation. The
// returned thumbprint is freshly computed from the key fields.
//
// Bug-13 fix: validate that the key-type-required fields are present
// before computing a thumbprint. The previous code accepted a JWK
// with only `"kty"` set and no `n`/`e` or `crv`/`x`/`y`, then
// computed a thumbprint over empty strings. Two such malformed JWKs
// collided on the same thumbprint, breaking any downstream identity
// check that compared thumbs.
func (jkey *JsonWebKey) Unmarshall(rcv string) error {
	err := json.Unmarshal([]byte(rcv), jkey)
	if err != nil {
		return err
	}
	switch jkey.Type {
	case "RSA":
		if jkey.PubMod == "" || jkey.PubExp == "" {
			return errors.New("invalid RSA JWK: missing n or e")
		}
	case "EC":
		if jkey.Curve == "" || jkey.Xcoord == "" || jkey.Ycoord == "" {
			return errors.New("invalid EC JWK: missing crv, x, or y")
		}
	case "":
		return errors.New("invalid JWK: missing kty")
	default:
		return fmt.Errorf("unsupported JWK kty: %q", jkey.Type)
	}
	jkey.Thumb = jkey.GenThumbprint()
	return nil
}

// From JsonWebKey struct, returns marshalled text
func (jkey *JsonWebKey) Marshal() string {
	marsh, err := json.Marshal(jkey)
	if err != nil {
		return ""
	}
	return string(marsh[:])
}
