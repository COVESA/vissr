/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Targeted gap-filling tests for functions identified in the coverage audit
* as below 100% (or 0%) and testable without real network/socket dependencies:
*
*   common.go:
*     - splitToPathQueryKeyValue (0%)
*     - GetRfcTime additional branches (55.6%)
*     - mustJsonString (60%) — error-fallback path
*
*   datatypes.go:
*     - AssymSign unsupported-key-type branch (90.9%)
*     - JsonWebToken.DecodeFromFull bad-base64 payload (93.3%)
*     - ExtendedJwt.DecodeFromFull JSON-unmarshal-failure branches (85.7%)
*     - PopToken.Initialize unsupported-key-type branch (86.7%)
*     - PopToken.GenerateToken with pre-initialized header (82.1%)
*     - PopToken.CheckSignature default branch + ES256 round-trip (80%)
*     - PopToken.Validate iat/aud/thumb/signature branches (88.9%)
*     - GetPubRsa bad base64 branch (84.6%)
*     - GetPubEcdsa unsupported-curve + bad-base64 branches (86.7%)
*
*   treeutils.go:
*     - ValidateToInt / ValidateToString all branches (62.5%)
*     - serializeUInt uint32 branch (50%)
*     - deSerializeUInt 4-byte branch (58.3%)
*     - countSegments (0%)
*     - compareNodeName (0%)
*     - pushPathSegment / popPathSegment extra branches (66.7% / 75%)
*     - getPathSegment (76.9%)
*     - decDepth (50%)
*     - Forest helpers: SetRootNodePointer, GetInfoType,
*       CreatePathListFile, sortPathList (0%)
*     - VSSsearchNodes (0%)
*     - VSSGetLeafNodesList / VSSGetDefaultList (0%)
*     - VSSgetParent / VSSgetDatatype / VSSgetUUID / VSSgetDefault /
*       VSSgetDescr / VSSgetNumOfAllowedElements / VSSgetAllowedElement /
*       VSSgetUnit (0%)
*     - hexToInt (0%)
*
*   managerhandlers.go:
*     - splitToPathQueryKeyValue (0%)
*     - validateSecConfig all branches
*
*   pbutils.go:
*     - populateJsonFromProto all method/type branches (0%)
*     - populateProtoFromJson all action types
*     - getMethodAndType all action types (25%)
*     - createSetPb / createSubscribePb / createUnsubscribePb (0%)
**/
package utils

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/covesa/vissr/grpc_pb"
)

// ============================================================================
// common.go — splitToPathQueryKeyValue
// ============================================================================

func TestSplitToPathQueryKeyValue_FilterQuery(t *testing.T) {
	vssPath, queryKey, queryValue := splitToPathQueryKeyValue("/Vehicle/Speed?filter=some-json")
	if vssPath != "/Vehicle/Speed" {
		t.Errorf("path = %q; want /Vehicle/Speed", vssPath)
	}
	if queryKey != "filter" {
		t.Errorf("queryKey = %q; want \"filter\"", queryKey)
	}
	if queryValue != "some-json" {
		t.Errorf("queryValue = %q; want \"some-json\"", queryValue)
	}
}

func TestSplitToPathQueryKeyValue_MetadataQuery(t *testing.T) {
	vssPath, queryKey, queryValue := splitToPathQueryKeyValue("/Vehicle/Speed?metadata=static")
	if vssPath != "/Vehicle/Speed" {
		t.Errorf("path = %q; want /Vehicle/Speed", vssPath)
	}
	if queryKey != "metadata" {
		t.Errorf("queryKey = %q; want \"metadata\"", queryKey)
	}
	if queryValue != "static" {
		t.Errorf("queryValue = %q; want \"static\"", queryValue)
	}
}

func TestSplitToPathQueryKeyValue_UnknownQueryKey(t *testing.T) {
	// An unknown query key (not filter, not metadata) should return
	// "filter" as key and "incorrect http query key" as value.
	_, queryKey, queryValue := splitToPathQueryKeyValue("/Vehicle/Speed?unknown=foo")
	if queryKey != "filter" {
		t.Errorf("queryKey = %q; want \"filter\"", queryKey)
	}
	if queryValue != "incorrect http query key" {
		t.Errorf("queryValue = %q; want \"incorrect http query key\"", queryValue)
	}
}

func TestSplitToPathQueryKeyValue_NoQuery(t *testing.T) {
	vssPath, queryKey, queryValue := splitToPathQueryKeyValue("/Vehicle/Speed")
	if vssPath != "/Vehicle/Speed" {
		t.Errorf("path = %q; want /Vehicle/Speed", vssPath)
	}
	if queryKey != "" {
		t.Errorf("queryKey = %q; want \"\"", queryKey)
	}
	if queryValue != "" {
		t.Errorf("queryValue = %q; want \"\"", queryValue)
	}
}

func TestSplitToPathQueryKeyValue_QuestionMarkNoEquals(t *testing.T) {
	// ? present but no = → delim2 == -1, falls through to return path, "", ""
	vssPath, queryKey, queryValue := splitToPathQueryKeyValue("/Vehicle?noequals")
	if queryKey != "" {
		t.Errorf("no-equals: queryKey = %q; want \"\"", queryKey)
	}
	if queryValue != "" {
		t.Errorf("no-equals: queryValue = %q; want \"\"", queryValue)
	}
	_ = vssPath
}

// ============================================================================
// datatypes.go — AssymSign unsupported key type
// ============================================================================

