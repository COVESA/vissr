/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the atServer protocol-hardening fixes (PR D — the three
* deferred architectural items from PR C / #136):
*
*   - Fix 3: aud/iss validation on ATs
*       (validated implicitly via tokenValidationResponse + generateAt;
*        the directly-testable helpers cover the new validation paths)
*
*   - Fix 7: HMAC authentication on ECF consent messages
*       - computeEcfHmac / verifyEcfHmac
*       - consentReplyResponse rejects bad / missing HMAC under set secret
*       - consentReplyResponse accepts in compat mode when secret unset
*       - consentCancelResponse mirror
*
*   - Fix 8: TLS + origin allow-list for the ECF websocket
*       - checkEcfOrigin against a configured allow-list
**/
package atServer

import (
	"net/http"
	"strings"
	"testing"
)

// withEcfSecret swaps in a test secret for the duration of fn and
// restores the prior value on return. The package var was set by
// init() to whatever VISSR_ECF_SECRET was at test start; we don't
// want tests leaking state into each other.
func withEcfSecret(secret string, fn func()) {
	prev := ecfSecret
	ecfSecret = secret
	defer func() { ecfSecret = prev }()
	fn()
}

func withEcfAllowedOrigins(origins []string, fn func()) {
	prev := ecfAllowedOrigins
	ecfAllowedOrigins = origins
	defer func() { ecfAllowedOrigins = prev }()
	fn()
}

// TestVerifyEcfHmac_RoundTrip pins the basic contract: a HMAC
// computed by computeEcfHmac is accepted by verifyEcfHmac with the
// same inputs, rejected with any tampered input.
func TestVerifyEcfHmac_RoundTrip(t *testing.T) {
	withEcfSecret("test-secret", func() {
		good := computeEcfHmac("consent-reply", "42", "YES")
		if !verifyEcfHmac("consent-reply", "42", "YES", good) {
			t.Errorf("verifyEcfHmac rejected its own output")
		}
		// Tampered action.
		if verifyEcfHmac("consent-cancel", "42", "YES", good) {
			t.Errorf("verifyEcfHmac accepted a HMAC for a different action")
		}
		// Tampered messageId.
		if verifyEcfHmac("consent-reply", "43", "YES", good) {
			t.Errorf("verifyEcfHmac accepted a HMAC for a different messageId")
		}
		// Tampered consent.
		if verifyEcfHmac("consent-reply", "42", "NO", good) {
			t.Errorf("verifyEcfHmac accepted a HMAC for a different consent value")
		}
		// Wrong HMAC outright.
		if verifyEcfHmac("consent-reply", "42", "YES", "deadbeef") {
			t.Errorf("verifyEcfHmac accepted a clearly wrong HMAC")
		}
	})
}

// TestVerifyEcfHmac_CompatModeAcceptsAll covers the backward-compat
// branch: when VISSR_ECF_SECRET is unset (ecfSecret == ""), every
// HMAC verification returns true. The startup warning was the
// one-time notification; per-request logging would be too noisy.
func TestVerifyEcfHmac_CompatModeAcceptsAll(t *testing.T) {
	withEcfSecret("", func() {
		if !verifyEcfHmac("consent-reply", "42", "YES", "") {
			t.Errorf("compat mode rejected empty HMAC; should accept")
		}
		if !verifyEcfHmac("consent-reply", "42", "YES", "garbage") {
			t.Errorf("compat mode rejected garbage HMAC; should accept")
		}
	})
}

// TestConsentReplyResponse_RejectsBadHmacWhenSecretSet pins the
// security property: with VISSR_ECF_SECRET set, a consent-reply
// message without a valid HMAC must be rejected.
func TestConsentReplyResponse_RejectsBadHmacWhenSecretSet(t *testing.T) {
	withEcfSecret("test-secret", func() {
		// No hmac field at all.
		resp := consentReplyResponse(`{"action":"consent-reply","messageId":"42","consent":"YES"}`)
		if !strings.Contains(resp, "401") {
			t.Errorf("expected 401 without HMAC; got %q", resp)
		}
		// Wrong hmac.
		resp = consentReplyResponse(`{"action":"consent-reply","messageId":"42","consent":"YES","hmac":"deadbeef"}`)
		if !strings.Contains(resp, "401") {
			t.Errorf("expected 401 with wrong HMAC; got %q", resp)
		}
	})
}

// TestConsentReplyResponse_AcceptsValidHmac confirms a valid HMAC
// gets past the auth check (the response will be 404-Not found
// because the messageId isn't in pendingList, but the path beyond
// auth is what we're verifying).
func TestConsentReplyResponse_AcceptsValidHmac(t *testing.T) {
	withEcfSecret("test-secret", func() {
		validHmac := computeEcfHmac("consent-reply", "42", "YES")
		body := `{"action":"consent-reply","messageId":"42","consent":"YES","hmac":"` + validHmac + `"}`
		resp := consentReplyResponse(body)
		if strings.Contains(resp, "401-Unauthorized") {
			t.Errorf("valid HMAC was rejected as 401-Unauthorized: %q", resp)
		}
	})
}

// TestConsentReplyResponse_CompatModeAcceptsWithoutHmac pins the
// backward-compat property: when VISSR_ECF_SECRET is unset, messages
// without an `hmac` field are still processed (the deployment runs
// with a startup warning; we don't want to break existing ECFs).
func TestConsentReplyResponse_CompatModeAcceptsWithoutHmac(t *testing.T) {
	withEcfSecret("", func() {
		resp := consentReplyResponse(`{"action":"consent-reply","messageId":"42","consent":"YES"}`)
		if strings.Contains(resp, "401-Unauthorized") {
			t.Errorf("compat mode rejected unsigned message: %q", resp)
		}
	})
}

// TestConsentCancelResponse_RejectsBadHmacWhenSecretSet mirrors the
// reply test for the cancel handler. Cancel has no consent field; the
// canonical signing string is "consent-cancel|<messageId>|".
func TestConsentCancelResponse_RejectsBadHmacWhenSecretSet(t *testing.T) {
	withEcfSecret("test-secret", func() {
		ch := make(chan string, 1)
		resp := consentCancelResponse(`{"action":"consent-cancel","messageId":"42"}`, ch)
		if !strings.Contains(resp, "401") {
			t.Errorf("expected 401 without HMAC; got %q", resp)
		}
		resp = consentCancelResponse(`{"action":"consent-cancel","messageId":"42","hmac":"deadbeef"}`, ch)
		if !strings.Contains(resp, "401") {
			t.Errorf("expected 401 with wrong HMAC; got %q", resp)
		}
	})
}

// TestConsentCancelResponse_AcceptsValidHmac mirrors the reply test.
func TestConsentCancelResponse_AcceptsValidHmac(t *testing.T) {
	withEcfSecret("test-secret", func() {
		validHmac := computeEcfHmac("consent-cancel", "42", "")
		body := `{"action":"consent-cancel","messageId":"42","hmac":"` + validHmac + `"}`
		ch := make(chan string, 1)
		resp := consentCancelResponse(body, ch)
		if strings.Contains(resp, "401-Unauthorized") {
			t.Errorf("valid HMAC was rejected as 401-Unauthorized: %q", resp)
		}
	})
}

// TestCheckEcfOrigin_AllowList pins the bug-8 origin allow-list
// behaviour. With the list configured, only listed origins pass;
// without it, any origin passes (compat mode).
func TestCheckEcfOrigin_AllowList(t *testing.T) {
	// With an allow-list configured.
	withEcfAllowedOrigins([]string{"https://ecf.example.com", "https://ecf-internal.example.com"}, func() {
		cases := []struct {
			origin string
			want   bool
		}{
			{"https://ecf.example.com", true},
			{"https://ecf-internal.example.com", true},
			{"https://attacker.example.com", false},
			{"", false},
			{"http://ecf.example.com", false}, // scheme mismatch
		}
		for _, tc := range cases {
			req := newRequestWithOrigin(tc.origin)
			if got := checkEcfOrigin(req); got != tc.want {
				t.Errorf("checkEcfOrigin(origin=%q) = %v; want %v", tc.origin, got, tc.want)
			}
		}
	})

	// Without an allow-list (compat mode): any origin accepted.
	withEcfAllowedOrigins(nil, func() {
		if !checkEcfOrigin(newRequestWithOrigin("https://anywhere.example.com")) {
			t.Errorf("compat mode rejected an origin; should accept any")
		}
		if !checkEcfOrigin(newRequestWithOrigin("")) {
			t.Errorf("compat mode rejected an empty origin; should accept")
		}
	})
}

// newRequestWithOrigin returns a minimal *http.Request with the
// given Origin header set.
func newRequestWithOrigin(origin string) *http.Request {
	req, _ := http.NewRequest("GET", "/", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}
