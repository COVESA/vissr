/**
* (C) 2026 Matt Jones / Ford
*
* Tests for the TLS code paths in managerhandlers.go (ReadTransportSecConfig,
* validateSecConfig, CertOptToInt, GetTLSConfig, safeCertPath) and the
* SecConfig.ServerName addition in managerdata.go. Race-mode clean.
**/

package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func init() {
	InitLog("tls_test-log.txt", "./logs", false, "info")
}

// --------------------------------------------------------------------------
// CertOptToInt — pre-fix was case-sensitive; unknown values silently used
// max security. Now normalises case and warns on unknown values.
// --------------------------------------------------------------------------

func TestCertOptToInt_KnownValues(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"NoClientCert", int(tls.NoClientCert)},
		{"ClientCertNoVerification", int(tls.RequireAnyClientCert)},
		{"ClientCertVerification", int(tls.RequireAndVerifyClientCert)},
	}
	for _, c := range cases {
		if got := CertOptToInt(c.in); got != c.want {
			t.Errorf("CertOptToInt(%q): got %d, want %d", c.in, got, c.want)
		}
	}
}

func TestCertOptToInt_CaseInsensitive(t *testing.T) {
	if got := CertOptToInt("noclientcert"); got != int(tls.NoClientCert) {
		t.Errorf("lowercase: got %d", got)
	}
	if got := CertOptToInt("NOCLIENTCERT"); got != int(tls.NoClientCert) {
		t.Errorf("uppercase: got %d", got)
	}
	if got := CertOptToInt("  ClientCertVerification  "); got != int(tls.RequireAndVerifyClientCert) {
		t.Errorf("whitespace: got %d", got)
	}
}

func TestCertOptToInt_EmptyDefaultsToMaxSecurity(t *testing.T) {
	if got := CertOptToInt(""); got != int(tls.RequireAndVerifyClientCert) {
		t.Errorf("empty: got %d; want max security", got)
	}
}

func TestCertOptToInt_UnknownDefaultsToMaxSecurity(t *testing.T) {
	// Pre-fix this silently fell through. Now warns + still defaults max.
	if got := CertOptToInt("unknown-option-xyz"); got != int(tls.RequireAndVerifyClientCert) {
		t.Errorf("unknown: got %d; want max security", got)
	}
}

// --------------------------------------------------------------------------
// safeCertPath — pre-fix the call sites did raw string concat, allowing
// path traversal via "../../etc/" in operator-supplied CaSecPath.
// --------------------------------------------------------------------------

