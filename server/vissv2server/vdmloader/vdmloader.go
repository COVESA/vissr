// Package vdmloader parses COVESA VDM GraphQL SDL files and builds
// vissr Node_t signal trees that can be registered in the HIM forest.
//
// VDM (Vehicle Data Model) uses GraphQL SDL with custom directives:
//   - @vspec(element: BRANCH|SENSOR|ACTUATOR|ATTRIBUTE, fqn: "Dot.Sep.Path")
//     on OBJECT → marks a type as a VSS tree node; on FIELD_DEFINITION → marks
//     a scalar field as a signal leaf; on ENUM_VALUE → provides originalName.
//   - @range(min: Float, max: Float) on FIELD_DEFINITION → numeric bounds.
//   - @instanceTag on OBJECT → marks a type as a multi-instance template.
//     The type must also carry @vspec(fqn:) and have fields dimension1,
//     dimension2, … each typed as an enum of allowed instance values.
//   - @viss_service on FIELD_DEFINITION → marks a field as a service procedure
//     entry point (v4.0 extension; not in upstream VDM yet).
//
// Instance tag expansion: types named *_InstanceTag and bearing @instanceTag
// are expanded by the loader into full per-instance subtrees. For example,
// a Seat_InstanceTag with Dimension1=[Row1,Row2] × Dimension2=[DriverSide,PassengerSide]
// produces branch nodes at Vehicle.Cabin.Seat.Row1.DriverSide, .Row1.PassengerSide, etc.,
// each containing a deep clone of the original Seat signal subtree.
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
  element: VspecElement
  fqn: String
  description: String
  comment: String
  deprecation: String
  originalName: String
) on OBJECT | FIELD_DEFINITION | ENUM_VALUE

directive @range(min: Float, max: Float) on FIELD_DEFINITION

directive @instanceTag on OBJECT

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

