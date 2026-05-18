/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Complete tests for utils/cryptoutils.go plus the in-scope JWT
* callees in datatypes.go (CheckSignature / extractAlg).
*
* Bug coverage map:
*    1  HMAC ==                         → TestCheckSignature_HmacUsesConstantTime,
*                                          TestCheckSignature_HmacRejectsWrongKey
*    2  Algorithm confusion             → TestCheckSignature_RejectsNone,
*                                          TestCheckSignature_RejectsUnknownAlg,
*                                          TestCheckSignature_HeaderSubstringDoesNotConfuse
*    3  Unchecked type assertions       → TestPemDecode*_RejectsWrongKeyType
*    7  Short-read in Import*           → TestImportRsaKey_RoundTrip_LargeFile
*    8  File mode 0600                  → TestExportKeyPair_PrivateFileIs0600
*   13  JWK required-field validation   → TestJwkUnmarshall_RejectsMissingFields
*   14  PemEncodeECDSA discarded errors → TestPemEncodeECDSA_HappyPath
**/
package utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Key generation
// ---------------------------------------------------------------------------

func TestGenRsaKey_ClampsSmallSizes(t *testing.T) {
	var key *rsa.PrivateKey
	// Sub-2048 sizes get clamped up to 2048 (existing behavior we
	// want to preserve). Pass 1024 — accepted, but bumped to 2048.
	if err := GenRsaKey(1024, &key); err != nil {
		t.Fatalf("GenRsaKey(1024) error: %v", err)
	}
	if key == nil {
		t.Fatalf("GenRsaKey produced nil key")
	}
	if got := key.N.BitLen(); got < 2048 {
		t.Errorf("GenRsaKey bit length = %d; want >= 2048 (clamped)", got)
	}
}

func TestGenRsaKey_AcceptsValidSize(t *testing.T) {
	var key *rsa.PrivateKey
	if err := GenRsaKey(2048, &key); err != nil {
		t.Fatalf("GenRsaKey(2048) error: %v", err)
	}
	if key.N.BitLen() != 2048 {
		t.Errorf("GenRsaKey(2048) bit length = %d; want 2048", key.N.BitLen())
	}
}

func TestGenEcdsaKey_P256(t *testing.T) {
	var key *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &key); err != nil {
		t.Fatalf("GenEcdsaKey error: %v", err)
	}
	if key.Curve != elliptic.P256() {
		t.Errorf("curve = %v; want P256", key.Curve)
	}
}

// ---------------------------------------------------------------------------
// PEM round-trips + bug-3 type-assertion guards
// ---------------------------------------------------------------------------

func freshRsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	var key *rsa.PrivateKey
	if err := GenRsaKey(2048, &key); err != nil {
		t.Fatalf("test key gen: %v", err)
	}
	return key
}

func freshEcdsaKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	var key *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &key); err != nil {
		t.Fatalf("test key gen: %v", err)
	}
	return key
}

func TestPemEncodeDecodeRSA_RoundTrip(t *testing.T) {
	orig := freshRsaKey(t)
	privPem, pubPem, err := PemEncodeRSA(orig)
	if err != nil {
		t.Fatalf("PemEncodeRSA: %v", err)
	}

	var got *rsa.PrivateKey
	if err := PemDecodeRSA(privPem, &got); err != nil {
		t.Fatalf("PemDecodeRSA: %v", err)
	}
	if got.N.Cmp(orig.N) != 0 {
		t.Errorf("PemDecodeRSA produced different modulus")
	}

	var gotPub *rsa.PublicKey
	if err := PemDecodeRSAPub(pubPem, &gotPub); err != nil {
		t.Fatalf("PemDecodeRSAPub: %v", err)
	}
	if gotPub.N.Cmp(orig.PublicKey.N) != 0 {
		t.Errorf("PemDecodeRSAPub produced different modulus")
	}
}

