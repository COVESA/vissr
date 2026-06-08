// sbom generates SPDX Software Bill of Materials documents for the vissr
// module and its dependencies.
//
// Supported formats: SPDX 2.2, SPDX 2.3, SPDX 3.0.1 (tag-value and JSON).
//
// Usage:
//
//	go run ./tools/sbom [flags]
//
// Flags:
//
//	--version  2.2 | 2.3 | 3.0.1       SPDX spec version (default: 2.3)
//	--format   tv | json               Output format (default: tv)
//	--out      <path>                  Output file (default: stdout)
//	--name     <package name>          Top-level package name (default: vissr)
//	--ns       <namespace URI>         SPDX document namespace
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/akamensky/argparse"
)

// ── model ────────────────────────────────────────────────────────────────────

type Package struct {
	Name        string
	Version     string
	License     string
	SPDXID      string
	DownloadURL string
	Homepage    string
	Supplier    string
}

type Document struct {
	SpecVersion   string
	DocumentName  string
	SPDXID        string
	Namespace     string
	Created       string
	CreatorTool   string
	CreatorOrg    string
	Packages      []Package
}

// ── entry point ───────────────────────────────────────────────────────────────

func main() {
	parser := argparse.NewParser("sbom", "Generate SPDX SBOM for vissr")

	version := parser.String("", "version", &argparse.Options{
		Required: false,
		Help:     "SPDX spec version: 2.2, 2.3, 3.0.1",
		Default:  "2.3",
	})
	format := parser.String("", "format", &argparse.Options{
		Required: false,
		Help:     "Output format: tv (tag-value) or json",
		Default:  "tv",
	})
	outPath := parser.String("", "out", &argparse.Options{
		Required: false,
		Help:     "Output file path (default: stdout)",
		Default:  "",
	})
	docName := parser.String("", "name", &argparse.Options{
		Required: false,
		Help:     "Document name",
		Default:  "vissr",
	})
	ns := parser.String("", "ns", &argparse.Options{
		Required: false,
		Help:     "SPDX document namespace URI",
		Default:  "",
	})

	if err := parser.Parse(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, parser.Usage(err))
		os.Exit(1)
	}

	switch *version {
	case "2.2", "2.3", "3.0.1":
	default:
		fmt.Fprintf(os.Stderr, "unsupported SPDX version %q (use 2.2, 2.3, or 3.0.1)\n", *version)
		os.Exit(1)
	}

	switch *format {
	case "tv", "json":
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (use tv or json)\n", *format)
		os.Exit(1)
	}

	pkgs, err := goListPackages()
	if err != nil {
		fmt.Fprintf(os.Stderr, "go list: %v\n", err)
		os.Exit(1)
	}

	namespace := *ns
	if namespace == "" {
		namespace = "https://spdx.org/spdxdocs/" + *docName + "-" + timestamp()
	}

	doc := Document{
		SpecVersion:  *version,
		DocumentName: *docName,
		SPDXID:       "SPDXRef-DOCUMENT",
		Namespace:    namespace,
		Created:      time.Now().UTC().Format(time.RFC3339),
		CreatorTool:  "vissr/tools/sbom",
		CreatorOrg:   "COVESA",
		Packages:     pkgs,
	}

	var w io.Writer = os.Stdout
	if *outPath != "" {
		if err := os.MkdirAll(filepath.Dir(*outPath), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			os.Exit(1)
		}
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	switch *format {
	case "json":
		err = writeJSON(w, doc)
	default:
		err = writeTV(w, doc)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
}

// ── go list integration ───────────────────────────────────────────────────────

type goModule struct {
	Path    string `json:"Path"`
	Version string `json:"Version"`
	License string `json:"License,omitempty"`
}

func goListPackages() ([]Package, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -m -json all: %w", err)
	}

	var pkgs []Package
	dec := json.NewDecoder(strings.NewReader(string(out)))
	idx := 1
	for {
		var m goModule
		if err := dec.Decode(&m); err != nil {
			break
		}
		if m.Path == "" {
			continue
		}
		pkg := Package{
			Name:        m.Path,
			Version:     m.Version,
			SPDXID:      fmt.Sprintf("SPDXRef-Package-%d", idx),
			DownloadURL: downloadURL(m.Path, m.Version),
			License:     noassertion(m.License),
		}
		pkgs = append(pkgs, pkg)
		idx++
	}
	return pkgs, nil
}

func downloadURL(path, version string) string {
	if version == "" {
		return "NOASSERTION"
	}
	return fmt.Sprintf("https://pkg.go.dev/%s@%s", path, version)
}

func noassertion(s string) string {
	if s == "" {
		return "NOASSERTION"
	}
	return s
}

// ── writers ──────────────────────────────────────────────────────────────────