// dimValue pairs an enum value name with the expanded path-segment name.
type dimValue struct {
	origName string // e.g. "Row1", "DriverSide" — used as Node_t.Name
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

	for _, t := range schema.Types {
		if t.BuiltIn {
			continue
		}
		typeFQN, typeElem := vspecDirectiveArgs(t.Directives)
		if typeFQN != "" && typeElem == "BRANCH" {
			if _, ok := fqnMap[typeFQN]; !ok {
				name := lastSegment(typeFQN)
				fqnMap[typeFQN] = utils.NewBranchNode(name)
			}
		}
		for _, f := range t.Fields {
			fieldFQN, fieldElem := vspecDirectiveArgs(f.Directives)
			if fieldFQN == "" {
				continue
			}
			name := lastSegment(fieldFQN)

			// @viss_service overrides element → PROCEDURE
			if hasDirective(f.Directives, "viss_service") {
				desc := descriptionArg(f.Directives)
				fqnMap[fieldFQN] = utils.NewProcedureNode(name, desc)
				continue
			}

			switch fieldElem {
			case "SENSOR", "ACTUATOR", "ATTRIBUTE":
				minVal, maxVal := rangeArgs(f.Directives)
				dt := mapDatatype(f.Type.Name())
				desc := descriptionArg(f.Directives)
				nodeType := mapElement(fieldElem)
				fqnMap[fieldFQN] = utils.NewSignalNode(name, nodeType, dt, desc, minVal, maxVal, "")
			case "BRANCH":
				if _, ok := fqnMap[fieldFQN]; !ok {
					fqnMap[fieldFQN] = utils.NewBranchNode(name)
				}
			}
		}
	}

	if len(fqnMap) == 0 {
		return nil, nil, fmt.Errorf("vdmloader: no @vspec-annotated nodes found in SDL")
	}

	// Sort FQNs so parents are always linked before children.
	fqns := make([]string, 0, len(fqnMap))
	for fqn := range fqnMap {
		fqns = append(fqns, fqn)
	}
	sort.Strings(fqns)

	// First pass: link parent → child so every base node has its children
	// attached before instance tag expansion clones them.
	for _, fqn := range fqns {
		parentFQN := parentOf(fqn)
		if parentFQN == "" {
			continue
		}
		parent, ok := fqnMap[parentFQN]
		if !ok {
			name := lastSegment(parentFQN)
			parent = utils.NewBranchNode(name)
			fqnMap[parentFQN] = parent
			fqns = append(fqns, parentFQN)
			sort.Strings(fqns)
		}
		child := fqnMap[fqn]
		appendChild(parent, child)
	}

	// Expand instance tags after linking: baseNode.Child is now populated so
	// deepClone correctly captures the full signal subtree for each instance.
	expandInstanceTags(schema, fqnMap)

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

// ── Instance tag expansion ───────────────────────────────────────────────────

// expandInstanceTags finds all *_InstanceTag types and expands the FQN they
// annotate into per-combination subtrees. The original children of the base
// FQN are deep-cloned into every instance branch.
func expandInstanceTags(schema *ast.Schema, fqnMap map[string]*utils.Node_t) {
	for _, t := range schema.Types {
		if t.BuiltIn || !strings.HasSuffix(t.Name, "_InstanceTag") {
			continue
		}
		if !hasDirective(t.Directives, "instanceTag") {
			continue
		}
		baseFQN, _ := vspecDirectiveArgs(t.Directives)
		if baseFQN == "" {
			continue
		}
		baseNode, ok := fqnMap[baseFQN]
		if !ok {
			// The base type may not have been declared as a BRANCH in the SDL;
			// create it now so the expansion has somewhere to attach.
			baseNode = utils.NewBranchNode(lastSegment(baseFQN))
			fqnMap[baseFQN] = baseNode
		}

		dims := collectDimensions(schema, t)
		if len(dims) == 0 {
			continue
		}

		// Snapshot all children of the base FQN (the template subtree).
		// We clone the baseNode itself to capture its current child list.
		templateRoot := deepClone(baseNode)

		// Remove all descendants of baseFQN from the map; they'll be re-added
		// under each instance path.
		prefix := baseFQN + "."
		for fqn := range fqnMap {
			if strings.HasPrefix(fqn, prefix) {
				delete(fqnMap, fqn)
			}
		}

		// Clear base node so expansion can attach fresh instance branches.
		baseNode.Child = nil
		baseNode.Children = 0

		// Generate every combination of dimension values.
		for _, combo := range dimensionCombinations(dims) {
			cur := baseNode
			curFQN := baseFQN
			for _, dv := range combo {
				curFQN = curFQN + "." + dv.origName
				// Reuse an existing branch for this segment if already created
				// by a prior combination (e.g. Row1 already exists when we
				// process Row1.PassengerSide after Row1.DriverSide).
				var branch *utils.Node_t
				for _, ch := range cur.Child {
					if ch.Name == dv.origName {
						branch = ch
						break
					}
				}
				if branch == nil {
					branch = utils.NewBranchNode(dv.origName)
					fqnMap[curFQN] = branch
					appendChild(cur, branch)
				}
				cur = branch
			}
			// cur is now the innermost instance branch (e.g. the DriverSide node).
			// Clone the template's children into it.
			instanceBaseFQN := curFQN
			for _, templateChild := range templateRoot.Child {
				cloned := deepClone(templateChild)
				appendChild(cur, cloned)
				addClonedToMap(fqnMap, cloned, instanceBaseFQN+"."+cloned.Name)
			}
		}
	}
}

// collectDimensions reads the dimension1, dimension2, … fields from an
// _InstanceTag type and returns the ordered list of allowed values for each.
func collectDimensions(schema *ast.Schema, instType *ast.Definition) [][]dimValue {
	var dims [][]dimValue
	for i := 1; ; i++ {
		fieldName := fmt.Sprintf("dimension%d", i)
		f := instType.Fields.ForName(fieldName)
		if f == nil {
			break
		}
		enumType := schema.Types[f.Type.Name()]
		if enumType == nil || enumType.Kind != ast.Enum {
			break
		}
		var values []dimValue
		for _, ev := range enumType.EnumValues {
			origName := originalName(ev)
			values = append(values, dimValue{origName: origName})
		}
		if len(values) > 0 {
			dims = append(dims, values)
		}
	}
	return dims
}

// originalName extracts the @vspec(originalName:) from an enum value, falling
// back to converting the SCREAMING_SNAKE_CASE enum name to PascalCase.
func originalName(ev *ast.EnumValueDefinition) string {
	for _, d := range ev.Directives {
		if d.Name != "vspec" {
			continue
		}
		for _, arg := range d.Arguments {
			if arg.Name == "originalName" {
				return strings.Trim(arg.Value.String(), `"`)
			}
		}
	}
	return screaming2Pascal(ev.Name)
}

// screaming2Pascal converts SCREAMING_SNAKE_CASE to PascalCase.
// e.g. "DRIVER_SIDE" → "DriverSide", "ROW1" → "Row1".
func screaming2Pascal(s string) string {
	parts := strings.Split(s, "_")
	var sb strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		sb.WriteByte(p[0]) // keep first char as-is (already upper from enum)
		if len(p) > 1 {
			sb.WriteString(strings.ToLower(p[1:]))
		}
	}
	return sb.String()
}