func TestSafeCertPath_HappyPath(t *testing.T) {
	got, err := safeCertPath("/tmp/certs", "ca/", "Root.CA.crt")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := filepath.Clean("/tmp/certs/ca/Root.CA.crt")
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestSafeCertPath_EmptySub(t *testing.T) {
	got, err := safeCertPath("/tmp/certs", "", "Root.CA.crt")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != filepath.Clean("/tmp/certs/Root.CA.crt") {
		t.Errorf("got %q", got)
	}
}

func TestSafeCertPath_TraversalInSub(t *testing.T) {
	if _, err := safeCertPath("/tmp/certs", "../../etc", "passwd"); err == nil {
		t.Errorf("expected traversal rejection")
	}
}

func TestSafeCertPath_TraversalInFilename(t *testing.T) {
	if _, err := safeCertPath("/tmp/certs", "ca", "../../etc/passwd"); err == nil {
		t.Errorf("expected rejection of path separators in filename")
	}
}

func TestSafeCertPath_AbsoluteSubDoesNotEscapeBase(t *testing.T) {
	// Go's filepath.Join strips the leading slash on a middle component:
	// Join("/tmp/certs","/etc","passwd") -> /tmp/certs/etc/passwd. The
	// result stays inside the base so safeCertPath legitimately accepts it.
	// Verifying this here so the assumption is documented + protected
	// against future filepath.Join semantic changes.
	got, err := safeCertPath("/tmp/certs", "/etc", "passwd")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := filepath.Clean("/tmp/certs/etc/passwd")
	if got != want {
		t.Errorf("got %q; want %q (absolute sub should not escape base)", got, want)
	}
}

func TestSafeCertPath_ExplicitTraversalStillRejected(t *testing.T) {
	// The substantive traversal-rejection case: any ".." segment that
	// resolves outside the base must be rejected, regardless of whether
	// it's expressed via the sub or via stacked-up parents.
	if _, err := safeCertPath("/tmp/certs", "ca/../../etc", "passwd"); err == nil {
		t.Errorf("expected rejection on chained ../../ traversal")
	}
}

func TestSafeCertPath_EmptyBase(t *testing.T) {
	if _, err := safeCertPath("", "ca", "Root.CA.crt"); err == nil {
		t.Errorf("expected error for empty base")
	}
}

func TestSafeCertPath_EmptyFilename(t *testing.T) {
	if _, err := safeCertPath("/tmp/certs", "ca", ""); err == nil {
		t.Errorf("expected error for empty filename")
	}
}

// --------------------------------------------------------------------------
// validateSecConfig — applies defaults + warnings.
// --------------------------------------------------------------------------

func TestValidateSecConfig_NoopOnPlaintext(t *testing.T) {
	cfg := &SecConfig{TransportSec: "no", ServerName: ""}
	validateSecConfig(cfg)
	// Should not have auto-defaulted ServerName since TLS is off.
	if cfg.ServerName != "" {
		t.Errorf("plaintext should leave ServerName untouched; got %q", cfg.ServerName)
	}
}

func TestValidateSecConfig_CoercesUnknownTransportSec(t *testing.T) {
	cfg := &SecConfig{TransportSec: "maybe"}
	validateSecConfig(cfg)
	if cfg.TransportSec != "no" {
		t.Errorf("got %q; want \"no\"", cfg.TransportSec)
	}
}

func TestValidateSecConfig_DefaultsServerNameWithWarning(t *testing.T) {
	// The warning goes to the log; we can't easily assert on it without
	// hooking the logger. We assert the default is applied.
	cfg := &SecConfig{TransportSec: "yes", ServerName: ""}
	validateSecConfig(cfg)
	if cfg.ServerName != "localhost" {
		t.Errorf("expected default \"localhost\"; got %q", cfg.ServerName)
	}
}

func TestValidateSecConfig_PreservesOperatorServerName(t *testing.T) {
	cfg := &SecConfig{TransportSec: "yes", ServerName: "viss.example.com"}
	validateSecConfig(cfg)
	if cfg.ServerName != "viss.example.com" {
		t.Errorf("operator value overwritten: got %q", cfg.ServerName)
	}
}

func TestValidateSecConfig_DefaultsServerCertOpt(t *testing.T) {
	cfg := &SecConfig{TransportSec: "yes", ServerName: "x", ServerCertOpt: ""}
	validateSecConfig(cfg)
	if cfg.ServerCertOpt != "ClientCertVerification" {
		t.Errorf("expected default max-security; got %q", cfg.ServerCertOpt)
	}
}

func TestValidateSecConfig_NilSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on nil: %v", r)
		}
	}()
	validateSecConfig(nil)
}

// --------------------------------------------------------------------------
// ReadTransportSecConfig — sync.Once race safety + happy/malformed/missing.
// --------------------------------------------------------------------------

// withTempTrSecConfigPath temporarily redirects TrSecConfigPath to a writable
// tempdir and resets secureConfigOnce so the test can drive it.
func withTempTrSecConfigPath(t *testing.T) (dir string, restore func()) {
	t.Helper()
	dir = t.TempDir() + string(filepath.Separator)
	savedPath := TrSecConfigPath
	savedCfg := SecureConfiguration
	TrSecConfigPath = dir
	SecureConfiguration = SecConfig{}
	secureConfigOnce = sync.Once{}
	restore = func() {
		TrSecConfigPath = savedPath
		SecureConfiguration = savedCfg
		secureConfigOnce = sync.Once{} // reset to clean state after test
	}
	return
}