func writeTV(w io.Writer, doc Document) error {
	// Header
	fmt.Fprintf(w, "SPDXVersion: SPDX-%s\n", doc.SpecVersion)
	fmt.Fprintf(w, "DataLicense: CC0-1.0\n")
	fmt.Fprintf(w, "SPDXID: %s\n", doc.SPDXID)
	fmt.Fprintf(w, "DocumentName: %s\n", doc.DocumentName)
	fmt.Fprintf(w, "DocumentNamespace: %s\n", doc.Namespace)
	fmt.Fprintf(w, "Creator: Tool: %s\n", doc.CreatorTool)
	fmt.Fprintf(w, "Creator: Organization: %s\n", doc.CreatorOrg)
	fmt.Fprintf(w, "Created: %s\n", doc.Created)

	for _, pkg := range doc.Packages {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "PackageName: %s\n", pkg.Name)
		fmt.Fprintf(w, "SPDXID: %s\n", pkg.SPDXID)
		if pkg.Version != "" {
			fmt.Fprintf(w, "PackageVersion: %s\n", pkg.Version)
		}
		fmt.Fprintf(w, "PackageDownloadLocation: %s\n", noassertion(pkg.DownloadURL))
		fmt.Fprintf(w, "FilesAnalyzed: false\n")
		fmt.Fprintf(w, "PackageLicenseConcluded: %s\n", noassertion(pkg.License))
		fmt.Fprintf(w, "PackageLicenseDeclared: %s\n", noassertion(pkg.License))
		fmt.Fprintf(w, "PackageCopyrightText: NOASSERTION\n")

		if doc.SpecVersion == "3.0.1" {
			fmt.Fprintf(w, "PackageVerificationCode: %s\n", packageVerificationCode(pkg))
		}

		// Relationship: document describes this package.
		fmt.Fprintf(w, "Relationship: %s DESCRIBES %s\n", doc.SPDXID, pkg.SPDXID)
	}
	return nil
}

// ── JSON writer ───────────────────────────────────────────────────────────────

type spdxDocJSON struct {
	SpdxVersion     string            `json:"spdxVersion"`
	DataLicense     string            `json:"dataLicense"`
	SPDXID          string            `json:"SPDXID"`
	Name            string            `json:"name"`
	DocumentNamespace string          `json:"documentNamespace"`
	CreationInfo    spdxCreationInfo  `json:"creationInfo"`
	Packages        []spdxPackageJSON `json:"packages"`
	Relationships   []spdxRelJSON     `json:"relationships,omitempty"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackageJSON struct {
	Name                  string `json:"name"`
	SPDXID                string `json:"SPDXID"`
	Version               string `json:"versionInfo,omitempty"`
	DownloadLocation      string `json:"downloadLocation"`
	FilesAnalyzed         bool   `json:"filesAnalyzed"`
	LicenseConcluded      string `json:"licenseConcluded"`
	LicenseDeclared       string `json:"licenseDeclared"`
	CopyrightText         string `json:"copyrightText"`
	PrimaryPackagePurpose string `json:"primaryPackagePurpose,omitempty"`
}

type spdxRelJSON struct {
	SpdxElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSpdxElement string `json:"relatedSpdxElement"`
}

func writeJSON(w io.Writer, doc Document) error {
	v := "SPDX-" + doc.SpecVersion
	jdoc := spdxDocJSON{
		SpdxVersion:       v,
		DataLicense:       "CC0-1.0",
		SPDXID:            doc.SPDXID,
		Name:              doc.DocumentName,
		DocumentNamespace: doc.Namespace,
		CreationInfo: spdxCreationInfo{
			Created: doc.Created,
			Creators: []string{
				"Tool: " + doc.CreatorTool,
				"Organization: " + doc.CreatorOrg,
			},
		},
	}

	for _, pkg := range doc.Packages {
		jp := spdxPackageJSON{
			Name:             pkg.Name,
			SPDXID:           pkg.SPDXID,
			Version:          pkg.Version,
			DownloadLocation: noassertion(pkg.DownloadURL),
			FilesAnalyzed:    false,
			LicenseConcluded: noassertion(pkg.License),
			LicenseDeclared:  noassertion(pkg.License),
			CopyrightText:    "NOASSERTION",
		}
		if doc.SpecVersion == "3.0.1" {
			jp.PrimaryPackagePurpose = "LIBRARY"
		}
		jdoc.Packages = append(jdoc.Packages, jp)
		jdoc.Relationships = append(jdoc.Relationships, spdxRelJSON{
			SpdxElementID:      doc.SPDXID,
			RelationshipType:   "DESCRIBES",
			RelatedSpdxElement: pkg.SPDXID,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jdoc)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func packageVerificationCode(pkg Package) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s@%s", pkg.Name, pkg.Version)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func timestamp() string {
	return time.Now().UTC().Format("20060102T150405Z")
}