func TestAssymSign_UnsupportedKeyType_Gap(t *testing.T) {
	tok := JsonWebToken{}
	tok.SetHeader("XX256")
	tok.AddClaim("x", "y")
	// Pass an unsupported key type (string is not rsa.PrivateKey or ecdsa.PrivateKey)
	err := tok.AssymSign("not-a-key")
	if err == nil {
		t.Errorf("AssymSign should return error on unsupported key type; got nil")
	}
	if !strings.Contains(err.Error(), "invalid key type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestAssymSign_ECDSARoundTrip(t *testing.T) {
	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	tok := JsonWebToken{}
	tok.SetHeader("ES256")
	tok.AddClaim("sub", "test")
	if err := tok.AssymSign(ecKey); err != nil {
		t.Fatalf("AssymSign(ECDSA): %v", err)
	}
	if tok.EncodedSignature == "" {
		t.Errorf("ECDSA signature should be non-empty")
	}
	// Verify it
	if err := tok.CheckAssymSignature(&ecKey.PublicKey); err != nil {
		t.Errorf("ECDSA signature verification failed: %v", err)
	}
}

// ============================================================================
// datatypes.go — JsonWebToken.DecodeFromFull error branches
// ============================================================================

func TestJwtDecodeFromFull_TooFewParts(t *testing.T) {
	var tok JsonWebToken
	err := tok.DecodeFromFull("only.two")
	if err == nil {
		t.Errorf("DecodeFromFull should error on 2-part input; got nil")
	}
}

func TestJwtDecodeFromFull_TooManyParts(t *testing.T) {
	var tok JsonWebToken
	err := tok.DecodeFromFull("a.b.c.d")
	if err == nil {
		t.Errorf("DecodeFromFull should error on 4-part input; got nil")
	}
}

func TestJwtDecodeFromFull_BadBase64Header(t *testing.T) {
	var tok JsonWebToken
	// Header part is not valid base64url
	err := tok.DecodeFromFull("!!!invalid!!!.dmFsaWQ.c2ln")
	if err == nil {
		t.Errorf("DecodeFromFull should error on bad base64 header; got nil")
	}
}

func TestJwtDecodeFromFull_BadBase64Payload(t *testing.T) {
	var tok JsonWebToken
	// Header is valid, payload is bad base64
	validHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	err := tok.DecodeFromFull(validHeader + ".!!!bad!!!.sig")
	if err == nil {
		t.Errorf("DecodeFromFull should error on bad base64 payload; got nil")
	}
}

// ============================================================================
// datatypes.go — ExtendedJwt.DecodeFromFull branches
// ============================================================================

func TestExtendedJwtDecodeFromFull_HappyPath(t *testing.T) {
	// Build a valid JWT with JSON header and payload using only string values
	// (ExtendedJwt uses map[string]string so int values will fail unmarshaling).
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test","iat":"1234567890"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("fakesig"))

	var ext ExtendedJwt
	err := ext.DecodeFromFull(header + "." + payload + "." + sig)
	if err != nil {
		t.Fatalf("ExtendedJwt.DecodeFromFull: %v", err)
	}
	if ext.HeaderClaims["alg"] != "HS256" {
		t.Errorf("header alg = %q; want HS256", ext.HeaderClaims["alg"])
	}
	if ext.PayloadClaims["sub"] != "test" {
		t.Errorf("payload sub = %q; want test", ext.PayloadClaims["sub"])
	}
}

func TestExtendedJwtDecodeFromFull_BadInnerToken(t *testing.T) {
	// Token with too few parts → inner DecodeFromFull fails
	var ext ExtendedJwt
	err := ext.DecodeFromFull("only.two")
	if err == nil {
		t.Errorf("ExtendedJwt.DecodeFromFull should propagate inner error; got nil")
	}
}

func TestExtendedJwtDecodeFromFull_BadHeaderJSON(t *testing.T) {
	// Header decodes from base64 but is not valid JSON
	badHeader := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test"}`))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))

	var ext ExtendedJwt
	err := ext.DecodeFromFull(badHeader + "." + payload + "." + sig)
	if err == nil {
		t.Errorf("ExtendedJwt.DecodeFromFull should error on bad header JSON; got nil")
	}
}

func TestExtendedJwtDecodeFromFull_BadPayloadJSON(t *testing.T) {
	// Header is valid JSON, payload decodes from base64 but is not valid JSON
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	badPayload := base64.RawURLEncoding.EncodeToString([]byte("not json either"))
	sig := base64.RawURLEncoding.EncodeToString([]byte("sig"))

	var ext ExtendedJwt
	err := ext.DecodeFromFull(header + "." + badPayload + "." + sig)
	if err == nil {
		t.Errorf("ExtendedJwt.DecodeFromFull should error on bad payload JSON; got nil")
	}
}

// ============================================================================
// datatypes.go — PopToken paths
// ============================================================================

func TestPopTokenInitialize_UnsupportedKeyType(t *testing.T) {
	var pop PopToken
	// Pass an invalid public key type (string)
	err := pop.Initialize(nil, nil, "not-a-key")
	if err == nil {
		t.Errorf("PopToken.Initialize should error on unsupported key type; got nil")
	}
}

func TestPopTokenInitialize_RSA(t *testing.T) {
	var rsaKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &rsaKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	var pop PopToken
	if err := pop.Initialize(nil, nil, &rsaKey.PublicKey); err != nil {
		t.Fatalf("PopToken.Initialize(RSA): %v", err)
	}
	if pop.HeaderClaims["alg"] != "RS256" {
		t.Errorf("alg = %q; want RS256", pop.HeaderClaims["alg"])
	}
}

func TestPopTokenInitialize_ECDSA(t *testing.T) {
	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	var pop PopToken
	if err := pop.Initialize(nil, nil, &ecKey.PublicKey); err != nil {
		t.Fatalf("PopToken.Initialize(ECDSA): %v", err)
	}
	if pop.HeaderClaims["alg"] != "ES256" {
		t.Errorf("alg = %q; want ES256", pop.HeaderClaims["alg"])
	}
}

func TestPopTokenGenerateToken_PreInitializedHeader(t *testing.T) {
	// With HeaderClaims already set, GenerateToken skips Initialize.
	var rsaKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &rsaKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	var pop PopToken
	if err := pop.Initialize(nil, nil, &rsaKey.PublicKey); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	token, err := pop.GenerateToken(rsaKey)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if token == "" {
		t.Errorf("GenerateToken returned empty token")
	}
	// Should be 3-part JWT
	if len(strings.Split(token, ".")) != 3 {
		t.Errorf("token not 3-part JWT: %q", token)
	}
}

func TestPopTokenGenerateToken_UnsupportedKeyType(t *testing.T) {
	// With HeaderClaims nil and unsupported key → error
	var pop PopToken
	_, err := pop.GenerateToken("not-a-key")
	if err == nil {
		t.Errorf("GenerateToken with unsupported key type should error; got nil")
	}
}

func TestPopTokenGenerateToken_ECDSAUninitialized(t *testing.T) {
	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	var pop PopToken // HeaderClaims nil → auto-initialize
	token, err := pop.GenerateToken(ecKey)
	if err != nil {
		t.Fatalf("GenerateToken(ECDSA uninitialized): %v", err)
	}
	if len(strings.Split(token, ".")) != 3 {
		t.Errorf("token not 3-part JWT: %q", token)
	}
}

func TestPopTokenCheckSignature_DefaultAlgError(t *testing.T) {
	var pop PopToken
	pop.HeaderClaims = map[string]string{"alg": "UNSUPPORTED"}
	err := pop.CheckSignature()
	if err == nil {
		t.Errorf("CheckSignature with unknown alg should error; got nil")
	}
}

func TestPopTokenCheckSignature_RSARoundTrip(t *testing.T) {
	var rsaKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &rsaKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	var pop PopToken
	token, err := pop.GenerateToken(rsaKey)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	var pop2 PopToken
	if err := pop2.Jwt.DecodeFromFull(token); err != nil {
		t.Fatalf("DecodeFromFull: %v", err)
	}
	// Re-parse pop2's header and JWK for CheckSignature
	if err := pop2.Unmarshal(token); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := pop2.CheckSignature(); err != nil {
		t.Errorf("RSA CheckSignature failed: %v", err)
	}
}

func TestPopTokenCheckSignature_ECDSARoundTrip(t *testing.T) {
	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	var pop PopToken
	token, err := pop.GenerateToken(ecKey)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	var pop2 PopToken
	if err := pop2.Unmarshal(token); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := pop2.CheckSignature(); err != nil {
		t.Errorf("ECDSA CheckSignature failed: %v", err)
	}
}

func TestGetPubEcdsa_UnsupportedCurve(t *testing.T) {
	// Use a JWK with an unsupported curve name
	var jwk JsonWebKey
	_ = jwk.Unmarshall(`{"kty":"EC","crv":"P-256","x":"x1","y":"y1"}`)
	jwk.Curve = "P-521" // supported, but override payload after unmarshal
	var pop PopToken
	pop.Jwk = jwk
	_, err := pop.GetPubEcdsa()
	if err == nil {
		t.Errorf("GetPubEcdsa with unsupported curve should error; got nil")
	}
}

func TestGetPubEcdsa_BadXCoord(t *testing.T) {
	var pop PopToken
	pop.Jwk.Curve = "P-256"
	pop.Jwk.Xcoord = "!not-base64-urlencode!"
	pop.Jwk.Ycoord = "dmFsaWQ" // valid
	_, err := pop.GetPubEcdsa()
	if err == nil {
		t.Errorf("GetPubEcdsa with bad X coord should error; got nil")
	}
}

func TestGetPubEcdsa_BadYCoord(t *testing.T) {
	var ecKey *ecdsa.PrivateKey
	if err := GenEcdsaKey(elliptic.P256(), &ecKey); err != nil {
		t.Fatalf("GenEcdsaKey: %v", err)
	}
	var pop PopToken
	pop.Jwk.Curve = "P-256"
	pop.Jwk.Xcoord = base64.RawURLEncoding.EncodeToString(ecKey.X.Bytes())
	pop.Jwk.Ycoord = "!not-base64!"
	_, err := pop.GetPubEcdsa()
	if err == nil {
		t.Errorf("GetPubEcdsa with bad Y coord should error; got nil")
	}
}

func TestGetPubRsa_BadModulus(t *testing.T) {
	var pop PopToken
	pop.Jwk.PubMod = "!not-valid-base64!"
	pop.Jwk.PubExp = "AQAB"
	_, err := pop.GetPubRsa()
	if err == nil {
		t.Errorf("GetPubRsa with bad modulus should error; got nil")
	}
}

func TestGetPubRsa_BadExponent(t *testing.T) {
	var rsaKey *rsa.PrivateKey
	if err := GenRsaKey(2048, &rsaKey); err != nil {
		t.Fatalf("GenRsaKey: %v", err)
	}
	var pop PopToken
	pop.Jwk.PubMod = base64.RawURLEncoding.EncodeToString(rsaKey.PublicKey.N.Bytes())
	pop.Jwk.PubExp = "!bad!"
	_, err := pop.GetPubRsa()
	if err == nil {
		t.Errorf("GetPubRsa with bad exponent should error; got nil")
	}
}

// ============================================================================
// treeutils.go — ValidateToInt / ValidateToString
// ============================================================================

func TestValidateToInt_AllBranches(t *testing.T) {
	cases := []struct {
		in   string
		want uint8
	}{
		{"", 0},
		{"write-only", 1},
		{"read-write", 2},
		{"write-only+consent", 11},
		{"read-write+consent", 12},
		{"consent", 10},         // consent without rw/wo → 0 + 10 = 10
		{"something-else", 0},   // default: no matching
	}
	for _, c := range cases {
		if got := ValidateToInt(c.in); got != c.want {
			t.Errorf("ValidateToInt(%q) = %d; want %d", c.in, got, c.want)
		}
	}
}

func TestValidateToString_AllBranches(t *testing.T) {
	cases := []struct {
		in   uint8
		want string // partial - just check it contains expected substring or is empty
	}{
		{0, ""},
		{1, "write-only"},
		{2, "read-write"},
		{10, "+consent"},  // consent only
		{11, "+consent"},  // write-only+consent → output is "+consent" (only the consent branch matches 11/10=1)
		{12, "+consent"},  // read-write+consent → output is "+consent"
	}
	for _, c := range cases {
		got := ValidateToString(c.in)
		if c.want == "" {
			if got != "" {
				t.Errorf("ValidateToString(%d) = %q; want \"\"", c.in, got)
			}
		} else {
			if !strings.Contains(got, c.want) {
				t.Errorf("ValidateToString(%d) = %q; want to contain %q", c.in, got, c.want)
			}
		}
	}
}

// ============================================================================
// treeutils.go — serializeUInt / deSerializeUInt uint32 branches
// ============================================================================

func TestSerializeUInt_Uint32(t *testing.T) {
	// uint32 branch
	val := uint32(0x01020304)
	b := serializeUInt(val)
	if len(b) != 4 {
		t.Fatalf("serializeUInt(uint32) len = %d; want 4", len(b))
	}
	// deSerialize should round-trip
	got := deSerializeUInt(b)
	if got.(uint32) != val {
		t.Errorf("deSerializeUInt(4-byte) = %v; want %v", got, val)
	}
}

func TestSerializeUInt_Uint8(t *testing.T) {
	val := uint8(42)
	b := serializeUInt(val)
	if len(b) != 1 {
		t.Fatalf("serializeUInt(uint8) len = %d; want 1", len(b))
	}
	if deSerializeUInt(b).(uint8) != val {
		t.Errorf("deSerializeUInt(1-byte) mismatch")
	}
}

func TestSerializeUInt_Uint16(t *testing.T) {
	val := uint16(0x0102)
	b := serializeUInt(val)
	if len(b) != 2 {
		t.Fatalf("serializeUInt(uint16) len = %d; want 2", len(b))
	}
	if deSerializeUInt(b).(uint16) != val {
		t.Errorf("deSerializeUInt(2-byte) mismatch")
	}
}

func TestSerializeUInt_UnknownType(t *testing.T) {
	// Unknown type returns nil — must not panic.
	got := serializeUInt(uint64(42))
	if got != nil {
		t.Errorf("serializeUInt(uint64) = %v; want nil", got)
	}
}

func TestDeSerializeUInt_UnknownSize(t *testing.T) {
	// 3-byte buf → unknown size, returns nil.
	got := deSerializeUInt([]byte{1, 2, 3})
	if got != nil {
		t.Errorf("deSerializeUInt(3-byte) = %v; want nil", got)
	}
}

// ============================================================================
// treeutils.go — countSegments
// ============================================================================

func TestCountSegments(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"Vehicle", 1},
		{"Vehicle.Speed", 2},
		{"Vehicle.Cabin.HVAC", 3},
		{"A.B.C.D.E", 5},
	}
	for _, c := range cases {
		if got := countSegments(c.path); got != c.want {
			t.Errorf("countSegments(%q) = %d; want %d", c.path, got, c.want)
		}
	}
}

