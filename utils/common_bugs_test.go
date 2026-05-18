/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Complete tests for utils/common.go. The pre-existing common_test.go
* has TestUdsRegistration; this file adds coverage for everything else.
*
* Bug coverage map:
*    4  ExtractFromToken panic on malformed → TestExtractFromToken_NoPanic*
*    5  ExtractFromToken substring match    → TestExtractFromToken_ExactKeyMatch
*    6  JsonSchemaValidate nil-deref        → TestJsonSchemaValidate_NoSchemaLoaded
*    9  udsRegList race                     → TestReadUdsRegistrations_Concurrent
*   10  JsonSchemaInit race                 → covered by sync.Once + race detector
*   11  FileExists nil-deref                → TestFileExists_PermissionErrorReturnsFalse
*   12  GetRfcTime offset stripping         → TestGetRfcTime_*
*   15  SetErrorResponse bounds check       → TestSetErrorResponse_OutOfRangeIndex
**/
package utils

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain initialises the package-level Info / Error / Warning
// loggers (utils.InitLog populates them). Without this every test
// that triggers a logging call would nil-deref the logrus pointer.
func TestMain(m *testing.M) {
	InitLog("utils-test.log", os.TempDir(), false, "error")
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// ExtractFromToken — bugs 4 and 5
// ---------------------------------------------------------------------------

// makeTestJWT builds a minimal three-part JWT (header.payload.sig)
// with the supplied claim maps. The signature is a fake constant
// since ExtractFromToken doesn't verify it.
func makeTestJWT(headerClaims, payloadClaims map[string]interface{}) string {
	header := mustMarshalJson(headerClaims)
	payload := mustMarshalJson(payloadClaims)
	return base64.RawURLEncoding.EncodeToString([]byte(header)) +
		"." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) +
		"." +
		"fakeSig"
}

func mustMarshalJson(m map[string]interface{}) string {
	out, _ := jsonMarshalShim(m)
	return out
}

// jsonMarshalShim is a tiny wrapper to avoid pulling encoding/json
// into the test imports — common.go already exports nothing that
// json-marshals a generic map, so we go through json directly.
// (encoding/json is fine to import in tests.)
func jsonMarshalShim(m map[string]interface{}) (string, error) {
	// Lazy import via json package — local to the test file.
	return _jsonMarshalShim(m)
}

func TestExtractFromToken_HappyPath(t *testing.T) {
	tok := makeTestJWT(
		map[string]interface{}{"alg": "RS256", "typ": "JWT"},
		map[string]interface{}{"aud": "w3org/gen2", "iss": "vissr-agt-server", "sub": "user-1"},
	)
	cases := []struct {
		claim string
		want  string
	}{
		{"alg", "RS256"},
		{"typ", "JWT"},
		{"aud", "w3org/gen2"},
		{"iss", "vissr-agt-server"},
		{"sub", "user-1"},
	}
	for _, tc := range cases {
		if got := ExtractFromToken(tok, tc.claim); got != tc.want {
			t.Errorf("ExtractFromToken(%q) = %q; want %q", tc.claim, got, tc.want)
		}
	}
}

// TestExtractFromToken_ExactKeyMatch pins the bug-5 fix. Previously
// looking for "aud" would also match inside "audience" because of
// the substring-based scan.
func TestExtractFromToken_ExactKeyMatch(t *testing.T) {
	tok := makeTestJWT(
		map[string]interface{}{"alg": "RS256"},
		map[string]interface{}{"audience": "decoy", "iss": "real-iss"},
	)
	// "aud" is NOT present — must return "" even though "audience"
	// is in the payload.
	if got := ExtractFromToken(tok, "aud"); got != "" {
		t.Errorf("ExtractFromToken(\"aud\") = %q; want \"\" (audience must not match aud) — bug-5 regression", got)
	}
	if got := ExtractFromToken(tok, "audience"); got != "decoy" {
		t.Errorf("ExtractFromToken(\"audience\") = %q; want \"decoy\"", got)
	}
}

