# Tree utilities (`utils/treeutils.go`, `utils/datamodel.go`)

This describes how the VISSR tree handling worked **before** it was refactored to the HIM-profile aware
binary parser (vss-tools `him-support` branch), followed by a short summary of what changed.

## Before the refactor

### Overview

VISSR has no dependency on the vss-tools Go parser. Instead, `utils/treeutils.go` contains its own copy of the
Go binary tree parser (originally from `vss-tools/binary/go_parser`), extended with VISSR specifics: a forest of
trees, hardening against malformed files, and helper constructors for dynamically registered service trees.
The commented-out `replace` lines in `go.mod` and the commented imports in `atServer.go` are remnants of the time
when the vss-tools parser was used as an external module.

### Loading the forest

`InitForest("viss.him")` reads the HIM configuration file line by line (plain string matching, not a YAML parse).
For each `HIM.<Root>:` entry it creates a `HimTree` (`RootName`, `TreeType`, `Domain`, `Version`, `Handle`) and, on
the `local:` line, loads the referenced binary file with `VSSReadTree()`. The root node name is then overwritten
with `RootName`. The trees are kept in the package-level `himForest` slice.

`SetRootNodePointer(path)` selects the tree from the first path segment, `GetInfoType()` derives the info type
(`Data`, `Service`, `TypeDefinition`, ...) from the last segment of the tree domain. Service trees can also be added
and removed at runtime with `RegisterServiceTree()` / `DeregisterServiceTree()`, using the
`NewBranchNode/NewSignalNode/NewProcedureNode/NewIoStructNode/NewPropertyNode` constructors.

### Data model

```go
type Node_t struct {
    Name, NodeType, Uuid, Description, Datatype, Min, Max, Unit string
    Allowed uint8; AllowedDef []string
    DefaultValue string
    Validate uint8
    Children uint8
    Parent *Node_t; Child []*Node_t
}
```

- `NodeType` was a **string** and the known types (`sensor`, `actuator`, `attribute`, `branch`, `struct`, `property`,
  `procedure`, `iostruct`) were string constants, described in the code as a "non-exhaustive list".
  The node types `ro`, `rw`, `symlink` and `typedef` had no constants.
- Callers compared node types with `==` against the constants (`utils.ACTUATOR`, `utils.PROCEDURE`, ...), and
  `VSSgetType()` returned the string, which was also used directly in JSON output.
- The default value was stored in `DefaultValue`.

### Binary format and reading

One node per record, written depth-first (`traverseAndWriteNode` / `traverseAndReadNode`): length-prefixed
name, type, uuid, description (uint16 length), datatype, min, max, unit, allowed (hex-length encoded elements),
default, validate, and the number of children. The format is the same for all HIM profiles.

The reader was hardened compared to the original vss-tools code: errors are propagated instead of ignored
(`io.ReadFull`), tree depth and node count are capped (`MAX_TREE_DEPTH`, `MAX_TREE_NODES`), the allowed-values
string is validated, all file handling of the shared `treeFp` is serialized by `treeFpMu`, and a failed parse returns
`nil` so that `InitForest` fails. The datatype bytes were always consumed, but only stored when the node was not
of type `branch`.

### Searching

`VSSsearchNodes(path, root, maxFound, anyDepth, leafNodesOnly, ...)` does a recursive descent with `*` wildcard
support (including speculative matching), collecting `SearchData_t{NodePath, NodeHandle}` and the maximum access
control `validate` value along the matched paths.

With `leafNodesOnly == true` every node whose type was not `branch` counted as a leaf. As a consequence `struct`,
`procedure` and `iostruct` nodes, which only contain other nodes, were reported as leaves (for instance by the
path list and by the Data profile get request). The same function produced the path list files
(`VSSGetLeafNodesList`) and the default value list (`VSSGetDefaultList`).

`VSSgetDatatype()` returned the datatype field for every node, while `VSSgetUnit()` and
`VSSgetNumOfAllowedElements()` returned an empty value/zero only for `branch` and `struct` nodes.

### Users

`vissv2server.go` (Data profile get/set/metadata, struct verification, file transfer), `vissServiceMgr/` (Service
profile: `procedure`, `iostruct`, `property` handling), `atServer/` and `setvalidate.go` (set value validation).

## After the refactor

The parser and data model are now in line with the vss-tools `binary/go_parser` (`datamodel` + `parserlib`) from the
`him-support` branch, which supports the node types of all HIM profiles. The code is vendored into `utils` (no
external module dependency), with the VISSR hardening and forest handling kept.

| Area | Before | After |
|------|--------|-------|
| `Node_t.NodeType` | `string` | `NodeTypes_t` (uint8), constants for all HIM node types: `SENSOR, ACTUATOR, ATTRIBUTE, BRANCH, STRUCT, PROPERTY, RO, RW, PROCEDURE, IOSTRUCT, SYMLINK, TYPEDEF` |
| Type string mapping | none (string used as is) | `StringToNodetype()` / `NodetypeToString()`; an unknown type in a binary file is a parse error |
| Default value field | `DefaultValue` | `DefaultAllowed` (name used by vss-tools) |
| `VSSgetType()` | returns `string` | returns `NodeTypes_t`; use `NodetypeToString()` for JSON/log output |
| Leaf node test | type != `branch` | `IsContainerType()` is false, i.e. `branch`, `struct`, `procedure` and `iostruct` are never leaves |
| Datatype/unit/allowed | not for `branch` / `struct` (unit, allowed only) | not for container types and `symlink` (`IsTypeWithoutDatatype()`), for all three accessors |
| `NewSignalNode()` | node type as `string` | node type as `NodeTypes_t` |

Behavior change to be aware of: `struct`, `procedure` and `iostruct` nodes are no longer included in leaf
searches (path list files, Data profile get). Searches that are not limited to leaf nodes return the same nodes as before.

Not changed: binary file format, `viss.him` format, forest handling, search algorithm, the hardening, and the
exported function names (other than the type changes above).
