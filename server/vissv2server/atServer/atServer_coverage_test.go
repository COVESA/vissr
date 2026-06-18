package atServer

// Coverage for the AT generator and PoP validation, which the existing suite
// documented as needing real keypairs and therefore left at 0%.

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

// makePopPayload builds an AtGenPayload whose PoP token is validly signed by a
// fresh client key and whose AGT "pub" claim carries the matching JWK
// thumbprint (the binding validatePop checks). aud overrides the default.
func makePopPayload(t *testing.T, aud string) AtGenPayload {
	t.Helper()
	clientPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("client key: %v", err)
	}
	src := utils.PopToken{}
	if err := src.Initialize(nil, map[string]string{"aud": aud}, &clientPriv.PublicKey); err != nil {
		t.Fatalf("pop.Initialize: %v", err)
	}
	token, err := src.GenerateToken(clientPriv)
	if err != nil {
		t.Fatalf("pop.GenerateToken: %v", err)
	}
	var popTk utils.PopToken
	if err := popTk.Unmarshal(token); err != nil {
		t.Fatalf("pop.Unmarshal: %v", err)
	}
	return AtGenPayload{
		PopTk: popTk,
		Agt:   utils.ExtendedJwt{PayloadClaims: map[string]string{"pub": popTk.Jwk.Thumb}},
	}
}

func TestGenerateAt_ReturnsSignedToken(t *testing.T) {
	payload := AtGenPayload{
		Purpose: "fuel-status",
		Agt:     utils.ExtendedJwt{PayloadClaims: map[string]string{"clx": "Driver+Vehicle+Cloud"}},
	}
	tok := generateAt(payload)
	if strings.Contains(tok, "error") {
		t.Fatalf("generateAt returned an error: %s", tok)
	}
	if n := len(strings.Split(tok, ".")); n != 3 {
		t.Errorf("generateAt token has %d segments, want 3 (HS256 JWT): %s", n, tok)
	}
}

func TestValidatePop_HappyPath(t *testing.T) {
	ok, msg := validatePop(makePopPayload(t, "vissv2/agts"))
	if !ok {
		t.Fatalf("validatePop rejected a valid PoP: %s", msg)
	}
}

func TestValidatePop_WrongAud(t *testing.T) {
	ok, msg := validatePop(makePopPayload(t, "wrong-aud"))
	if ok {
		t.Fatal("validatePop accepted a PoP with the wrong aud")
	}
	if !strings.Contains(msg, "aud") {
		t.Errorf("expected aud error, got: %s", msg)
	}
}

func TestValidatePop_ThumbMismatch(t *testing.T) {
	p := makePopPayload(t, "vissv2/agts")
	p.Agt.PayloadClaims["pub"] = "not-the-thumbprint"
	ok, msg := validatePop(p)
	if ok {
		t.Fatal("validatePop accepted a PoP whose key does not match the AGT pub claim")
	}
	if !strings.Contains(msg, "matching") {
		t.Errorf("expected key-mismatch error, got: %s", msg)
	}
}

func TestValidatePop_StaleIat(t *testing.T) {
	p := makePopPayload(t, "vissv2/agts")
	// Force an iat far in the past so CheckIat rejects it.
	p.PopTk.PayloadClaims["iat"] = "100"
	ok, msg := validatePop(p)
	if ok {
		t.Fatal("validatePop accepted a PoP with a stale iat")
	}
	if !strings.Contains(msg, "iat") {
		t.Errorf("expected iat error, got: %s", msg)
	}
}

func TestValidatePop_RepeatedJti(t *testing.T) {
	p := makePopPayload(t, "vissv2/agts")
	if ok, msg := validatePop(p); !ok {
		t.Fatalf("first validatePop should pass: %s", msg)
	}
	// Same PoP (same jti) replayed must be rejected.
	if ok, msg := validatePop(p); ok || !strings.Contains(msg, "Repeated JTI") {
		t.Errorf("replayed PoP not rejected: ok=%v msg=%s", ok, msg)
	}
}