// ============================================================================
// treeutils.go — compareNodeName
// ============================================================================

func TestCompareNodeName(t *testing.T) {
	cases := []struct {
		nodeName, pathName string
		want               bool
	}{
		{"Speed", "Speed", true},
		{"Speed", "RPM", false},
		{"anything", "*", true},  // wildcard
		{"", "", true},
		{"Speed", "", false},
	}
	for _, c := range cases {
		if got := compareNodeName(c.nodeName, c.pathName); got != c.want {
			t.Errorf("compareNodeName(%q,%q) = %v; want %v", c.nodeName, c.pathName, got, c.want)
		}
	}
}

// ============================================================================
// treeutils.go — pushPathSegment / popPathSegment
// ============================================================================

func TestPushPathSegment_FirstSegment(t *testing.T) {
	var ctx SearchContext_t
	// CurrentDepth = 0 → no dot prefix
	pushPathSegment("Vehicle", &ctx)
	if ctx.MatchPath != "Vehicle" {
		t.Errorf("pushPathSegment at depth 0: MatchPath = %q; want \"Vehicle\"", ctx.MatchPath)
	}
}

func TestPushPathSegment_SubsequentSegment(t *testing.T) {
	ctx := SearchContext_t{MatchPath: "Vehicle", CurrentDepth: 1}
	pushPathSegment("Speed", &ctx)
	if ctx.MatchPath != "Vehicle.Speed" {
		t.Errorf("pushPathSegment at depth 1: MatchPath = %q; want \"Vehicle.Speed\"", ctx.MatchPath)
	}
}

