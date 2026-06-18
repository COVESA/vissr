package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func testDoc(version string, pkgs []Package) Document {
	return Document{
		SpecVersion:  version,
		DocumentName: "test-doc",
		SPDXID:       "SPDXRef-DOCUMENT",
		Namespace:    "https://example.com/spdx/test",
		Created:      "2026-06-08T00:00:00Z",
		CreatorTool:  "vissr/tools/sbom",
		CreatorOrg:   "COVESA",
		Packages:     pkgs,
	}
}

func testPkgs() []Package {
	return []Package{
		{Name: "example.com/foo", Version: "v1.2.3", SPDXID: "SPDXRef-Package-1", DownloadURL: "https://pkg.go.dev/example.com/foo@v1.2.3"},
		{Name: "example.com/bar", Version: "v0.1.0", SPDXID: "SPDXRef-Package-2", DownloadURL: "https://pkg.go.dev/example.com/bar@v0.1.0"},
	}
}

// ── noassertion ──────────────────────────────────────────────────────────────

func TestNoassertion_Empty(t *testing.T) {
	if got := noassertion(""); got != "NOASSERTION" {
		t.Fatalf("got %q", got)
	}
}

func TestNoassertion_NonEmpty(t *testing.T) {
	if got := noassertion("Apache-2.0"); got != "Apache-2.0" {
		t.Fatalf("got %q", got)
	}
}

// ── downloadURL ──────────────────────────────────────────────────────────────

func TestDownloadURL_WithVersion(t *testing.T) {
	got := downloadURL("github.com/foo/bar", "v1.0.0")
	want := "https://pkg.go.dev/github.com/foo/bar@v1.0.0"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDownloadURL_NoVersion(t *testing.T) {
	if got := downloadURL("github.com/foo/bar", ""); got != "NOASSERTION" {
		t.Fatalf("got %q", got)
	}
}

// ── timestamp ─────────────────────────────────────────────────────────────────

func TestTimestamp_Format(t *testing.T) {
	ts := timestamp()
	if _, err := time.Parse("20060102T150405Z", ts); err != nil {
		t.Fatalf("timestamp %q does not parse: %v", ts, err)
	}
}

// ── packageVerificationCode ──────────────────────────────────────────────────

func TestPackageVerificationCode_Deterministic(t *testing.T) {
	pkg := Package{Name: "example.com/foo", Version: "v1.0.0"}
	a := packageVerificationCode(pkg)
	b := packageVerificationCode(pkg)
	if a != b {
		t.Fatalf("non-deterministic: %q != %q", a, b)
	}
}

func TestPackageVerificationCode_Hex64(t *testing.T) {
	pkg := Package{Name: "example.com/foo", Version: "v1.0.0"}
	code := packageVerificationCode(pkg)
	if len(code) != 64 {
		t.Fatalf("expected 64-char hex, got len=%d: %q", len(code), code)
	}
	for _, c := range code {
		if !('0' <= c && c <= '9') && !('a' <= c && c <= 'f') {
			t.Fatalf("non-hex char %q in %q", c, code)
		}
	}
}

func TestPackageVerificationCode_DiffersByInput(t *testing.T) {
	a := packageVerificationCode(Package{Name: "foo", Version: "v1"})
	b := packageVerificationCode(Package{Name: "foo", Version: "v2"})
	if a == b {
		t.Fatalf("different packages produced same code")
	}
}

// ── writeTV ───────────────────────────────────────────────────────────────────

func TestWriteTV_Header22(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.2", testPkgs())
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertContains(t, out, "SPDXVersion: SPDX-2.2")
	assertContains(t, out, "DataLicense: CC0-1.0")
	assertContains(t, out, "SPDXID: SPDXRef-DOCUMENT")
	assertContains(t, out, "DocumentName: test-doc")
	assertContains(t, out, "DocumentNamespace: https://example.com/spdx/test")
	assertContains(t, out, "Creator: Tool: vissr/tools/sbom")
	assertContains(t, out, "Creator: Organization: COVESA")
	assertContains(t, out, "Created: 2026-06-08T00:00:00Z")
}

func TestWriteTV_PackageFields(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertContains(t, out, "PackageName: example.com/foo")
	assertContains(t, out, "PackageVersion: v1.2.3")
	assertContains(t, out, "PackageDownloadLocation: https://pkg.go.dev/example.com/foo@v1.2.3")
	assertContains(t, out, "FilesAnalyzed: false")
	assertContains(t, out, "PackageLicenseConcluded: NOASSERTION")
	assertContains(t, out, "PackageLicenseDeclared: NOASSERTION")
	assertContains(t, out, "PackageCopyrightText: NOASSERTION")
	assertContains(t, out, "Relationship: SPDXRef-DOCUMENT DESCRIBES SPDXRef-Package-1")
}

func TestWriteTV_Version301HasVerificationCode(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("3.0.1", testPkgs())
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertContains(t, out, "PackageVerificationCode:")
}

func TestWriteTV_Version22NoVerificationCode(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.2", testPkgs())
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "PackageVerificationCode:") {
		t.Fatal("2.2 output should not contain PackageVerificationCode")
	}
}