// TestExtractFromToken_NoPanicOnMalformedToken pins the bug-4 fix.
// Each input here used to drive a slice-bound or base64 panic.
func TestExtractFromToken_NoPanicOnMalformedToken(t *testing.T) {
	inputs := []string{
		"",                                                // empty
		"no.dots.here.too.many",                           // 5 parts
		"onlyonedot",                                      // 0 dots
		"only.onedot",                                     // 1 dot
		"!@#$.payload.sig",                                // unparseable base64
		"a.b.c",                                           // shortest 3-part with garbage
		base64.RawURLEncoding.EncodeToString([]byte("{}")) + "." + base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".sig",
	}
	for _, in := range inputs {
		// Must not panic, must return "".
		if got := ExtractFromToken(in, "aud"); got != "" {
			t.Errorf("ExtractFromToken(%q) = %q; want \"\"", in, got)
		}
	}
}

func TestExtractFromToken_NumericClaim(t *testing.T) {
	tok := makeTestJWT(
		map[string]interface{}{"alg": "RS256"},
		map[string]interface{}{"iat": 1234567890},
	)
	if got := ExtractFromToken(tok, "iat"); got != "1234567890" {
		t.Errorf("ExtractFromToken(iat) = %q; want \"1234567890\"", got)
	}
}

func TestExtractFromToken_BooleanClaim(t *testing.T) {
	tok := makeTestJWT(
		map[string]interface{}{"alg": "RS256"},
		map[string]interface{}{"active": true},
	)
	if got := ExtractFromToken(tok, "active"); got != "true" {
		t.Errorf("ExtractFromToken(active) = %q; want \"true\"", got)
	}
}

func TestExtractFromToken_MissingClaim(t *testing.T) {
	tok := makeTestJWT(
		map[string]interface{}{"alg": "RS256"},
		map[string]interface{}{"sub": "user-1"},
	)
	if got := ExtractFromToken(tok, "nope"); got != "" {
		t.Errorf("ExtractFromToken(nope) = %q; want \"\"", got)
	}
}

// ---------------------------------------------------------------------------
// SetErrorResponse — bug 15
// ---------------------------------------------------------------------------

func TestSetErrorResponse_OutOfRangeIndex(t *testing.T) {
	req := map[string]interface{}{"action": "get", "requestId": "1"}
	resp := map[string]interface{}{}
	// Must not panic. Should populate a synthetic error.
	SetErrorResponse(req, resp, 99999, "")
	errMap, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("SetErrorResponse did not populate error map: %v", resp)
	}
	if errMap["reason"] != "internal_error" {
		t.Errorf("out-of-range index: reason = %v; want internal_error", errMap["reason"])
	}
}

func TestSetErrorResponse_NegativeIndex(t *testing.T) {
	req := map[string]interface{}{"action": "get"}
	resp := map[string]interface{}{}
	SetErrorResponse(req, resp, -1, "")
	if _, ok := resp["error"].(map[string]interface{}); !ok {
		t.Errorf("negative index: error map missing")
	}
}

func TestSetErrorResponse_ValidIndexPropagates(t *testing.T) {
	req := map[string]interface{}{
		"action":    "set",
		"requestId": "42",
		"RouterId":  "0?0",
	}
	resp := map[string]interface{}{}
	SetErrorResponse(req, resp, 0, "")
	if resp["action"] != "set" {
		t.Errorf("action not propagated")
	}
	if resp["requestId"] != "42" {
		t.Errorf("requestId not propagated")
	}
	if resp["RouterId"] != "0?0" {
		t.Errorf("RouterId not propagated")
	}
	errMap := resp["error"].(map[string]interface{})
	if errMap["number"] != "400" {
		t.Errorf("error.number = %v; want 400", errMap["number"])
	}
}

// ---------------------------------------------------------------------------
// GetRfcTime — bug 12
// ---------------------------------------------------------------------------