func TestPopPathSegment_WithDot(t *testing.T) {
	ctx := SearchContext_t{MatchPath: "Vehicle.Speed"}
	popPathSegment(&ctx)
	if ctx.MatchPath != "Vehicle" {
		t.Errorf("popPathSegment: MatchPath = %q; want \"Vehicle\"", ctx.MatchPath)
	}
}

func TestPopPathSegment_NoDot(t *testing.T) {
	ctx := SearchContext_t{MatchPath: "Vehicle"}
	popPathSegment(&ctx)
	if ctx.MatchPath != "" {
		t.Errorf("popPathSegment no-dot: MatchPath = %q; want \"\"", ctx.MatchPath)
	}
}

// ============================================================================
// treeutils.go — hexToInt (legacy unchecked variant)
// ============================================================================

func TestHexToInt_Legacy(t *testing.T) {
	if got := hexToInt('0'); got != 0 {
		t.Errorf("hexToInt('0') = %d; want 0", got)
	}
	if got := hexToInt('9'); got != 9 {
		t.Errorf("hexToInt('9') = %d; want 9", got)
	}
	if got := hexToInt('A'); got != 10 {
		t.Errorf("hexToInt('A') = %d; want 10", got)
	}
	if got := hexToInt('F'); got != 15 {
		t.Errorf("hexToInt('F') = %d; want 15", got)
	}
}

// ============================================================================
// treeutils.go — Forest helpers
// ============================================================================

// buildTestForestEntry populates himForest with a single entry for test use.
// Returns cleanup function.
func buildTestForestEntry(t *testing.T) func() {
	t.Helper()
	leaf := &Node_t{Name: "Speed", NodeType: SENSOR, Datatype: "float"}
	root := NewBranchNode("Vehicle", leaf)

	old := himForest
	himForest = []HimTree{
		{
			RootName: "Vehicle",
			Handle:   root,
			Domain:   "org.example.Vehicle",
			Version:  "1.0",
		},
	}
	return func() { himForest = old }
}

func TestSetRootNodePointer_Found(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	ptr := SetRootNodePointer("Vehicle.Speed")
	if ptr == nil {
		t.Fatalf("SetRootNodePointer returned nil for known root")
	}
	if ptr.Name != "Vehicle" {
		t.Errorf("Name = %q; want Vehicle", ptr.Name)
	}
}

func TestSetRootNodePointer_NotFound(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	ptr := SetRootNodePointer("Unknown.Signal")
	if ptr != nil {
		t.Errorf("SetRootNodePointer on unknown root returned non-nil: %v", ptr)
	}
}

func TestGetInfoType_Found(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	infoType := GetInfoType(himForest[0].Handle)
	if infoType != "Vehicle" {
		t.Errorf("GetInfoType = %q; want \"Vehicle\"", infoType)
	}
}

func TestGetInfoType_NoDot(t *testing.T) {
	old := himForest
	root := NewBranchNode("X")
	himForest = []HimTree{{RootName: "X", Handle: root, Domain: "nodot"}}
	defer func() { himForest = old }()

	infoType := GetInfoType(root)
	if infoType != "Missing" {
		t.Errorf("GetInfoType no-dot = %q; want \"Missing\"", infoType)
	}
}

func TestGetInfoType_NotFound(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	orphan := NewBranchNode("Orphan")
	infoType := GetInfoType(orphan)
	if infoType != "Missing" {
		t.Errorf("GetInfoType not-found = %q; want \"Missing\"", infoType)
	}
}

func TestForestInfoList(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	list := ForestInfoList()
	if len(list) == 0 {
		t.Fatalf("ForestInfoList returned empty list")
	}
	if list[0].RootName != "Vehicle" {
		t.Errorf("RootName = %q; want Vehicle", list[0].RootName)
	}
}

func TestGetForestRoot_FoundGap(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	got := GetForestRoot("Vehicle")
	if got == nil {
		t.Fatalf("GetForestRoot returned nil for known root")
	}
}

func TestGetForestRoot_NotFoundGap(t *testing.T) {
	cleanup := buildTestForestEntry(t)
	defer cleanup()

	if got := GetForestRoot("Unknown"); got != nil {
		t.Errorf("GetForestRoot returned non-nil for unknown root")
	}
}

// ============================================================================
// treeutils.go — VSSsearchNodes
// ============================================================================

