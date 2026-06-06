/**
* (C) 2026 Ford Motor Company
*
* Coverage tests filling cryptoutils.go gaps:
*   - ImportRsaPubKey — happy path + missing-file error
*   - ImportEcdsaKey  — happy path + missing-file error
*   - PemDecodeRSA    — nil-PEM error branch
*   - PemDecodeRSAPub — nil-PEM error branch + wrong-type branch
*   - PemDecodeECDSA  — nil-PEM error branch
*   - ExportKeyPair   — unsupported key-type error branch
*   - ExportKeyPair   — ECDSA both files written
*   - PemEncodeRSA    — round-trip for the pubKey branch
*   - Marshal on JsonWebKey — non-empty path
**/
package utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ImportRsaPubKey — happy path via ExportKeyPair + round-trip
// ---------------------------------------------------------------------------

func TestImportRsaPubKey_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	pubFile := filepath.Join(tmp, "pub.rsa")

	var privKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &privKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	if err := ExportKeyPair(privKey, "", pubFile); err != nil {
		t.Fatalf("ExportKeyPair: %v", err)
	}

	var gotPub *rsa.PublicKey
	if err := ImportRsaPubKey(pubFile, &gotPub); err != nil {
		t.Fatalf("ImportRsaPubKey: %v", err)
	}
	if gotPub.N.Cmp(privKey.PublicKey.N) != 0 {
		t.Errorf("imported RSA public key has different modulus")
	}
}

func TestImportRsaPubKey_MissingFile(t *testing.T) {
	var key *rsa.PublicKey
	err := ImportRsaPubKey("/nonexistent/path/key.pem", &key)
	if err == nil {
		t.Errorf("expected error for missing file; got nil")
	}
}

// ---------------------------------------------------------------------------
// ImportEcdsaKey — happy path via PemEncodeECDSA + round-trip
// ---------------------------------------------------------------------------

func TestImportEcdsaKey_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	privFile := filepath.Join(tmp, "priv.ec")

	var privKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &privKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	// Write PEM to file via PemEncodeECDSA
	privPem, _, err := PemEncodeECDSA(privKey)
	if err != nil {
		t.Fatalf("PemEncodeECDSA: %v", err)
	}
	if err := os.WriteFile(privFile, []byte(privPem), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var gotKey *ecdsa.PrivateKey
	if err := ImportEcdsaKey(privFile, &gotKey); err != nil {
		t.Fatalf("ImportEcdsaKey: %v", err)
	}
	if gotKey.Curve != privKey.Curve || gotKey.X.Cmp(privKey.X) != 0 {
		t.Errorf("imported ECDSA key has different curve/X")
	}
}

func TestImportEcdsaKey_MissingFile(t *testing.T) {
	var key *ecdsa.PrivateKey
	err := ImportEcdsaKey("/nonexistent/path/key.pem", &key)
	if err == nil {
		t.Errorf("expected error for missing file; got nil")
	}
}

// ---------------------------------------------------------------------------
// PemDecodeRSA — nil-PEM branch (not-PEM input)
// ---------------------------------------------------------------------------