// dimensionCombinations returns every ordered combination of one value per
// dimension.  For [[Row1,Row2],[DriverSide,PassengerSide]] it returns:
// [[Row1,DriverSide],[Row1,PassengerSide],[Row2,DriverSide],[Row2,PassengerSide]].
func dimensionCombinations(dims [][]dimValue) [][]dimValue {
	if len(dims) == 0 {
		return [][]dimValue{{}}
	}
	var result [][]dimValue
	for _, v := range dims[0] {
		for _, rest := range dimensionCombinations(dims[1:]) {
			combo := make([]dimValue, 1+len(rest))
			combo[0] = v
			copy(combo[1:], rest)
			result = append(result, combo)
		}
	}
	return result
}

// deepClone returns a full copy of the Node_t subtree rooted at n.
// Parent pointers in the clone are set correctly within the cloned subtree;
// the clone root's Parent is left nil (caller sets it after attaching).
func deepClone(n *utils.Node_t) *utils.Node_t {
	clone := &utils.Node_t{
		Name:         n.Name,
		NodeType:     n.NodeType,
		Uuid:         n.Uuid,
		Description:  n.Description,
		Datatype:     n.Datatype,
		Min:          n.Min,
		Max:          n.Max,
		Unit:         n.Unit,
		Allowed:      n.Allowed,
		DefaultValue: n.DefaultValue,
		Validate:     n.Validate,
	}
	if len(n.AllowedDef) > 0 {
		clone.AllowedDef = append([]string{}, n.AllowedDef...)
	}
	if len(n.Child) > 0 {
		clone.Child = make([]*utils.Node_t, len(n.Child))
		for i, c := range n.Child {
			clonedChild := deepClone(c)
			clonedChild.Parent = clone
			clone.Child[i] = clonedChild
		}
		clone.Children = uint8(len(clone.Child))
	}
	return clone
}

// addClonedToMap recursively registers every node in a cloned subtree into
// fqnMap using rootFQN as the FQN for node, then rootFQN+"."+child.Name etc.
func addClonedToMap(fqnMap map[string]*utils.Node_t, node *utils.Node_t, rootFQN string) {
	fqnMap[rootFQN] = node
	for _, child := range node.Child {
		addClonedToMap(fqnMap, child, rootFQN+"."+child.Name)
	}
}

// ── SDL directive helpers ────────────────────────────────────────────────────

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

// ── Type / element helpers ───────────────────────────────────────────────────

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

// ── FQN helpers ──────────────────────────────────────────────────────────────

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
