// vdminfo prints the signal tree from one or more VDM .graphql SDL files.
//
// Usage:
//
//	vdminfo <dir-or-file> [<dir-or-file> …]
//
// For directories every *.graphql file is parsed. For individual files the
// path is parsed directly. The signal tree is printed in indented form with
// per-node metadata (nodeType, datatype, range, unit, default, allowed).
//
// Example:
//
//	go run ./tools/vdminfo ./server/vissv2server/vdmloader/testdata/
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/covesa/vissr/server/vissv2server/vdmloader"
	"github.com/covesa/vissr/utils"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: vdminfo <dir-or-file> [<dir-or-file> …]")
		os.Exit(1)
	}

	// Initialise a silent logger so vdmloader doesn't panic on nil Info.
	utils.InitLog("vdminfo.log", os.TempDir(), false, "error")

	exitCode := 0
	for _, arg := range os.Args[1:] {
		info, err := os.Stat(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vdminfo: %v\n", err)
			exitCode = 1
			continue
		}
		var roots []*utils.Node_t
		var metas []vdmloader.TreeMeta

		if info.IsDir() {
			entries, err := os.ReadDir(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vdminfo: reading %s: %v\n", arg, err)
				exitCode = 1
				continue
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".graphql") {
					continue
				}
				r, m, err := vdmloader.ParseFile(arg + "/" + e.Name())
				if err != nil {
					fmt.Fprintf(os.Stderr, "vdminfo: %s: %v\n", e.Name(), err)
					exitCode = 1
					continue
				}
				roots = append(roots, r...)
				metas = append(metas, m...)
			}
		} else {
			r, m, err := vdmloader.ParseFile(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "vdminfo: %s: %v\n", arg, err)
				exitCode = 1
				continue
			}
			roots = append(roots, r...)
			metas = append(metas, m...)
		}

		for i, root := range roots {
			meta := metas[i]
			fmt.Printf("=== %s  (domain: %s, version: %s) ===\n", meta.RootName, meta.Domain, meta.Version)
			printNode(root, 0)
			fmt.Println()
		}
	}
	os.Exit(exitCode)
}

func printNode(n *utils.Node_t, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s%s", indent, n.Name)

	// Collect metadata annotations
	var parts []string
	if n.NodeType != "" && n.NodeType != utils.BRANCH {
		parts = append(parts, n.NodeType)
	}
	if n.Datatype != "" {
		parts = append(parts, n.Datatype)
	}
	if n.Unit != "" {
		parts = append(parts, n.Unit)
	}
	if n.Min != "" || n.Max != "" {
		parts = append(parts, fmt.Sprintf("%s..%s", n.Min, n.Max))
	}
	if n.DefaultValue != "" {
		parts = append(parts, fmt.Sprintf("default=%s", n.DefaultValue))
	}
	if len(n.AllowedDef) > 0 {
		parts = append(parts, fmt.Sprintf("allowed=[%s]", strings.Join(n.AllowedDef, "|")))
	}

	if len(parts) > 0 {
		fmt.Printf("  (%s)", strings.Join(parts, ", "))
	}
	if n.NodeType == utils.BRANCH || n.NodeType == "" {
		fmt.Printf("  [branch, %d children]", len(n.Child))
	}
	if n.Description != "" && depth == 0 {
		fmt.Printf("  — %s", n.Description)
	}
	fmt.Println()

	for _, child := range n.Child {
		printNode(child, depth+1)
	}
}