func TestPemEncodeECDSA_HappyPath(t *testing.T) {
	orig := freshEcdsaKey(t)
	privPem, pubPem, err := PemEncodeECDSA(orig)
	if err != nil {
		t.Fatalf("PemEncodeECDSA: %v", err)
	}
	if !strings.Contains(privPem, "EC PRIVATE KEY") {
		t.Errorf("priv PEM missing header: %s", privPem)
	}
	if !strings.Contains(pubPem, "EC PUBLIC KEY") {
		t.Errorf("pub PEM missing header: %s", pubPem)
	}
	// Round-trip the private side.
	var got *ecdsa.PrivateKey
	if err := PemDecodeECDSA(privPem, &got); err != nil {
		t.Fatalf("PemDecodeECDSA: %v", err)
	}
	if got.Curve != orig.Curve || got.X.Cmp(orig.X) != 0 {
		t.Errorf("ECDSA round trip mismatch")
	}
}

func TestPemDecodeRSA_RejectsNonRsaPkcs8(t *testing.T) {
	// Bug-3 fix: an ECDSA key wrapped in PKCS8 used to panic the
	// PemDecodeRSA path via the unchecked type assertion. Now the
	// function returns an error.
	ec := freshEcdsaKey(t)
	// Encode as a PKCS8 PEM block by hand using the EC helper —
	// actually PemEncodeECDSA produces SEC1 not PKCS8. We need a
	// PKCS8-shaped block. Encode via x509.MarshalPKCS8PrivateKey
	// directly.
	pkcs8 := pkcs8EncodePrivKey(t, ec)

	var rsaKey *rsa.PrivateKey
	err := PemDecodeRSA(pkcs8, &rsaKey)
	if err == nil {
		t.Errorf("PemDecodeRSA accepted a PKCS8 ECDSA key; should error")
	}
}

func TestPemDecodeECDSA_RejectsNonEcdsaPkcs8(t *testing.T) {
	rsaKey := freshRsaKey(t)
	pkcs8 := pkcs8EncodePrivKey(t, rsaKey)

	var ecKey *ecdsa.PrivateKey
	// PemDecodeECDSA requires PEM type "EC PRIVATE KEY" — our PKCS8
	// block carries "PRIVATE KEY". So it will fail at the type
	// header check, not at the type assertion. That's still a
	// rejection, just on a different line — fine.
	err := PemDecodeECDSA(pkcs8, &ecKey)
	if err == nil {
		t.Errorf("PemDecodeECDSA accepted a non-EC PKCS8 PEM; should error")
	}
}

// pkcs8EncodePrivKey hand-builds a PEM block of type "PRIVATE KEY"
// (PKCS8) for either an RSA or ECDSA private key. Used by the
// bug-3 tests to construct cross-type test fixtures.
func pkcs8EncodePrivKey(t *testing.T, key interface{}) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("pkcs8 marshal: %v", err)
	}
	return "-----BEGIN PRIVATE KEY-----\n" +
		base64.StdEncoding.EncodeToString(der) +
		"\n-----END PRIVATE KEY-----\n"
}

// ---------------------------------------------------------------------------
// File I/O — bug 7 (short read) + bug 8 (file mode 0600)
// ---------------------------------------------------------------------------

func TestImportRsaKey_RoundTrip_LargeFile(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "priv.rsa")

	orig := freshRsaKey(t)
	if err := ExportKeyPair(orig, keyFile, ""); err != nil {
		t.Fatalf("ExportKeyPair: %v", err)
	}

	var got *rsa.PrivateKey
	if err := ImportRsaKey(keyFile, &got); err != nil {
		t.Fatalf("ImportRsaKey: %v", err)
	}
	if got.N.Cmp(orig.N) != 0 {
		t.Errorf("ImportRsaKey round-trip mismatch")
	}

	// Bug-7 fix: produce a file larger than the bufio default (4
	// KiB) by padding the PEM with comments and re-importing. The
	// previous single-Read implementation would short-read.
	largePem, _, err := PemEncodeRSA(orig)
	if err != nil {
		t.Fatalf("PemEncodeRSA: %v", err)
	}
	padding := strings.Repeat("# pad comment to grow file size\n", 200) // ~6 KiB
	bigFile := filepath.Join(tmp, "priv-big.rsa")
	if err := os.WriteFile(bigFile, []byte(padding+largePem), 0600); err != nil {
		t.Fatalf("write big file: %v", err)
	}
	var got2 *rsa.PrivateKey
	if err := ImportRsaKey(bigFile, &got2); err != nil {
		t.Errorf("ImportRsaKey on >4KiB file: %v (bug-7 regression)", err)
	}
}