func TestReadTransportSecConfig_MissingFile(t *testing.T) {
	_, restore := withTempTrSecConfigPath(t)
	defer restore()
	ReadTransportSecConfig()
	if SecureConfiguration.TransportSec != "no" {
		t.Errorf("missing file should fall back to \"no\"; got %q", SecureConfiguration.TransportSec)
	}
}

func TestReadTransportSecConfig_HappyPath(t *testing.T) {
	dir, restore := withTempTrSecConfigPath(t)
	defer restore()
	payload := map[string]string{
		"transportSec":  "yes",
		"httpSecPort":   "443",
		"wsSecPort":     "6443",
		"caSecPath":     "ca/",
		"serverSecPath": "server/",
		"serverCertOpt": "ClientCertVerification",
		"serverName":    "viss.example.com",
	}
	b, _ := json.Marshal(payload)
	if err := os.WriteFile(dir+"transportSec.json", b, 0644); err != nil {
		t.Fatal(err)
	}
	ReadTransportSecConfig()
	if SecureConfiguration.TransportSec != "yes" {
		t.Errorf("TransportSec: got %q", SecureConfiguration.TransportSec)
	}
	if SecureConfiguration.ServerName != "viss.example.com" {
		t.Errorf("ServerName: got %q", SecureConfiguration.ServerName)
	}
	if SecureConfiguration.HttpSecPort != "443" {
		t.Errorf("HttpSecPort: got %q", SecureConfiguration.HttpSecPort)
	}
}

func TestReadTransportSecConfig_MalformedJSON(t *testing.T) {
	dir, restore := withTempTrSecConfigPath(t)
	defer restore()
	if err := os.WriteFile(dir+"transportSec.json", []byte("not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	ReadTransportSecConfig()
	if SecureConfiguration.TransportSec != "no" {
		t.Errorf("malformed JSON should fall back to \"no\"; got %q", SecureConfiguration.TransportSec)
	}
}

func TestReadTransportSecConfig_EmptyJSONCoercesToNo(t *testing.T) {
	dir, restore := withTempTrSecConfigPath(t)
	defer restore()
	os.WriteFile(dir+"transportSec.json", []byte("{}"), 0644)
	ReadTransportSecConfig()
	if SecureConfiguration.TransportSec != "no" {
		t.Errorf("empty {} should coerce to \"no\"; got %q", SecureConfiguration.TransportSec)
	}
}

func TestReadTransportSecConfig_TLSConfigDefaultsServerName(t *testing.T) {
	dir, restore := withTempTrSecConfigPath(t)
	defer restore()
	// transportSec=yes but no serverName -> defaults to localhost with warning.
	os.WriteFile(dir+"transportSec.json", []byte(`{"transportSec":"yes","caSecPath":"ca/","serverSecPath":"server/","serverCertOpt":"ClientCertVerification"}`), 0644)
	ReadTransportSecConfig()
	if SecureConfiguration.ServerName != "localhost" {
		t.Errorf("expected default ServerName=localhost; got %q", SecureConfiguration.ServerName)
	}
}

func TestReadTransportSecConfig_OnceProtection(t *testing.T) {
	// After a successful load, additional calls should NOT re-read the file.
	// Verify by mutating SecureConfiguration in between and observing it
	// survives the second call.
	dir, restore := withTempTrSecConfigPath(t)
	defer restore()
	os.WriteFile(dir+"transportSec.json", []byte(`{"transportSec":"yes","serverName":"once.example.com","caSecPath":"ca/","serverSecPath":"server/","serverCertOpt":"ClientCertVerification"}`), 0644)
	ReadTransportSecConfig()
	if SecureConfiguration.ServerName != "once.example.com" {
		t.Fatalf("first read: got %q", SecureConfiguration.ServerName)
	}
	// Overwrite the file with different contents - sync.Once should ignore it.
	os.WriteFile(dir+"transportSec.json", []byte(`{"transportSec":"yes","serverName":"DIFFERENT.example.com"}`), 0644)
	ReadTransportSecConfig()
	if SecureConfiguration.ServerName != "once.example.com" {
		t.Errorf("sync.Once should have suppressed second read; got %q", SecureConfiguration.ServerName)
	}
}

func TestReadTransportSecConfig_RaceSafe(t *testing.T) {
	dir, restore := withTempTrSecConfigPath(t)
	defer restore()
	os.WriteFile(dir+"transportSec.json", []byte(`{"transportSec":"yes","serverName":"race.example.com","caSecPath":"ca/","serverSecPath":"server/","serverCertOpt":"ClientCertVerification"}`), 0644)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ReadTransportSecConfig()
			_ = SecureConfiguration.ServerName // also exercises the read path
		}()
	}
	wg.Wait()
	if SecureConfiguration.ServerName != "race.example.com" {
		t.Errorf("got %q", SecureConfiguration.ServerName)
	}
}