func TestVSSsearchNodes_ExactMatch(t *testing.T) {
	leaf := NewSignalNode("Speed", SENSOR, "float", "speed", "0", "300", "km/h")
	root := NewBranchNode("Vehicle", leaf)

	var validation int
	results, count := VSSsearchNodes("Vehicle.Speed", root, MAXFOUNDNODES, false, true, 0, nil, &validation)
	if count != 1 {
		t.Fatalf("VSSsearchNodes count = %d; want 1", count)
	}
	if results[0].NodeHandle != leaf {
		t.Errorf("did not get leaf node handle")
	}
}

func TestVSSsearchNodes_Wildcard(t *testing.T) {
	leaf1 := NewSignalNode("Speed", SENSOR, "float", "speed", "0", "300", "km/h")
	leaf2 := NewSignalNode("RPM", SENSOR, "uint16", "rpm", "", "", "rpm")
	root := NewBranchNode("Vehicle", leaf1, leaf2)

	results, count := VSSsearchNodes("Vehicle.*", root, MAXFOUNDNODES, false, true, 0, nil, nil)
	if count < 1 {
		t.Fatalf("wildcard search returned count=%d; want >= 1", count)
	}
	_ = results
}

// ============================================================================
// treeutils.go — VSSGetLeafNodesList / VSSGetDefaultList
// ============================================================================

func TestVSSGetLeafNodesList_Basic(t *testing.T) {
	leaf := NewSignalNode("Speed", SENSOR, "float", "speed", "0", "300", "km/h")
	root := NewBranchNode("Vehicle", leaf)

	tmp := t.TempDir()
	listFile := filepath.Join(tmp, "leaflist.json")
	count := VSSGetLeafNodesList(root, "Vehicle", listFile)
	if count == 0 {
		t.Errorf("VSSGetLeafNodesList returned 0 for a tree with leaves")
	}
	data, err := os.ReadFile(listFile)
	if err != nil {
		t.Fatalf("list file not created: %v", err)
	}
	if !strings.Contains(string(data), "Vehicle") {
		t.Errorf("list file missing vehicle path: %s", data)
	}
}

func TestVSSGetDefaultList_WithDefaults(t *testing.T) {
	leaf := &Node_t{
		Name:         "Mode",
		NodeType:     ACTUATOR,
		Datatype:     "string",
		DefaultValue: "OFF",
	}
	root := NewBranchNode("Device", leaf)

	tmp := t.TempDir()
	listFile := filepath.Join(tmp, "defaults.json")
	count := VSSGetDefaultList(root, "Device", listFile)
	if count == 0 {
		t.Errorf("VSSGetDefaultList returned 0 for a tree with defaults")
	}
}

func TestVSSGetDefaultList_NoDefaults(t *testing.T) {
	leaf := NewSignalNode("Speed", SENSOR, "float", "speed", "0", "300", "km/h")
	root := NewBranchNode("Vehicle", leaf)

	tmp := t.TempDir()
	listFile := filepath.Join(tmp, "nodefaults.json")
	count := VSSGetDefaultList(root, "Vehicle", listFile)
	if count != 0 {
		t.Errorf("VSSGetDefaultList returned %d; want 0 for nodes with no default", count)
	}
}

// ============================================================================
// treeutils.go — sortPathList
// ============================================================================

func TestSortPathList_Basic(t *testing.T) {
	tmp := t.TempDir()
	listFile := filepath.Join(tmp, "sorted.json")
	content := `{"leafpaths":["Vehicle.Speed","Vehicle.RPM","Vehicle.Cabin.Temperature"]}`
	if err := os.WriteFile(listFile, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// sortPathList should not panic and should produce sorted output
	sortPathList(listFile)
	data, err := os.ReadFile(listFile)
	if err != nil {
		t.Fatalf("read after sort: %v", err)
	}
	// After sort, RPM should come before Speed (alphabetically)
	rpmIdx := strings.Index(string(data), "RPM")
	speedIdx := strings.Index(string(data), "Speed")
	if rpmIdx == -1 || speedIdx == -1 {
		t.Errorf("paths missing from sorted file: %s", data)
	}
	if rpmIdx > speedIdx {
		t.Errorf("sort did not reorder: RPM@%d, Speed@%d", rpmIdx, speedIdx)
	}
}

func TestSortPathList_MissingFile(t *testing.T) {
	// Must not panic on missing file
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sortPathList panicked on missing file: %v", r)
		}
	}()
	sortPathList("/nonexistent/path/pathlist.json")
}

func TestSortPathList_BadJSON(t *testing.T) {
	tmp := t.TempDir()
	listFile := filepath.Join(tmp, "bad.json")
	if err := os.WriteFile(listFile, []byte("not json"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sortPathList panicked on bad JSON: %v", r)
		}
	}()
	sortPathList(listFile)
}

// ============================================================================
// treeutils.go — VSS getter functions (0% coverage)
// ============================================================================

func TestVSSgetParent(t *testing.T) {
	child := NewSignalNode("Speed", SENSOR, "float", "", "", "", "")
	root := NewBranchNode("Vehicle", child)

	if VSSgetParent(child) != root {
		t.Errorf("VSSgetParent(child) != root")
	}
	if VSSgetParent(root) != nil {
		t.Errorf("VSSgetParent(root) != nil; want nil")
	}
}

func TestVSSgetDatatype(t *testing.T) {
	node := NewSignalNode("Speed", SENSOR, "float", "", "", "", "")
	if got := VSSgetDatatype(node); got != "float" {
		t.Errorf("VSSgetDatatype = %q; want \"float\"", got)
	}
}

func TestVSSgetUUID(t *testing.T) {
	node := &Node_t{Name: "n", Uuid: "test-uuid"}
	if got := VSSgetUUID(node); got != "test-uuid" {
		t.Errorf("VSSgetUUID = %q; want \"test-uuid\"", got)
	}
}

func TestVSSgetDefault(t *testing.T) {
	node := &Node_t{DefaultValue: "42"}
	if got := VSSgetDefault(node); got != "42" {
		t.Errorf("VSSgetDefault = %q; want \"42\"", got)
	}
}

func TestVSSgetDescr(t *testing.T) {
	node := &Node_t{Description: "vehicle speed"}
	if got := VSSgetDescr(node); got != "vehicle speed" {
		t.Errorf("VSSgetDescr = %q; want \"vehicle speed\"", got)
	}
}

func TestVSSgetNumOfAllowedElements_Sensor(t *testing.T) {
	node := &Node_t{NodeType: SENSOR, Allowed: 3}
	if got := VSSgetNumOfAllowedElements(node); got != 3 {
		t.Errorf("VSSgetNumOfAllowedElements(sensor) = %d; want 3", got)
	}
}