func TestWriteTV_EmptyPackages(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", nil)
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertContains(t, out, "SPDXVersion: SPDX-2.3")
	if strings.Contains(out, "PackageName:") {
		t.Fatal("no packages expected in output")
	}
}

func TestWriteTV_NoVersionField_EmptyVersion(t *testing.T) {
	var buf bytes.Buffer
	pkgs := []Package{{Name: "example.com/foo", Version: "", SPDXID: "SPDXRef-Package-1"}}
	doc := testDoc("2.3", pkgs)
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "PackageVersion:") {
		t.Fatal("PackageVersion should be omitted when empty")
	}
}

func TestWriteTV_MultiplePackages(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertContains(t, out, "PackageName: example.com/foo")
	assertContains(t, out, "PackageName: example.com/bar")
}

func TestWriteTV_LicenseSet(t *testing.T) {
	var buf bytes.Buffer
	pkgs := []Package{{
		Name:    "example.com/foo",
		Version: "v1.0.0",
		SPDXID:  "SPDXRef-Package-1",
		License: "MIT",
	}}
	doc := testDoc("2.3", pkgs)
	if err := writeTV(&buf, doc); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	assertContains(t, out, "PackageLicenseConcluded: MIT")
	assertContains(t, out, "PackageLicenseDeclared: MIT")
}

// ── writeJSON ─────────────────────────────────────────────────────────────────

func TestWriteJSON_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
}

func TestWriteJSON_HeaderFields(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	if jd.SpdxVersion != "SPDX-2.3" {
		t.Fatalf("spdxVersion=%q", jd.SpdxVersion)
	}
	if jd.DataLicense != "CC0-1.0" {
		t.Fatalf("dataLicense=%q", jd.DataLicense)
	}
	if jd.SPDXID != "SPDXRef-DOCUMENT" {
		t.Fatalf("SPDXID=%q", jd.SPDXID)
	}
	if jd.Name != "test-doc" {
		t.Fatalf("name=%q", jd.Name)
	}
	if jd.DocumentNamespace != "https://example.com/spdx/test" {
		t.Fatalf("namespace=%q", jd.DocumentNamespace)
	}
}

func TestWriteJSON_CreationInfo(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	if jd.CreationInfo.Created != "2026-06-08T00:00:00Z" {
		t.Fatalf("created=%q", jd.CreationInfo.Created)
	}
	if len(jd.CreationInfo.Creators) != 2 {
		t.Fatalf("creators len=%d", len(jd.CreationInfo.Creators))
	}
}

func TestWriteJSON_PackageCount(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	if len(jd.Packages) != 2 {
		t.Fatalf("packages len=%d", len(jd.Packages))
	}
}

func TestWriteJSON_RelationshipCount(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	if len(jd.Relationships) != 2 {
		t.Fatalf("relationships len=%d", len(jd.Relationships))
	}
	for _, r := range jd.Relationships {
		if r.RelationshipType != "DESCRIBES" {
			t.Fatalf("unexpected relationship type %q", r.RelationshipType)
		}
		if r.SpdxElementID != "SPDXRef-DOCUMENT" {
			t.Fatalf("unexpected elementId %q", r.SpdxElementID)
		}
	}
}

func TestWriteJSON_Version301HasPrimaryPurpose(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("3.0.1", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	for _, p := range jd.Packages {
		if p.PrimaryPackagePurpose != "LIBRARY" {
			t.Fatalf("package %q missing LIBRARY purpose", p.Name)
		}
	}
}

func TestWriteJSON_Version22NoPrimaryPurpose(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.2", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	for _, p := range jd.Packages {
		if p.PrimaryPackagePurpose != "" {
			t.Fatalf("package %q should not have primaryPackagePurpose in 2.2", p.Name)
		}
	}
}

func TestWriteJSON_EmptyPackages(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", nil)
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	if len(jd.Packages) != 0 {
		t.Fatalf("expected 0 packages")
	}
}

func TestWriteJSON_FilesAnalyzedFalse(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	for _, p := range jd.Packages {
		if p.FilesAnalyzed {
			t.Fatalf("package %q filesAnalyzed should be false", p.Name)
		}
	}
}

