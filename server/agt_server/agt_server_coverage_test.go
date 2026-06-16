package main

// Coverage for the AGT token generators (generateAgt / generateLTAgt) and
// initKey, which the original suite documented as integration-only. generateAgt
// was 0% and generateLTAgt only exercised the malformed-PoP early return.

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"strings"
	"testing"

	"github.com/covesa/vissr/utils"
)

// setTestKey installs a fresh RSA signing key into the package global privKey
// so the generators can sign. Restores the previous key on cleanup.
func setTestKey(t *testing.T) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	prev := privKey
	privKey = k
	t.Cleanup(func() { privKey = prev })
}

// signedPop builds a PoP token signed by a fresh client key, with aud
// "vissv2/agts", and returns the token plus its JWK thumbprint (the value the
// AGT server uses as payload.Key).
func signedPop(t *testing.T) (token, thumb string) {
	t.Helper()
	clientPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("client key: %v", err)
	}
	pop := utils.PopToken{}
	if err := pop.Initialize(nil, map[string]string{"aud": "vissv2/agts"}, &clientPriv.PublicKey); err != nil {
		t.Fatalf("pop.Initialize: %v", err)
	}
	tok, err := pop.GenerateToken(clientPriv)
	if err != nil {
		t.Fatalf("pop.GenerateToken: %v", err)
	}
	return tok, pop.Jwk.Thumb
}

func TestGenerateAgt_HappyPath(t *testing.T) {
	setTestKey(t)
	resp := generateAgt(Payload{Vin: "VIN123", Context: "Driver+Vehicle+Cloud"})
	if !strings.Contains(resp, `"token"`) {
		t.Fatalf("generateAgt did not return a token: %s", resp)
	}
	if strings.Contains(resp, `"error"`) {
		t.Fatalf("generateAgt returned an error: %s", resp)
	}
}

func TestGenerateLTAgt_HappyPath(t *testing.T) {
	setTestKey(t)
	token, thumb := signedPop(t)
	resp := generateLTAgt(Payload{Vin: "VIN123", Context: "Driver+Vehicle+Cloud", Key: thumb}, token)
	if !strings.Contains(resp, `"token"`) {
		t.Fatalf("generateLTAgt did not return a token: %s", resp)
	}
}

func TestGenerateLTAgt_RepeatedJTI(t *testing.T) {
	setTestKey(t)
	token, thumb := signedPop(t)
	p := Payload{Vin: "VIN123", Context: "Driver+Vehicle+Cloud", Key: thumb}
	if resp := generateLTAgt(p, token); strings.Contains(resp, `"error"`) {
		t.Fatalf("first call should succeed: %s", resp)
	}
	// Replaying the same PoP (same jti) must be rejected.
	resp := generateLTAgt(p, token)
	if !strings.Contains(resp, "Repeated JTI") {
		t.Errorf("replayed PoP not rejected: %s", resp)
	}
}

func TestGenerateLTAgt_InvalidSignature(t *testing.T) {
	setTestKey(t)
	token, thumb := signedPop(t)
	// Corrupt the signature segment (last dot-delimited part).
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		parts[2] = "AAAA" + parts[2]
		token = strings.Join(parts, ".")
	}
	resp := generateLTAgt(Payload{Vin: "VIN123", Context: "Driver+Vehicle+Cloud", Key: thumb}, token)
	if !strings.Contains(resp, "Invalid POP signature") {
		t.Errorf("tampered PoP not rejected for signature: %s", resp)
	}
}

func TestGenerateLTAgt_MalformedPop(t *testing.T) {
	setTestKey(t)
	resp := generateLTAgt(Payload{Vin: "VIN"}, "not-a-jwt")
	if !strings.Contains(resp, "malformed") && !strings.Contains(resp, "error") {
		t.Errorf("malformed PoP not rejected: %s", resp)
	}
}

// TestInitKey_GeneratesWhenMissing covers initKey's generate-and-export branch
// by running it in a temp dir where no key file exists.
func TestInitKey_GeneratesWhenMissing(t *testing.T) {
	prev := privKey
	t.Cleanup(func() { privKey = prev })

	tmp := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	privKey = nil
	initKey()
	if privKey == nil {
		t.Fatal("initKey did not set privKey")
	}
}