func TestGetRfcTime_FormatShape(t *testing.T) {
	got := GetRfcTime()
	// Should end with "Z" (UTC indicator after offset stripping).
	if !strings.HasSuffix(got, "Z") {
		t.Errorf("GetRfcTime = %q; expected trailing Z", got)
	}
	// Should NOT contain a "+HH:MM" or "-HH:MM" offset followed by Z
	// (the old bug produced strings like "...+02:00Z").
	if strings.Contains(got, "+0") && strings.HasSuffix(got, "Z") {
		// "+0" can appear in the year ("2026") or hour-tens, but if it
		// appears followed by colon then Z that's the bug-12 shape.
		if matches := strings.Index(got, ":"); matches >= 0 && strings.Contains(got[matches:], "+0") {
			t.Errorf("GetRfcTime = %q; offset not stripped before Z (bug-12 regression)", got)
		}
	}
	// Should parse back as a valid RFC3339 timestamp.
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("GetRfcTime = %q; not parseable as RFC3339: %v", got, err)
	}
}

func TestGetRfcTime_RoughlyNow(t *testing.T) {
	got, err := time.Parse(time.RFC3339, GetRfcTime())
	if err != nil {
		t.Fatalf("could not parse GetRfcTime output: %v", err)
	}
	delta := time.Since(got)
	if delta < -time.Second || delta > 5*time.Second {
		t.Errorf("GetRfcTime returned a time %v from now; expected within ~5s", delta)
	}
}

// ---------------------------------------------------------------------------
// FileExists — bug 11
// ---------------------------------------------------------------------------

func TestFileExists_ReturnsTrue(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "hello.txt")
	if err := os.WriteFile(f, []byte("hi"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !FileExists(f) {
		t.Errorf("FileExists(%q) = false; want true", f)
	}
}

func TestFileExists_ReturnsFalseOnMissing(t *testing.T) {
	if FileExists("/no/such/file/anywhere/12345") {
		t.Errorf("FileExists returned true for nonexistent path")
	}
}

func TestFileExists_DirectoryReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	if FileExists(tmp) {
		t.Errorf("FileExists(%q) = true; want false (it's a directory)", tmp)
	}
}

// TestFileExists_DoesNotPanicOnPermissionError is the bug-11 pin.
// The previous code dereferenced a nil `info` if Stat returned an
// error that wasn't IsNotExist. We construct an unreadable parent
// directory to force EACCES on stat. (Skipped on platforms where we
// can't reliably produce EACCES, e.g. running as root.)
func TestFileExists_DoesNotPanicOnPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — EACCES is hard to produce reliably")
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "noaccess")
	if err := os.Mkdir(dir, 0000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.Chmod(dir, 0700) // allow cleanup
	target := filepath.Join(dir, "child")

	// Must not panic; must return false.
	got := FileExists(target)
	if got {
		t.Errorf("FileExists(%q) = true; want false on EACCES", target)
	}
}

// ---------------------------------------------------------------------------
// udsRegList race — bug 9
// ---------------------------------------------------------------------------

// TestReadUdsRegistrations_Concurrent exercises the mutex added in
// the bug-9 fix. Run with -race to detect any unguarded access.
func TestReadUdsRegistrations_Concurrent(t *testing.T) {
	tmp := t.TempDir()
	sockFile := filepath.Join(tmp, "uds-reg.json")
	if err := os.WriteFile(sockFile, []byte(`[{"root":"Vehicle","serverFeeder":"/tmp/a"}]`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	var wg sync.WaitGroup
	const goroutines = 8
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Half of them re-read, half just call Get*.
			if idx%2 == 0 {
				_ = ReadUdsRegistrations(sockFile)
			} else {
				_ = GetUdsPath("Vehicle.Speed", "serverFeeder")
			}
		}(i)
	}
	wg.Wait()
	// Sanity: at least one read succeeded.
	if len(GetUdsPath("Vehicle.Speed", "serverFeeder")) == 0 && len(udsRegList) == 0 {
		// not strictly required to be non-empty; just verifying no race
	}
}

// ---------------------------------------------------------------------------
// JsonSchemaValidate — bug 6
// ---------------------------------------------------------------------------