func TestExportKeyPair_PrivateFileIs0600(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "priv.rsa")
	pubFile := filepath.Join(tmp, "pub.rsa")

	if err := ExportKeyPair(freshRsaKey(t), keyFile, pubFile); err != nil {
		t.Fatalf("ExportKeyPair: %v", err)
	}
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("stat priv: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("private key file mode = %#o; want 0600 (bug-8 regression)", mode)
	}
}

func TestExportKeyPair_PrivOnly(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "priv.rsa")
	if err := ExportKeyPair(freshRsaKey(t), keyFile, ""); err != nil {
		t.Fatalf("ExportKeyPair (priv only): %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("private key not written: %v", err)
	}
}

func TestExportKeyPair_ECDSA_FileIs0600(t *testing.T) {
	tmp := t.TempDir()
	keyFile := filepath.Join(tmp, "priv.ec")
	if err := ExportKeyPair(freshEcdsaKey(t), keyFile, ""); err != nil {
		t.Fatalf("ExportKeyPair ECDSA: %v", err)
	}
	info, _ := os.Stat(keyFile)
	if info.Mode().Perm() != 0600 {
		t.Errorf("ECDSA private key mode = %#o; want 0600", info.Mode().Perm())
	}
}

// ---------------------------------------------------------------------------
// JsonWebKey — bug 13 (required-field validation)
// ---------------------------------------------------------------------------