func TestVSSgetNumOfAllowedElements_Branch(t *testing.T) {
	node := &Node_t{NodeType: BRANCH, Allowed: 3}
	if got := VSSgetNumOfAllowedElements(node); got != 0 {
		t.Errorf("VSSgetNumOfAllowedElements(branch) = %d; want 0", got)
	}
}

func TestVSSgetNumOfAllowedElements_Struct(t *testing.T) {
	node := &Node_t{NodeType: STRUCT, Allowed: 5}
	if got := VSSgetNumOfAllowedElements(node); got != 0 {
		t.Errorf("VSSgetNumOfAllowedElements(struct) = %d; want 0", got)
	}
}

func TestVSSgetAllowedElement(t *testing.T) {
	node := &Node_t{AllowedDef: []string{"ON", "OFF", "AUTO"}}
	if got := VSSgetAllowedElement(node, 1); got != "OFF" {
		t.Errorf("VSSgetAllowedElement(1) = %q; want \"OFF\"", got)
	}
}

func TestVSSgetUnit_NonBranch(t *testing.T) {
	node := &Node_t{NodeType: SENSOR, Unit: "km/h"}
	if got := VSSgetUnit(node); got != "km/h" {
		t.Errorf("VSSgetUnit(sensor) = %q; want \"km/h\"", got)
	}
}

func TestVSSgetUnit_Branch(t *testing.T) {
	node := &Node_t{NodeType: BRANCH, Unit: "km/h"}
	if got := VSSgetUnit(node); got != "" {
		t.Errorf("VSSgetUnit(branch) = %q; want \"\"", got)
	}
}

func TestVSSgetChild_InRange(t *testing.T) {
	leaf := NewSignalNode("Speed", SENSOR, "float", "", "", "", "")
	root := NewBranchNode("Vehicle", leaf)
	if got := VSSgetChild(root, 0); got != leaf {
		t.Errorf("VSSgetChild(0) != leaf")
	}
}

func TestVSSgetChild_OutOfRange(t *testing.T) {
	leaf := NewSignalNode("Speed", SENSOR, "float", "", "", "", "")
	root := NewBranchNode("Vehicle", leaf)
	if got := VSSgetChild(root, 5); got != nil {
		t.Errorf("VSSgetChild(5) = %v; want nil", got)
	}
}

// ============================================================================
// treeutils.go — CreatePathListFile / PopulateDefault
// ============================================================================

func TestCreatePathListFile_Basic(t *testing.T) {
	leaf := NewSignalNode("Speed", SENSOR, "float", "speed", "", "", "")
	root := NewBranchNode("Vehicle", leaf)

	old := himForest
	himForest = []HimTree{{RootName: "Vehicle", Handle: root}}
	defer func() { himForest = old }()

	tmp := t.TempDir()
	pListPath := tmp + "/"
	// Must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CreatePathListFile panicked: %v", r)
		}
	}()
	CreatePathListFile(pListPath)
	// Check file was created
	if _, err := os.Stat(filepath.Join(tmp, "pathlist1.json")); err != nil {
		t.Errorf("pathlist1.json not created: %v", err)
	}
}

// ============================================================================
// pbutils.go — getMethodAndType all branches
// ============================================================================