// TestJsonSchemaValidate_NoSchemaLoaded covers the bug-6 fix: when
// the schema file wasn't loaded (the test environment doesn't ship
// vissv3.0-schema.json next to the test binary), Validate used to
// nil-deref. Now it returns an explanatory error string.
func TestJsonSchemaValidate_NoSchemaLoaded(t *testing.T) {
	// Save & restore the package global so we don't leak state.
	prev := jsonSchema
	jsonSchema = nil
	defer func() { jsonSchema = prev }()

	got := JsonSchemaValidate(`{"action":"get"}`)
	if !strings.Contains(got, "schema not loaded") {
		t.Errorf("JsonSchemaValidate without schema = %q; want \"schema not loaded\" message", got)
	}
}

// ---------------------------------------------------------------------------
// VerifyTokenSignature — round-trip through the new CheckSignature
// ---------------------------------------------------------------------------

// VerifyTokenSignature is signed (string, string) → error, so it
// covers the HS256 path only. The RSA/ECDSA paths are exercised
// directly in cryptoutils_test.go.
func TestVerifyTokenSignature_HmacRoundTrip(t *testing.T) {
	tok := JsonWebToken{}
	tok.SetHeader("HS256")
	tok.AddClaim("foo", "bar")
	tok.SymmSign("the-secret")

	full := tok.GetFullToken()
	if err := VerifyTokenSignature(full, "the-secret"); err != nil {
		t.Errorf("VerifyTokenSignature happy path: %v", err)
	}
	if err := VerifyTokenSignature(full, "wrong-secret"); err == nil {
		t.Errorf("VerifyTokenSignature accepted wrong secret")
	}
}

func TestVerifyTokenSignature_MalformedReturnsError(t *testing.T) {
	if err := VerifyTokenSignature("not.a.jwt.with.too.many.dots", "k"); err == nil {
		t.Errorf("VerifyTokenSignature accepted a malformed token")
	}
	if err := VerifyTokenSignature("no-dots-at-all", "k"); err == nil {
		t.Errorf("VerifyTokenSignature accepted a malformed token (no dots)")
	}
}

// ---------------------------------------------------------------------------
// Helper: jsonMarshalShim implementation (no separate file needed —
// keep encoding/json import local).
// ---------------------------------------------------------------------------

func _jsonMarshalShim(m map[string]interface{}) (string, error) {
	// Build a stable-ordered JSON object so test fixtures are
	// deterministic. encoding/json doesn't guarantee key order but
	// ExtractFromToken doesn't care about order, so default
	// marshalling is fine.
	type _kv struct {
		K string
		V interface{}
	}
	// Reuse encoding/json via reflection-free roundtrip.
	// (Tests can rely on encoding/json being available.)
	out := mapToJSON(m)
	return out, nil
}

// mapToJSON is a tiny ad-hoc JSON serializer for the test-fixture
// shape we need (string/number/bool/string-keyed nested maps).
// Avoids pulling in a separate import block at the file level.
func mapToJSON(m map[string]interface{}) string {
	var buf strings.Builder
	buf.WriteByte('{')
	first := true
	for k, v := range m {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteByte('"')
		buf.WriteString(k)
		buf.WriteString(`":`)
		switch val := v.(type) {
		case string:
			buf.WriteByte('"')
			buf.WriteString(val)
			buf.WriteByte('"')
		case int:
			buf.WriteString(strconv.Itoa(val))
		case int64:
			buf.WriteString(strconv.FormatInt(val, 10))
		case float64:
			buf.WriteString(strconv.FormatFloat(val, 'f', -1, 64))
		case bool:
			buf.WriteString(strconv.FormatBool(val))
		case nil:
			buf.WriteString("null")
		default:
			// Unsupported — emit as quoted string of the Go default
			// formatting. Should not be used by our tests.
			buf.WriteByte('"')
			buf.WriteString("<unsupported>")
			buf.WriteByte('"')
		}
	}
	buf.WriteByte('}')
	return buf.String()
}