func TestPemDecodeRSA_NilPEM(t *testing.T) {
	var key *rsa.PrivateKey
	err := PemDecodeRSA("not a pem block at all", &key)
	if err == nil {
		t.Errorf("PemDecodeRSA should error on non-PEM input; got nil")
	}
	if !strings.Contains(err.Error(), "pem format") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestPemDecodeRSA_WrongPEMType(t *testing.T) {
	// A valid PEM block but with the wrong type header (EC PRIVATE KEY vs RSA PRIVATE KEY)
	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	ecPem, _, err := PemEncodeECDSA(ecKey)
	if err != nil {
		t.Fatalf("PemEncodeECDSA: %v", err)
	}
	var rsaKey *rsa.PrivateKey
	err = PemDecodeRSA(ecPem, &rsaKey)
	if err == nil {
		t.Errorf("PemDecodeRSA should reject EC PRIVATE KEY block; got nil error")
	}
}

// ---------------------------------------------------------------------------
// PemDecodeRSAPub — nil-PEM branch + wrong-type branch
// ---------------------------------------------------------------------------

func TestPemDecodeRSAPub_NilPEM(t *testing.T) {
	var key *rsa.PublicKey
	err := PemDecodeRSAPub("garbage text not pem", &key)
	if err == nil {
		t.Errorf("PemDecodeRSAPub should error on non-PEM input; got nil")
	}
}

func TestPemDecodeRSAPub_WrongPEMType(t *testing.T) {
	// An RSA private key PEM block has the wrong type for PemDecodeRSAPub
	var privKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &privKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	privPem, _, err := PemEncodeRSA(privKey)
	if err != nil {
		t.Fatalf("PemEncodeRSA: %v", err)
	}
	var pubKey *rsa.PublicKey
	err = PemDecodeRSAPub(privPem, &pubKey)
	if err == nil {
		t.Errorf("PemDecodeRSAPub should reject RSA PRIVATE KEY block; got nil error")
	}
}

// ---------------------------------------------------------------------------
// PemDecodeECDSA — nil-PEM branch
// ---------------------------------------------------------------------------

func TestPemDecodeECDSA_NilPEM(t *testing.T) {
	var key *ecdsa.PrivateKey
	err := PemDecodeECDSA("totally not pem", &key)
	if err == nil {
		t.Errorf("PemDecodeECDSA should error on non-PEM input; got nil")
	}
}

// ---------------------------------------------------------------------------
// ExportKeyPair — unsupported key type error branch
// ---------------------------------------------------------------------------

type unsupportedKey struct{}

func TestExportKeyPair_UnsupportedKeyType(t *testing.T) {
	tmp := t.TempDir()
	err := ExportKeyPair(unsupportedKey{}, filepath.Join(tmp, "key"), "")
	if err == nil {
		t.Errorf("ExportKeyPair should error on unsupported key type; got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ExportKeyPair — ECDSA with both private and public files
// ---------------------------------------------------------------------------

func TestExportKeyPair_ECDSA_BothFiles(t *testing.T) {
	tmp := t.TempDir()
	privFile := filepath.Join(tmp, "priv.ec")
	pubFile := filepath.Join(tmp, "pub.ec")

	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	if err := ExportKeyPair(ecKey, privFile, pubFile); err != nil {
		t.Fatalf("ExportKeyPair(ECDSA both files): %v", err)
	}
	// Both files should exist
	if _, err := os.Stat(privFile); err != nil {
		t.Errorf("private file not created: %v", err)
	}
	if _, err := os.Stat(pubFile); err != nil {
		t.Errorf("public file not created: %v", err)
	}
	// Private file mode should be 0600
	info, err := os.Stat(privFile)
	if err != nil {
		t.Fatalf("stat priv: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("ECDSA private key mode = %#o; want 0600", mode)
	}
}

// ---------------------------------------------------------------------------
// JsonWebKey.Marshal — non-empty result
// ---------------------------------------------------------------------------

func TestJsonWebKeyMarshal_NonEmpty(t *testing.T) {
	var jwk JsonWebKey
	if err := jwk.Unmarshall(`{"kty":"RSA","n":"abc","e":"AQAB"}`); err != nil {
		t.Fatalf("Unmarshall: %v", err)
	}
	got := jwk.Marshal()
	if got == "" {
		t.Errorf("Marshal returned empty string for valid JWK")
	}
	if !strings.Contains(got, "RSA") {
		t.Errorf("Marshal result missing kty: %q", got)
	}
}

// ---------------------------------------------------------------------------
// PemEncodeRSA — verify both priv and pub PEM are non-empty
// ---------------------------------------------------------------------------

func TestPemEncodeRSA_BothOutputsNonEmpty(t *testing.T) {
	var privKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &privKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	privPem, pubPem, err := PemEncodeRSA(privKey)
	if err != nil {
		t.Fatalf("PemEncodeRSA: %v", err)
	}
	if !strings.Contains(privPem, "RSA PRIVATE KEY") {
		t.Errorf("privPem missing RSA PRIVATE KEY header")
	}
	if !strings.Contains(pubPem, "RSA PUBLIC KEY") {
		t.Errorf("pubPem missing RSA PUBLIC KEY header")
	}
}