// --------------------------------------------------------------------------
// GetTLSConfig — happy path with no client-cert required (no CA file needed);
// the CA-file error paths use Error.Fatalf which calls os.Exit and so can't
// be unit-tested cleanly.
// --------------------------------------------------------------------------

func TestGetTLSConfig_NoClientCert_NoCAFile(t *testing.T) {
	// certOpt = NoClientCert (0). Per the function contract no CA file is
	// touched; caCertFile can be "" and the call must still succeed.
	cfg := GetTLSConfig("viss.example.com", "", tls.NoClientCert, nil)
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
	if cfg.ServerName != "viss.example.com" {
		t.Errorf("ServerName: got %q", cfg.ServerName)
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Errorf("ClientAuth: got %v", cfg.ClientAuth)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion: got %x; want TLS 1.2", cfg.MinVersion)
	}
}

func TestGetTLSConfig_NoClientCert_WithServerCert(t *testing.T) {
	serverCert := &tls.Certificate{} // empty cert struct - just exercises the branch
	cfg := GetTLSConfig("v.example.com", "", tls.NoClientCert, serverCert)
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("expected 1 cert; got %d", len(cfg.Certificates))
	}
}

func TestGetTLSConfig_EmptyHostWarns(t *testing.T) {
	// We can't easily assert on the warning - just verify the call doesn't
	// panic and we still get a config back.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	cfg := GetTLSConfig("", "", tls.NoClientCert, nil)
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
	if cfg.ServerName != "" {
		t.Errorf("got %q", cfg.ServerName)
	}
}

// --------------------------------------------------------------------------
// GetTLSConfig — fatal paths (subprocess pattern). Pre-fix bugs:
//   - empty cert pool was silently used when AppendCertsFromPEM failed
//   - nil returned on file-read error -> caller used default TLS config
// Both fixed by Error.Fatalf which calls os.Exit(1). To unit-test that, we
// re-exec the test binary with a sentinel env var; the subprocess runs the
// fatal path and exits; the parent asserts the non-zero exit.
// --------------------------------------------------------------------------

