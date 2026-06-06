// Package vdmloader parses COVESA VDM GraphQL SDL files and builds
// vissr Node_t signal trees that can be registered in the HIM forest.
//
// VDM (Vehicle Data Model) uses GraphQL SDL with custom directives:
//   - @vspec(element: BRANCH|SENSOR|ACTUATOR|ATTRIBUTE, fqn: "Dot.Sep.Path")
//     on OBJECT → marks a type as a VSS tree node; on FIELD_DEFINITION → marks
//     a scalar field as a signal leaf.
//   - @range(min: Float, max: Float) on FIELD_DEFINITION → numeric bounds.
//   - @viss_service on FIELD_DEFINITION → marks a field as a service procedure
//     entry point (v4.0 extension; not in upstream VDM yet).
//
// The loader reconstructs the dot-separated FQN hierarchy from the SDL and
// builds a utils.Node_t tree for each root node found.
package vdmloader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/covesa/vissr/utils"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// vdmPreamble defines the directives and custom scalars needed to parse any VDM SDL.
// It is prepended to every SDL string before parsing.
const vdmPreamble = `
directive @vspec(
  element: VspecElement!
  fqn: String!
  description: String
  comment: String
  deprecation: String
) on OBJECT | FIELD_DEFINITION

directive @range(min: Float, max: Float) on FIELD_DEFINITION

directive @viss_service on FIELD_DEFINITION

enum VspecElement {
  BRANCH
  SENSOR
  ACTUATOR
  ATTRIBUTE
  STRUCT
  PROPERTY
}

scalar Int8
scalar UInt8
scalar Int16
scalar UInt16
scalar UInt32
scalar Int64
scalar UInt64
`

// TreeMeta holds the metadata extracted alongside each root Node_t.
type TreeMeta struct {
	RootName string // first FQN segment, e.g. "Vehicle"
	Domain   string // same as RootName; last segment is used as vissr info type
	Version  string // from schema @specifiedBy or directory convention
}

// signalNode groups a Node_t with its FQN for tree-linking.
type signalNode struct {
	fqn  string
	node *utils.Node_t
}

// LoadDir scans dir for *.graphql files, parses each as VDM SDL, and
// registers the resulting Node_t trees in the vissr HIM forest via
// utils.RegisterServiceTree. Returns the number of trees registered.
func LoadDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("vdmloader: reading %s: %w", dir, err)
	}

	total := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".graphql") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return total, fmt.Errorf("vdmloader: reading %s: %w", path, err)
		}
		roots, metas, err := ParseSDL(string(data))
		if err != nil {
			return total, fmt.Errorf("vdmloader: parsing %s: %w", path, err)
		}
		for i, root := range roots {
			if !utils.RegisterServiceTree(metas[i].RootName, metas[i].Domain, metas[i].Version, root) {
				utils.Info.Printf("vdmloader: tree %s already registered, skipping", metas[i].RootName)
			} else {
				total++
			}
		}
	}
	return total, nil
}

// ParseFile parses a single VDM SDL file and returns root nodes and metadata.
func ParseFile(path string) ([]*utils.Node_t, []TreeMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("vdmloader: reading %s: %w", path, err)
	}
	return ParseSDL(string(data))
}

// ParseSDL parses a VDM GraphQL SDL string and returns the built Node_t trees
// (one per FQN root) together with their metadata.
func ParseSDL(sdl string) ([]*utils.Node_t, []TreeMeta, error) {
	src := &ast.Source{
		Name:  "vdm.graphql",
		Input: vdmPreamble + sdl,
	}

	schema, gqlErr := gqlparser.LoadSchema(src)
	if gqlErr != nil {
		return nil, nil, fmt.Errorf("vdmloader: SDL parse error: %v", gqlErr)
	}

	// fqnMap: FQN → node
	fqnMap := make(map[string]*utils.Node_t)
	// visit every type definition to find BRANCH, SENSOR, ACTUATOR, ATTRIBUTE nodes
	for _, t := range schema.Types {
		if t.BuiltIn {
			continue
		}
		typeFQN, typeElem := vspecDirectiveArgs(t.Directives)
		if typeFQN != "" && typeElem == "BRANCH" {
			// Ensure branch node exists
			if _, ok := fqnMap[typeFQN]; !ok {
				name := lastSegment(typeFQN)
				fqnMap[typeFQN] = utils.NewBranchNode(name)
			}
		}
		// Visit fields on any type for signal leaves
		for _, f := range t.Fields {
			fieldFQN, fieldElem := vspecDirectiveArgs(f.Directives)
			if fieldFQN == "" {
				continue
			}
			name := lastSegment(fieldFQN)
			switch fieldElem {
			case "SENSOR", "ACTUATOR", "ATTRIBUTE":
				minVal, maxVal := rangeArgs(f.Directives)
				dt := mapDatatype(f.Type.Name())
				desc := descriptionArg(f.Directives)
				nodeType := mapElement(fieldElem)
				n := utils.NewSignalNode(name, nodeType, dt, desc, minVal, maxVal, "")
				fqnMap[fieldFQN] = n
			case "BRANCH":
				if _, ok := fqnMap[fieldFQN]; !ok {
					fqnMap[fieldFQN] = utils.NewBranchNode(name)
				}
			case "PROCEDURE":
				// @viss_service marks a procedure entry (v4.0 extension)
				desc := descriptionArg(f.Directives)
				n := utils.NewProcedureNode(name, desc)
				fqnMap[fieldFQN] = n
			}
			// Also handle @viss_service fields regardless of element
			if hasDirective(f.Directives, "viss_service") {
				if fqnMap[fieldFQN] == nil || fqnMap[fieldFQN].NodeType != utils.PROCEDURE {
					desc := descriptionArg(f.Directives)
					fqnMap[fieldFQN] = utils.NewProcedureNode(name, desc)
				}
			}
		}
	}

	if len(fqnMap) == 0 {
		return nil, nil, fmt.Errorf("vdmloader: no @vspec-annotated nodes found in SDL")
	}

	// Sort FQNs so parent nodes are always processed before children
	fqns := make([]string, 0, len(fqnMap))
	for fqn := range fqnMap {
		fqns = append(fqns, fqn)
	}
	sort.Strings(fqns)

	// Link parent → child
	for _, fqn := range fqns {
		parentFQN := parentOf(fqn)
		if parentFQN == "" {
			continue // root node
		}
		parent, ok := fqnMap[parentFQN]
		if !ok {
			// Synthesise missing intermediate branches
			name := lastSegment(parentFQN)
			parent = utils.NewBranchNode(name)
			fqnMap[parentFQN] = parent
			// Ensure parent is also linked upward (will be picked up on next pass)
			fqns = append(fqns, parentFQN)
			sort.Strings(fqns)
		}
		child := fqnMap[fqn]
		appendChild(parent, child)
	}

	// Collect root nodes (FQN with no dot)
	var roots []*utils.Node_t
	var metas []TreeMeta
	for fqn, node := range fqnMap {
		if !strings.Contains(fqn, ".") {
			metas = append(metas, TreeMeta{
				RootName: fqn,
				Domain:   fqn,
				Version:  "1.0",
			})
			roots = append(roots, node)
		}
	}
	if len(roots) == 0 {
		return nil, nil, fmt.Errorf("vdmloader: SDL contains no root-level BRANCH type")
	}
	return roots, metas, nil
}