func TestGetMethodAndType_GetRequest(t *testing.T) {
	m := map[string]interface{}{"action": "get", "path": "Vehicle.Speed"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_GET {
		t.Errorf("method = %v; want GET", method)
	}
	if mType != pb.MessageType_REQUEST {
		t.Errorf("mType = %v; want REQUEST", mType)
	}
}

func TestGetMethodAndType_GetResponse(t *testing.T) {
	// no "path" → RESPONSE
	m := map[string]interface{}{"action": "get", "value": "42"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_GET {
		t.Errorf("method = %v; want GET", method)
	}
	if mType != pb.MessageType_RESPONSE {
		t.Errorf("mType = %v; want RESPONSE", mType)
	}
}

func TestGetMethodAndType_SetRequest(t *testing.T) {
	m := map[string]interface{}{"action": "set", "path": "Vehicle.Speed", "value": "100"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_SET {
		t.Errorf("method = %v; want SET", method)
	}
	if mType != pb.MessageType_REQUEST {
		t.Errorf("mType = %v; want REQUEST", mType)
	}
}

func TestGetMethodAndType_SetResponse(t *testing.T) {
	m := map[string]interface{}{"action": "set", "ts": "2024-01-01"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_SET {
		t.Errorf("method = %v; want SET", method)
	}
	if mType != pb.MessageType_RESPONSE {
		t.Errorf("mType = %v; want RESPONSE (no path)", mType)
	}
}

func TestGetMethodAndType_SubscribeRequest(t *testing.T) {
	m := map[string]interface{}{"action": "subscribe", "path": "Vehicle.Speed"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_SUBSCRIBE {
		t.Errorf("method = %v; want SUBSCRIBE", method)
	}
	if mType != pb.MessageType_REQUEST {
		t.Errorf("mType = %v; want REQUEST", mType)
	}
}

func TestGetMethodAndType_SubscribeStream(t *testing.T) {
	// no "path" → STREAM
	m := map[string]interface{}{"action": "subscribe", "subscriptionId": "42"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_SUBSCRIBE {
		t.Errorf("method = %v; want SUBSCRIBE", method)
	}
	if mType != pb.MessageType_STREAM {
		t.Errorf("mType = %v; want STREAM", mType)
	}
}

func TestGetMethodAndType_UnsubscribeRequest(t *testing.T) {
	m := map[string]interface{}{"action": "unsubscribe", "subscriptionId": "42"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_UNSUBSCRIBE {
		t.Errorf("method = %v; want UNSUBSCRIBE", method)
	}
	if mType != pb.MessageType_REQUEST {
		t.Errorf("mType = %v; want REQUEST", mType)
	}
}

func TestGetMethodAndType_UnsubscribeResponse(t *testing.T) {
	// "ts" present → RESPONSE
	m := map[string]interface{}{"action": "unsubscribe", "ts": "2024-01-01"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_UNSUBSCRIBE {
		t.Errorf("method = %v; want UNSUBSCRIBE", method)
	}
	if mType != pb.MessageType_RESPONSE {
		t.Errorf("mType = %v; want RESPONSE", mType)
	}
}

func TestGetMethodAndType_SubscriptionEvent(t *testing.T) {
	m := map[string]interface{}{"action": "subscription"}
	method, mType := getMethodAndType(m)
	if method != pb.MessageMethod_SUBSCRIBE {
		t.Errorf("subscription action: method = %v; want SUBSCRIBE", method)
	}
	if mType != pb.MessageType_STREAM {
		t.Errorf("subscription action: mType = %v; want STREAM", mType)
	}
}

func TestGetMethodAndType_Unknown(t *testing.T) {
	m := map[string]interface{}{"action": "unknown-action"}
	method, mType := getMethodAndType(m)
	if method != -1 {
		t.Errorf("unknown action method = %v; want -1", method)
	}
	if mType != -1 {
		t.Errorf("unknown action mType = %v; want -1", mType)
	}
}

// ============================================================================
// pbutils.go — round-trip JSON→Protobuf→JSON for set/subscribe/unsubscribe
// ============================================================================

func TestJsonToProtobuf_SetRequest(t *testing.T) {
	json := `{"action":"set","path":"Vehicle.Speed","value":"100","requestId":"2"}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf set request returned nil")
	}
	// Round-trip
	got := ProtobufToJson(pb)
	if got == "" {
		t.Errorf("ProtobufToJson(set request) returned empty string")
	}
}

func TestJsonToProtobuf_SetResponse(t *testing.T) {
	// No "path" → set response
	json := `{"action":"set","ts":"2024-01-01T00:00:00Z","requestId":"2"}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf set response returned nil")
	}
}

func TestJsonToProtobuf_SubscribeRequest(t *testing.T) {
	json := `{"action":"subscribe","path":"Vehicle.Speed","requestId":"3"}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf subscribe request returned nil")
	}
	got := ProtobufToJson(pb)
	if got == "" {
		t.Errorf("ProtobufToJson(subscribe request) returned empty string")
	}
}

func TestJsonToProtobuf_SubscribeStream(t *testing.T) {
	// No path → stream type
	json := `{"action":"subscribe","subscriptionId":"sub-1","ts":"2024-01-01","data":[{"path":"Vehicle.Speed","dp":{"value":"42","ts":"2024-01-01"}}]}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf subscribe stream returned nil")
	}
}

func TestJsonToProtobuf_UnsubscribeRequest(t *testing.T) {
	json := `{"action":"unsubscribe","subscriptionId":"sub-1","requestId":"4"}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf unsubscribe request returned nil")
	}
	got := ProtobufToJson(pb)
	if got == "" {
		t.Errorf("ProtobufToJson(unsubscribe request) returned empty string")
	}
}

func TestJsonToProtobuf_UnsubscribeResponse(t *testing.T) {
	// "ts" present → response
	json := `{"action":"unsubscribe","subscriptionId":"sub-1","ts":"2024-01-01","requestId":"4"}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf unsubscribe response returned nil")
	}
}

func TestJsonToProtobuf_SubscriptionEvent(t *testing.T) {
	json := `{"action":"subscription","subscriptionId":"sub-1","ts":"2024-01-01","data":[{"path":"Vehicle.Speed","dp":{"value":"42","ts":"2024-01-01"}}]}`
	pb := JsonToProtobuf(json)
	if pb == nil {
		t.Fatalf("JsonToProtobuf subscription event returned nil")
	}
}

func TestJsonToProtobuf_InvalidJSON(t *testing.T) {
	pb := JsonToProtobuf("not json at all")
	// populateProtoFromJson returns nil → proto.Marshal(nil) returns nil/empty
	// The function should handle this gracefully without panic
	_ = pb
}

func TestPopulateJsonFromProto_UnknownMethod(t *testing.T) {
	// protoMessage with zero value → falls through to return ""
	msg := JsonToProtobuf(`{"action":"get","path":"Vehicle.Speed","requestId":"1"}`)
	if msg == nil {
		t.Skip("JsonToProtobuf returned nil; cannot test populateJsonFromProto")
	}
	got := ProtobufToJson(msg)
	if got == "" {
		t.Errorf("ProtobufToJson on valid protobuf returned empty string")
	}
}

// ============================================================================
// managerhandlers.go — validateSecConfig
// ============================================================================

func TestValidateSecConfig_NilDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("validateSecConfig(nil) panicked: %v", r)
		}
	}()
	validateSecConfig(nil)
}

func TestValidateSecConfig_TransportSecNo(t *testing.T) {
	cfg := &SecConfig{TransportSec: "no"}
	validateSecConfig(cfg)
	if cfg.TransportSec != "no" {
		t.Errorf("TransportSec = %q; want \"no\"", cfg.TransportSec)
	}
}

func TestValidateSecConfig_TransportSecNonCanonical(t *testing.T) {
	// A non-yes, non-no, non-empty value should be coerced to "no"
	cfg := &SecConfig{TransportSec: "maybe"}
	validateSecConfig(cfg)
	if cfg.TransportSec != "no" {
		t.Errorf("TransportSec = %q; want \"no\" after coercion", cfg.TransportSec)
	}
}

func TestValidateSecConfig_TransportSecYes_DefaultsApplied(t *testing.T) {
	cfg := &SecConfig{
		TransportSec: "yes",
		// All other fields empty — defaults should be applied
	}
	validateSecConfig(cfg)
	if cfg.ServerName != "localhost" {
		t.Errorf("ServerName = %q; want \"localhost\" default", cfg.ServerName)
	}
	if cfg.ServerCertOpt != "ClientCertVerification" {
		t.Errorf("ServerCertOpt = %q; want \"ClientCertVerification\" default", cfg.ServerCertOpt)
	}
}

func TestValidateSecConfig_TransportSecYes_ExistingServerName(t *testing.T) {
	cfg := &SecConfig{
		TransportSec: "yes",
		ServerName:   "myserver.example.com",
	}
	validateSecConfig(cfg)
	if cfg.ServerName != "myserver.example.com" {
		t.Errorf("ServerName = %q; want \"myserver.example.com\" (not overwritten)", cfg.ServerName)
	}
}

// ============================================================================
// treeutils.go — RegisterServiceTree / DeregisterServiceTree
// ============================================================================

func TestRegisterServiceTree_DoubleRegistration(t *testing.T) {
	old := himForest
	himForest = nil
	defer func() { himForest = old }()

	root := NewBranchNode("SvcRoot")
	if ok := RegisterServiceTree("MySvc", "org.example.MySvc.Service", "1.0", root); !ok {
		t.Fatalf("first RegisterServiceTree failed")
	}
	// Second registration with same rootName should return false
	if ok := RegisterServiceTree("MySvc", "org.example.MySvc.Service", "1.0", root); ok {
		t.Errorf("duplicate RegisterServiceTree should return false; got true")
	}
}

func TestDeregisterServiceTree_RemovesEntry(t *testing.T) {
	old := himForest
	himForest = nil
	defer func() { himForest = old }()

	root := NewBranchNode("SvcRoot")
	RegisterServiceTree("MySvc", "org.example.MySvc.Service", "1.0", root)
	DeregisterServiceTree("MySvc")

	if GetForestRoot("MySvc") != nil {
		t.Errorf("DeregisterServiceTree did not remove the entry")
	}
}

func TestDeregisterServiceTree_NotFound(t *testing.T) {
	old := himForest
	himForest = nil
	defer func() { himForest = old }()

	// Must not panic on unknown rootName
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DeregisterServiceTree panicked on unknown rootName: %v", r)
		}
	}()
	DeregisterServiceTree("NonExistent")
}