func TestGetTLSConfig_FatalsOnMissingCAFile(t *testing.T) {
	if os.Getenv("VISSR_TLS_TEST_FATAL_MISSING") == "1" {
		// Subprocess body. RequireAndVerifyClientCert (4) > tls.RequestClientCert
		// so the CA file IS required. /no/such/path doesn't exist - GetTLSConfig
		// must Error.Fatalf, which calls os.Exit(1).
		InitLog("tls_test-log.txt", "./logs", false, "info")
		GetTLSConfig("x", "/no/such/cert-file-please", tls.RequireAndVerifyClientCert, nil)
		// If we reach here, the bug is back: GetTLSConfig returned nil
		// instead of fatal-exiting.
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestGetTLSConfig_FatalsOnMissingCAFile")
	cmd.Env = append(os.Environ(), "VISSR_TLS_TEST_FATAL_MISSING=1")
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok && !ee.Success() {
		return // expected: subprocess exited non-zero
	}
	t.Fatalf("expected subprocess to fatal-exit on missing CA file; got err=%v", err)
}

func TestGetTLSConfig_FatalsOnEmptyPEM(t *testing.T) {
	if os.Getenv("VISSR_TLS_TEST_FATAL_EMPTY") == "1" {
		InitLog("tls_test-log.txt", "./logs", false, "info")
		// File exists but has no PEM blocks - AppendCertsFromPEM returns false.
		dir, err := os.MkdirTemp("", "tls_test_empty_pem_*")
		if err != nil {
			os.Exit(2)
		}
		fp := filepath.Join(dir, "Root.CA.crt")
		if err := os.WriteFile(fp, []byte("this is not a PEM"), 0644); err != nil {
			os.Exit(2)
		}
		GetTLSConfig("x", fp, tls.RequireAndVerifyClientCert, nil)
		os.Exit(0) // unreachable if fix works
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestGetTLSConfig_FatalsOnEmptyPEM")
	cmd.Env = append(os.Environ(), "VISSR_TLS_TEST_FATAL_EMPTY=1")
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok && !ee.Success() {
		return
	}
	t.Fatalf("expected subprocess to fatal-exit on empty PEM; got err=%v", err)
}

// --------------------------------------------------------------------------
// GetTLSConfig — happy path with a real CA cert. Verifies AppendCertsFromPEM
// actually populates ClientCAs (regression on the bool-return fix).
// --------------------------------------------------------------------------

// writeSelfSignedCA generates a fresh self-signed CA, writes it as PEM to dir,
// and returns the file path.
func writeSelfSignedCA(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "vissr-tls-test-CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	fp := filepath.Join(dir, "Root.CA.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(fp, pemBytes, 0644); err != nil {
		t.Fatal(err)
	}
	return fp
}

func TestGetTLSConfig_LoadsValidCAFile(t *testing.T) {
	dir := t.TempDir()
	caFile := writeSelfSignedCA(t, dir)
	cfg := GetTLSConfig("viss.example.com", caFile, tls.RequireAndVerifyClientCert, nil)
	if cfg == nil {
		t.Fatal("expected non-nil cfg")
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs should be populated when ClientAuth requires verification")
	}
	subjects := cfg.ClientCAs.Subjects() //nolint:staticcheck // SA1019 - usage is intentional for tests
	if len(subjects) == 0 {
		t.Errorf("ClientCAs pool empty - AppendCertsFromPEM regressed?")
	}
	if cfg.ServerName != "viss.example.com" {
		t.Errorf("ServerName: got %q", cfg.ServerName)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion: got %x", cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth: got %v", cfg.ClientAuth)
	}
}

// --------------------------------------------------------------------------
// Integration-only entry points
//
// HttpServer.InitClientServer and WsServer.InitClientServer are the top-level
// listener bootstraps. They bind the configured port (or :8888 / :8080 in
// plaintext mode), then call Error.Fatal(s.ListenAndServeTLS(...)) /
// Error.Fatal(s.ListenAndServe(...)). Those entry points block forever in
// production and os.Exit on any error, so they can't be unit-tested without
// driving a real TCP listener (and that lives in the integration suite, not
// here). Their building blocks - GetTLSConfig, CertOptToInt, safeCertPath,
// ReadTransportSecConfig, validateSecConfig - are all covered above.
// --------------------------------------------------------------------------

// --------------------------------------------------------------------------
// SecConfig: verify the new ServerName field round-trips through JSON.
// --------------------------------------------------------------------------

func TestSecConfig_ServerNameJSONTag(t *testing.T) {
	in := SecConfig{
		TransportSec: "yes",
		ServerName:   "x.example.com",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out SecConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ServerName != "x.example.com" {
		t.Errorf("ServerName round-trip: got %q", out.ServerName)
	}
}