func TestWriteJSON_LicenseSet(t *testing.T) {
	var buf bytes.Buffer
	pkgs := []Package{{
		Name:    "example.com/foo",
		Version: "v1.0.0",
		SPDXID:  "SPDXRef-Package-1",
		License: "Apache-2.0",
	}}
	doc := testDoc("2.3", pkgs)
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	var jd spdxDocJSON
	if err := json.Unmarshal(buf.Bytes(), &jd); err != nil {
		t.Fatal(err)
	}
	if jd.Packages[0].LicenseConcluded != "Apache-2.0" {
		t.Fatalf("licenseConcluded=%q", jd.Packages[0].LicenseConcluded)
	}
}

func TestWriteJSON_Indented(t *testing.T) {
	var buf bytes.Buffer
	doc := testDoc("2.3", testPkgs())
	if err := writeJSON(&buf, doc); err != nil {
		t.Fatal(err)
	}
	// Indented output must have newlines and leading spaces.
	if !strings.Contains(buf.String(), "\n  ") {
		t.Fatal("expected indented JSON")
	}
}

// ── all three spec versions produce valid output ──────────────────────────────

func TestAllVersions_TV(t *testing.T) {
	for _, v := range []string{"2.2", "2.3", "3.0.1"} {
		t.Run(v, func(t *testing.T) {
			var buf bytes.Buffer
			doc := testDoc(v, testPkgs())
			if err := writeTV(&buf, doc); err != nil {
				t.Fatal(err)
			}
			assertContains(t, buf.String(), "SPDXVersion: SPDX-"+v)
		})
	}
}

func TestAllVersions_JSON(t *testing.T) {
	for _, v := range []string{"2.2", "2.3", "3.0.1"} {
		t.Run(v, func(t *testing.T) {
			var buf bytes.Buffer
			doc := testDoc(v, testPkgs())
			if err := writeJSON(&buf, doc); err != nil {
				t.Fatal(err)
			}
			var raw map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
				t.Fatalf("not valid JSON for v%s: %v", v, err)
			}
			if raw["spdxVersion"] != "SPDX-"+v {
				t.Fatalf("spdxVersion=%q", raw["spdxVersion"])
			}
		})
	}
}

// ── fuzz ──────────────────────────────────────────────────────────────────────

func FuzzWriteTV(f *testing.F) {
	f.Add("2.2", "example.com/foo", "v1.0.0", "MIT")
	f.Add("2.3", "", "", "")
	f.Add("3.0.1", "a/b/c", "v0.0.1", "Apache-2.0")
	f.Add("2.3", "x", "v999.999.999", "NOASSERTION")

	f.Fuzz(func(t *testing.T, version, pkgName, pkgVer, license string) {
		doc := Document{
			SpecVersion:  version,
			DocumentName: "fuzz",
			SPDXID:       "SPDXRef-DOCUMENT",
			Namespace:    "https://fuzz.example.com",
			Created:      "2026-06-08T00:00:00Z",
			CreatorTool:  "sbom",
			CreatorOrg:   "test",
			Packages: []Package{{
				Name:    pkgName,
				Version: pkgVer,
				SPDXID:  "SPDXRef-Package-1",
				License: license,
			}},
		}
		var buf bytes.Buffer
		_ = writeTV(&buf, doc) // must not panic
	})
}

func FuzzWriteJSON(f *testing.F) {
	f.Add("2.2", "example.com/foo", "v1.0.0", "MIT")
	f.Add("2.3", "", "", "")
	f.Add("3.0.1", "a/b/c", "v0.0.1", "Apache-2.0")
	f.Add("2.3", "x", "v999.999.999", "NOASSERTION")

	f.Fuzz(func(t *testing.T, version, pkgName, pkgVer, license string) {
		doc := Document{
			SpecVersion:  version,
			DocumentName: "fuzz",
			SPDXID:       "SPDXRef-DOCUMENT",
			Namespace:    "https://fuzz.example.com",
			Created:      "2026-06-08T00:00:00Z",
			CreatorTool:  "sbom",
			CreatorOrg:   "test",
			Packages: []Package{{
				Name:    pkgName,
				Version: pkgVer,
				SPDXID:  "SPDXRef-Package-1",
				License: license,
			}},
		}
		var buf bytes.Buffer
		_ = writeJSON(&buf, doc) // must not panic
	})
}

func FuzzDownloadURL(f *testing.F) {
	f.Add("github.com/foo/bar", "v1.0.0")
	f.Add("", "")
	f.Add("x", "")
	f.Add("github.com/foo/bar", "")

	f.Fuzz(func(t *testing.T, path, version string) {
		got := downloadURL(path, version)
		if got == "" {
			t.Fatal("downloadURL must not return empty string")
		}
	})
}

// ── assertions ────────────────────────────────────────────────────────────────

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q\ngot:\n%s", needle, haystack)
	}
}
