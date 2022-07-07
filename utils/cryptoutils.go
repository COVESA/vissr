/************
*	File implementing multiple cryptographic support for the implementation
*
*	Author: Jose Jesus Sanchez Gomez (sanchezg@lcc.uma.es)
*	2021, NICS Lab (University of Malaga)
*
*************/

package utils

import (
	"bufio"
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
// Gets rsa key in pem format and decodes it into rsa.privatekey
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
	// Gets private key from parsed key
	*privKey = parsedKey.(*rsa.PrivateKey)
	return nil
}

// Gets rsa pub key in pem format and decodes it into rsa.publickey
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
	*pubKey = parsedKey.(*rsa.PublicKey)
	return nil
}

// Gets ECDSA key in pem format and decodes it into ecdsa.PrivateKey
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
	*privKey = parsedKey.(*ecdsa.PrivateKey)
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

// Returns ECDSA Keys as string in PEM format
func PemEncodeECDSA(privKey *ecdsa.PrivateKey) (strPrivKey string, strPubKey string, err error) {
	byteKey, _ := x509.MarshalECPrivateKey(privKey)
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
	byteKey, _ = x509.MarshalPKIXPublicKey(&privKey.PublicKey)
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
// Gets rsa private key from pem file
func ImportRsaKey(filename string, privKey **rsa.PrivateKey) error {
	privFile, err := os.Open(filename)
	if err != nil {
		return err
	}
	prvFileInfo, err := privFile.Stat() // Gets info of io
	if err != nil {
		return err
	}
	prvBytes := make([]byte, prvFileInfo.Size())
	prvBuffer := bufio.NewReader(privFile)
	_, err = prvBuffer.Read(prvBytes)
	if err != nil {
		return err
	}
	err = PemDecodeRSA(string(prvBytes), privKey)
	return err
}

// Gets rsa public ket from pem file
func ImportRsaPubKey(filename string, pubKey **rsa.PublicKey) error {
	pubFile, err := os.Open(filename)
	if err != nil {
		return err
	}
	pubFileInfo, err := pubFile.Stat()
	if err != nil {
		return err
	}
	pubBytes := make([]byte, pubFileInfo.Size())
	pubBuffer := bufio.NewReader(pubFile)
	_, err = pubBuffer.Read(pubBytes)
	if err != nil {
		return err
	}
	err = PemDecodeRSAPub(string(pubBytes), pubKey)
	return err
}

// Gets ecdsa private key from pem file
func ImportEcdsaKey(filename string, privKey **ecdsa.PrivateKey) error {
	privFile, err := os.Open(filename)
	if err != nil {
		return err
	}
	prvFileInfo, err := privFile.Stat() // Gets info of io
	if err != nil {
		return err
	}
	prvBytes := make([]byte, prvFileInfo.Size())
	prvBuffer := bufio.NewReader(privFile)
	_, err = prvBuffer.Read(prvBytes)
	if err != nil {
		return err
	}
	err = PemDecodeECDSA(string(prvBytes), privKey)
	return err
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
			privFile, err := os.Create(privFileName) //".rsa"
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
			pubFile, err := os.Create(pubFileName) // + ".rsa.pub"
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
			ecdsaByt, _ := x509.MarshalECPrivateKey(ecdsaPriv)
			privBlock := pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: ecdsaByt,
			}
			privFile, err := os.Create(privFileName) //+ ".ec"
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
			ecdsaByt2, _ := x509.MarshalPKIXPublicKey(&ecdsaPriv.PublicKey)
			pubBlock := pem.Block{
				Type:  "EC PUBLIC KEY",
				Bytes: ecdsaByt2,
			}
			pubFile, err := os.Create(pubFileName) // + ".ec.pub"
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
func (jkey *JsonWebKey) Unmarshall(rcv string) error {
	err := json.Unmarshal([]byte(rcv), jkey)
	if err != nil {
		return err
	}
	jkey.Thumb = jkey.GenThumbprint()
	return err
}

// From JsonWebKey struct, returns marshalled text
func (jkey *JsonWebKey) Marshal() string {
	marsh, err := json.Marshal(jkey)
	if err != nil {
		return ""
	}
	return string(marsh[:])
}