// vspecDirectiveArgs extracts (fqn, element) from a @vspec directive list.
func vspecDirectiveArgs(dirs ast.DirectiveList) (fqn, element string) {
	for _, d := range dirs {
		if d.Name != "vspec" {
			continue
		}
		for _, arg := range d.Arguments {
			switch arg.Name {
			case "fqn":
				fqn = strings.Trim(arg.Value.String(), `"`)
			case "element":
				element = arg.Value.String()
			}
		}
		return
	}
	return
}

// descriptionArg returns the description string from @vspec if present.
func descriptionArg(dirs ast.DirectiveList) string {
	for _, d := range dirs {
		if d.Name != "vspec" {
			continue
		}
		for _, arg := range d.Arguments {
			if arg.Name == "description" {
				return strings.Trim(arg.Value.String(), `"`)
			}
		}
	}
	return ""
}

// rangeArgs extracts (min, max) strings from a @range directive list.
func rangeArgs(dirs ast.DirectiveList) (min, max string) {
	for _, d := range dirs {
		if d.Name != "range" {
			continue
		}
		for _, arg := range d.Arguments {
			switch arg.Name {
			case "min":
				min = formatFloat(arg.Value.String())
			case "max":
				max = formatFloat(arg.Value.String())
			}
		}
		return
	}
	return
}

// hasDirective reports whether the directive list contains a directive by name.
func hasDirective(dirs ast.DirectiveList, name string) bool {
	for _, d := range dirs {
		if d.Name == name {
			return true
		}
	}
	return false
}

// mapDatatype maps GraphQL type names to VSS datatype strings.
func mapDatatype(gqlType string) string {
	switch gqlType {
	case "Int8":
		return "int8"
	case "UInt8":
		return "uint8"
	case "Int16":
		return "int16"
	case "UInt16":
		return "uint16"
	case "Int":
		return "int32"
	case "UInt32":
		return "uint32"
	case "Int64":
		return "int64"
	case "UInt64":
		return "uint64"
	case "Float":
		return "float"
	case "Boolean":
		return "bool"
	case "String":
		return "string"
	default:
		return strings.ToLower(gqlType)
	}
}

// mapElement converts a VspecElement string to a utils node type constant.
func mapElement(element string) string {
	switch element {
	case "SENSOR":
		return utils.SENSOR
	case "ACTUATOR":
		return utils.ACTUATOR
	case "ATTRIBUTE":
		return utils.ATTRIBUTE
	case "BRANCH":
		return utils.BRANCH
	default:
		return utils.ATTRIBUTE
	}
}

// lastSegment returns the portion of an FQN after the final dot.
func lastSegment(fqn string) string {
	i := strings.LastIndex(fqn, ".")
	if i < 0 {
		return fqn
	}
	return fqn[i+1:]
}

// parentOf returns the FQN with the last segment removed, or "" for roots.
func parentOf(fqn string) string {
	i := strings.LastIndex(fqn, ".")
	if i < 0 {
		return ""
	}
	return fqn[:i]
}

// appendChild adds child to parent if not already present.
func appendChild(parent, child *utils.Node_t) {
	for _, existing := range parent.Child {
		if existing == child {
			return
		}
	}
	child.Parent = parent
	parent.Child = append(parent.Child, child)
	parent.Children = uint8(len(parent.Child))
}

// formatFloat trims trailing zeros from a float string for compact storage.
func formatFloat(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