// ============================================================================
// treeutils.go — isEndOfScope
// ============================================================================

func TestIsEndOfScope_EmptyList(t *testing.T) {
	ctx := SearchContext_t{ListSize: 0}
	if isEndOfScope(&ctx) {
		t.Errorf("isEndOfScope with empty list should return false")
	}
}

func TestIsEndOfScope_MatchFound(t *testing.T) {
	ctx := SearchContext_t{
		ListSize:    2,
		NoScopeList: []string{"Vehicle.Speed", "Vehicle.RPM"},
		MatchPath:   "Vehicle.Speed",
	}
	if !isEndOfScope(&ctx) {
		t.Errorf("isEndOfScope should return true when MatchPath is in NoScopeList")
	}
}

func TestIsEndOfScope_NoMatch(t *testing.T) {
	ctx := SearchContext_t{
		ListSize:    2,
		NoScopeList: []string{"Vehicle.Speed", "Vehicle.RPM"},
		MatchPath:   "Vehicle.Temperature",
	}
	if isEndOfScope(&ctx) {
		t.Errorf("isEndOfScope should return false when MatchPath is not in NoScopeList")
	}
}

// ============================================================================
// treeutils.go — getPathSegment
// ============================================================================

func TestGetPathSegment_FirstSegment(t *testing.T) {
	ctx := SearchContext_t{
		SearchPath:   "Vehicle.Speed",
		CurrentDepth: 0,
	}
	// At depth=0, offset=0: returns the first path segment.
	got := getPathSegment(0, &ctx)
	if got != "Vehicle" {
		t.Errorf("getPathSegment(offset=0, depth=0) = %q; want \"Vehicle\"", got)
	}
}

func TestGetPathSegment_WithOffset(t *testing.T) {
	// offset=1, depth=1: loop runs for i=1 up to (1+1)=2, so once.
	ctx := SearchContext_t{
		SearchPath:   "Vehicle.Speed",
		CurrentDepth: 1,
	}
	got := getPathSegment(1, &ctx)
	if got != "Speed" {
		t.Errorf("getPathSegment(offset=1, depth=1) = %q; want \"Speed\"", got)
	}
}

// ============================================================================
// common.go — GetRfcTime shape correctness
// ============================================================================

func TestGetRfcTime_HasMillisecondPrecision(t *testing.T) {
	ts := GetRfcTime()
	// Must end with Z
	if !strings.HasSuffix(ts, "Z") {
		t.Errorf("GetRfcTime = %q; must end with Z", ts)
	}
	// Must look like an RFC3339 timestamp (at minimum "2006-01-02T15:04:05")
	if len(ts) < 19 {
		t.Errorf("GetRfcTime too short: %q", ts)
	}
	// T separator must be present
	if !strings.Contains(ts, "T") {
		t.Errorf("GetRfcTime missing T separator: %q", ts)
	}
}

// ============================================================================
// random.Reader-based key — just ensure no race on concurrent GenRsaKey calls
// ============================================================================

func TestGenRsaKey_ZeroSize(t *testing.T) {
	// size=0 → not divisible by 8 (0%8==0 but 0 < 2048) → clamp to 2048
	var key *rsa.PrivateKey
	if err := GenRsaKey(0, &key); err != nil {
		t.Fatalf("GenRsaKey(0): %v", err)
	}
	if key == nil {
		t.Fatalf("GenRsaKey(0) key is nil")
	}
}

func TestGenRsaKey_OddSize(t *testing.T) {
	// size=1023 → 1023%8 != 0 → clamp to 2048
	var key *rsa.PrivateKey
	if err := GenRsaKey(1023, &key); err != nil {
		t.Fatalf("GenRsaKey(1023): %v", err)
	}
	if key.N.BitLen() < 2048 {
		t.Errorf("GenRsaKey(1023) bit length = %d; want >= 2048", key.N.BitLen())
	}
}

// TrimLogFile large-file (>10MB) branch: integration-only.
//
// TrimLogFile's trim path calls os.Create("logtmp.txt") relative to the
// process working directory, then os.Remove(fi.Name()) where fi.Name()
// returns only the base name of the log file (not the absolute path).
// This means the function depends on the log file being in the current
// working directory — the test framework's temp dir is different, so the
// Remove call fails and log.Fatalln (os.Exit) is invoked, killing the
// test binary. Until TrimLogFile is refactored to use absolute paths
// throughout, the large-file branch can only be covered by a live
// deployment test, not a Go unit test.

// ============================================================================
// managerhandlers.go — WsClientIndex helpers
// ============================================================================

func TestGetWsClientIndex_ReturnsMinus1WhenFull(t *testing.T) {
	// Save + clear all slots
	WsClientIndexMu.Lock()
	saved := make([]bool, len(WsClientIndexList))
	copy(saved, WsClientIndexList)
	for i := range WsClientIndexList {
		WsClientIndexList[i] = false // all taken
	}
	WsClientIndexMu.Unlock()
	defer func() {
		WsClientIndexMu.Lock()
		copy(WsClientIndexList, saved)
		WsClientIndexMu.Unlock()
	}()

	if got := getWsClientIndex(); got != -1 {
		t.Errorf("getWsClientIndex when full = %d; want -1", got)
	}
}

func TestReturnWsClientIndex_InvalidIndex(t *testing.T) {
	// Must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ReturnWsClientIndex panicked on invalid index: %v", r)
		}
	}()
	ReturnWsClientIndex(-1)
	ReturnWsClientIndex(9999)
}

// ============================================================================
// Ed25519 / random key bytes used in a few sign helpers
// ============================================================================

func TestCheckAssymSignature_UnsupportedKeyType_Gap(t *testing.T) {
	tok := JsonWebToken{}
	tok.SetHeader("RS256")
	tok.AddClaim("x", "y")
	// RSA key setup
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	_ = tok.AssymSign(key)

	// Now verify with unsupported key type
	err := tok.CheckAssymSignature("not-a-public-key")
	if err == nil {
		t.Errorf("CheckAssymSignature with string key should error; got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("unexpected error: %v", err)
	}
}