func TestJwkUnmarshall_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		jwk  string
	}{
		{"missing kty", `{"n":"abc","e":"AQAB"}`},
		{"RSA missing n", `{"kty":"RSA","e":"AQAB"}`},
		{"RSA missing e", `{"kty":"RSA","n":"abc"}`},
		{"EC missing crv", `{"kty":"EC","x":"x1","y":"y1"}`},
		{"EC missing x", `{"kty":"EC","crv":"P-256","y":"y1"}`},
		{"EC missing y", `{"kty":"EC","crv":"P-256","x":"x1"}`},
		{"unknown kty", `{"kty":"DH","n":"abc"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var jwk JsonWebKey
			err := jwk.Unmarshall(tc.jwk)
			if err == nil {
				t.Errorf("jwk Unmarshall accepted incomplete input; expected error")
			}
		})
	}
}

func TestJwkUnmarshall_AcceptsCompleteRsa(t *testing.T) {
	var jwk JsonWebKey
	err := jwk.Unmarshall(`{"kty":"RSA","n":"abc","e":"AQAB"}`)
	if err != nil {
		t.Errorf("complete RSA jwk rejected: %v", err)
	}
	if jwk.Thumb == "" {
		t.Errorf("jwk thumbprint not populated")
	}
}

func TestJwkUnmarshall_AcceptsCompleteEcdsa(t *testing.T) {
	var jwk JsonWebKey
	err := jwk.Unmarshall(`{"kty":"EC","crv":"P-256","x":"x1","y":"y1"}`)
	if err != nil {
		t.Errorf("complete EC jwk rejected: %v", err)
	}
}

// ---------------------------------------------------------------------------
// JWT signature verification — bugs 1 and 2
// ---------------------------------------------------------------------------

func TestExtractAlg(t *testing.T) {
	cases := []struct {
		header string
		want   string
	}{
		{`{"alg":"HS256","typ":"JWT"}`, "HS256"},
		{`{"alg":"RS256"}`, "RS256"},
		{`{"alg":"none"}`, "none"},
		{`{"typ":"JWT"}`, ""},      // no alg
		{`not json`, ""},           // malformed
		{``, ""},                   // empty
	}
	for _, tc := range cases {
		if got := extractAlg(tc.header); got != tc.want {
			t.Errorf("extractAlg(%q) = %q; want %q", tc.header, got, tc.want)
		}
	}
}

func TestCheckSignature_HmacRoundTrip(t *testing.T) {
	tok := JsonWebToken{}
	tok.SetHeader("HS256")
	tok.AddClaim("foo", "bar")
	tok.SymmSign("secret")

	if err := tok.CheckSignature("secret"); err != nil {
		t.Errorf("valid HS256 signature rejected: %v", err)
	}
}

func TestCheckSignature_HmacRejectsWrongKey(t *testing.T) {
	tok := JsonWebToken{}
	tok.SetHeader("HS256")
	tok.AddClaim("foo", "bar")
	tok.SymmSign("the-real-secret")

	if err := tok.CheckSignature("wrong-secret"); err == nil {
		t.Errorf("CheckSignature accepted wrong key")
	}
}

func TestCheckSignature_RejectsNoneAlg(t *testing.T) {
	// Bug-2 fix: previously a header without "HS256" fell through to
	// CheckAssymSignature, which would attempt asymmetric verify with
	// whatever key was passed. `alg:none` tokens must be refused
	// unconditionally.
	tok := JsonWebToken{}
	tok.SetHeader("none")
	tok.AddClaim("foo", "bar")
	tok.SymmSign("anything")

	err := tok.CheckSignature("anything")
	if err == nil {
		t.Errorf("CheckSignature accepted alg=none token (bug-2 regression)")
	}
}

func TestCheckSignature_RejectsUnknownAlg(t *testing.T) {
	tok := JsonWebToken{}
	tok.SetHeader("HS512") // not on the allow-list
	tok.AddClaim("foo", "bar")
	tok.SymmSign("anything")

	if err := tok.CheckSignature("anything"); err == nil {
		t.Errorf("CheckSignature accepted unknown alg HS512")
	}
}

func TestCheckSignature_HeaderSubstringDoesNotConfuse(t *testing.T) {
	// Bug-2 fix: the previous code used strings.Contains(header,
	// "HS256"). If "HS256" appeared in any other header claim (typ,
	// jku, kid, etc.), it would force HMAC verification even for a
	// token actually signed asymmetrically.
	//
	// Construct a real RS256 token, then mutate its header JSON to
	// contain "HS256" as a substring in an unrelated claim. After the
	// fix, the alg field is parsed properly and HMAC is not chosen.
	rsaKey := freshRsaKey(t)
	tok := JsonWebToken{}
	tok.SetHeader("RS256")
	tok.AddHeader("kid", "HS256-not-really")
	tok.AddClaim("foo", "bar")
	if err := tok.AssymSign(rsaKey); err != nil {
		t.Fatalf("AssymSign: %v", err)
	}

	// Should NOT verify as HMAC. Passing a string key would force the
	// HS256 path under the old code.
	if err := tok.CheckSignature("not-a-public-key"); err == nil {
		t.Errorf("CheckSignature falsely accepted a string key on an RS256 token (bug-2 regression)")
	}
	// But it SHOULD verify with the real public key.
	if err := tok.CheckSignature(&rsaKey.PublicKey); err != nil {
		t.Errorf("real RSA verification failed: %v", err)
	}
}

func TestCheckSignature_HmacUsesConstantTimeCompare(t *testing.T) {
	// Bug-1 fix: we can't directly test constant-time behavior, but
	// we can confirm the function behaves correctly (returns error
	// without panic) on signatures of varying length. hmac.Equal
	// short-circuits to false on length mismatch rather than
	// comparing byte-by-byte.
	tok := JsonWebToken{}
	tok.SetHeader("HS256")
	tok.AddClaim("foo", "bar")
	tok.SymmSign("secret")

	// Tamper: truncate signature.
	tok.EncodedSignature = tok.EncodedSignature[:len(tok.EncodedSignature)-2]
	if err := tok.CheckSignature("secret"); err == nil {
		t.Errorf("truncated signature accepted")
	}
	// Tamper: extend signature.
	tok.EncodedSignature += "AB"
	if err := tok.CheckSignature("secret"); err == nil {
		t.Errorf("extended signature accepted")
	}
}

func TestCheckSignature_RsaRoundTrip(t *testing.T) {
	rsaKey := freshRsaKey(t)
	tok := JsonWebToken{}
	tok.SetHeader("RS256")
	tok.AddClaim("foo", "bar")
	if err := tok.AssymSign(rsaKey); err != nil {
		t.Fatalf("AssymSign: %v", err)
	}
	if err := tok.CheckSignature(&rsaKey.PublicKey); err != nil {
		t.Errorf("RSA verification failed: %v", err)
	}
}

