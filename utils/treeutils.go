/**
* (C) 2024 Ford Motor Company
* (C) 2020 Geotab Inc
*
* All files and artifacts in this repository are licensed under the
* provisions of the license provided by the LICENSE file in this repository.
*
*
* Go binary VSS tree parser.
**/

package utils

import (
	"os"
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"strconv"
	"sync"
	"fmt"
)

// MAX_TREE_DEPTH caps recursion in traverseAndReadNode so a crafted
// VSS binary file can't blow the stack via deeply-nested branches.
// Modern production VSS trees max out around 10-15 levels.
const MAX_TREE_DEPTH = 64

// MAX_TREE_NODES caps the total nodes read from a binary VSS file.
// A pathological branching-factor-255 file could otherwise eat
// gigabytes of memory before the parse finishes.
const MAX_TREE_NODES = 200_000

// treeFp is the file handle reused across VSSReadTree / VSSWriteTree
// / CreatePathListFile / PopulateDefault. The mutex guards against
// concurrent calls clobbering it. Long-term these helpers should
// thread a *os.File through their call chains; the mutex is the
// minimum-invasive fix.
var treeFp *os.File
var treeFpMu sync.Mutex

type HimTree struct {
	RootName string
	Handle *Node_t
	TreeType string
	Domain string
	Version string
	FileName string
	Description string
}

var himForest []HimTree

type LeafPathList struct {
	LeafPaths []string
}

type ReadTreeMetadata_t struct {
    CurrentDepth int
    MaxTreeDepth int
    TotalNodes int
}
var readTreeMetadata ReadTreeMetadata_t

var isGetLeafNodeList bool
var isGetDefaultList bool

const MAXFOUNDNODES = 1500
type SearchData_t struct {
    NodePath string
    NodeHandle *Node_t
}

type SearchContext_t struct {
	RootNode *Node_t
	SwitchName bool
	RootNodeName string
	MaxFound int
	LeafNodesOnly bool
	MaxDepth int
	SearchPath string
	MatchPath string
	CurrentDepth int  // depth in tree from rootNode, and also depth (in segments) in searchPath
	SpeculationIndex int  // inc/dec when pathsegment in focus is wildcard
	SpeculativeMatches [20]int  // inc when matching node is saved
	MaxValidation int
	NumOfMatches int
	SearchData []SearchData_t
	ListSize int
	NoScopeList []string
	ListFp *os.File
}

// Access control values: none=0, write-only=1. read-write=2, consent +=10
// matrix preserving inherited value with read-write having priority over write-only and consent over no consent
//var validationMatrix [5][5]int = [5][5]int{{0,1,2,11,12}, {1,1,2,11,12}, {2,2,2,12,12}, {11,11,12,11,12}, {12,12,12,12,12}}

const (   // non-exhaustive list of node types
    SENSOR = "sensor"
    ACTUATOR = "actuator"
    ATTRIBUTE = "attribute"
    BRANCH = "branch"
    STRUCT = "struct"
    PROPERTY = "property"
    PROCEDURE = "procedure"
    IOSTRUCT = "iostruct"
)

type Node_t struct {
    Name string
    NodeType string
    Uuid string
    Description string
    Datatype string
    Min string
    Max string
    Unit string
    Allowed uint8
    AllowedDef []string
    DefaultValue string
    Validate uint8
    Children uint8
    Parent *Node_t
    Child []*Node_t
}

func ValidateToInt(validate string) uint8 {
    validation := (uint8)(0)
    if strings.Contains(validate, "write-only") {
        validation = 1
    } else if strings.Contains(validate, "read-write") {
        validation = 2
    }
    if strings.Contains(validate, "consent") {
        validation += 10
    }
    return validation
}

func ValidateToString(validate uint8) string {
    var validation string
    if (validate%10 == 1) {
        validation = "write-only"
    }
    if (validate%10 == 2) {
        validation = "read-write"
    }
    if (validate/10 == 1) {
        validation = "+consent"
    }
    return validation
}

func getMaxValidation(newValidation int, currentMaxValidation int) int {
	return validationMatrix[translateToMatrixIndex(newValidation)][translateToMatrixIndex(currentMaxValidation)]
}

/*func translateToMatrixIndex(index int) int {
	switch index {
		case 0: return 0
		case 1: return 1
		case 2: return 2
		case 11: return 3
		case 12: return 4
	}
	return 0
}*/

func InitForest(himPath string) bool {
	file, err := os.Open(himPath)
	if err != nil {
		Error.Printf("Error reading %s: %s", himPath, err)
		return false
	}
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var text string
	continueScan := true
	i := -1
	for continueScan {
		continueScan = scanner.Scan()
		text = scanner.Text()
		if len(text) == 0 {
			continue
		}
		rootIndex := strings.Index(text, "HIM.") + 4
		if rootIndex != 3 && text[len(text)-1] == ':' {
			i++
			himForest = append(himForest, HimTree{})
			himForest[i].RootName = text[rootIndex:len(text)-1]
		} else if text[0] != '#' && strings.Contains(text, "type:") && i != -1 {
			typeIndex := strings.Index(text, "type:") + 5
			himForest[i].TreeType = strings.TrimSpace(text[typeIndex:len(text)])
		} else if text[0] != '#' && strings.Contains(text, "domain:") {
			domainIndex := strings.Index(text, "domain:") + 7
			himForest[i].Domain = strings.TrimSpace(text[domainIndex:len(text)])
		} else if text[0] != '#' && strings.Contains(text, "version:") {
			versionIndex := strings.Index(text, "version:") + 8
			himForest[i].Version = strings.TrimSpace(text[versionIndex:len(text)])
		} else if text[0] != '#' && strings.Contains(text, "local:") {
			localIndex := strings.Index(text, "local:") + 6
			localPath := strings.TrimSpace(text[localIndex:len(text)])
			himForest[i].Handle = VSSReadTree(localPath)
			if himForest[i].Handle == nil {
				Error.Printf("Error parsing %s", localPath)
				return false
			}
			himForest[i].Handle.Name = himForest[i].RootName
		}
		
	}
	file.Close()
	return true
}

func SetRootNodePointer(rootPath string) *Node_t {
	dotIndex := GetFirstDotIndex(rootPath)
	rootNodeName := rootPath[:dotIndex]
	for i:=0; i < len(himForest); i++ {
		if himForest[i].RootName == rootNodeName {
			return himForest[i].Handle
		}
	}
	return nil
}

func GetInfoType(treeHandle *Node_t) string {
	for i:=0; i < len(himForest); i++ {
		if himForest[i].Handle == treeHandle {
			infoTypeIndex := strings.LastIndex(himForest[i].Domain, ".")
			if infoTypeIndex == -1 {
				return "Missing" //???
			} else {
				return himForest[i].Domain[infoTypeIndex+1:]
			}
		}
	}
	return "Missing" //???
}

// RegisterServiceTree adds a dynamically-built service tree to the HIM forest.
// Called by vissServiceMgr when a service process registers its procedure path.
// domain must end in ".Service" so GetInfoType returns "Service".
// Returns false if a tree for rootName already exists (no double-registration).
func RegisterServiceTree(rootName, domain, version string, root *Node_t) bool {
	for i := 0; i < len(himForest); i++ {
		if himForest[i].RootName == rootName {
			return false
		}
	}
	root.Name = rootName
	himForest = append(himForest, HimTree{
		RootName: rootName,
		Domain:   domain,
		Version:  version,
		Handle:   root,
	})
	return true
}

// DeregisterServiceTree removes a dynamically-registered service tree by rootName.
func DeregisterServiceTree(rootName string) {
	for i := 0; i < len(himForest); i++ {
		if himForest[i].RootName == rootName {
			himForest = append(himForest[:i], himForest[i+1:]...)
			return
		}
	}
}

// NewBranchNode creates a branch Node_t with the given name and children.
func NewBranchNode(name string, children ...*Node_t) *Node_t {
	n := &Node_t{Name: name, NodeType: BRANCH, Children: uint8(len(children))}
	if len(children) > 0 {
		n.Child = make([]*Node_t, len(children))
		for i, c := range children {
			n.Child[i] = c
			c.Parent = n
		}
	}
	return n
}

// NewProcedureNode creates a procedure Node_t for a VISSv3.2 service procedure.
func NewProcedureNode(name, description string, children ...*Node_t) *Node_t {
	n := &Node_t{Name: name, NodeType: PROCEDURE, Description: description, Children: uint8(len(children))}
	if len(children) > 0 {
		n.Child = make([]*Node_t, len(children))
		for i, c := range children {
			n.Child[i] = c
			c.Parent = n
		}
	}
	return n
}

// NewIoStructNode creates an iostruct Node_t (Input or Output container).
func NewIoStructNode(name string, children ...*Node_t) *Node_t {
	n := &Node_t{Name: name, NodeType: IOSTRUCT, Children: uint8(len(children))}
	if len(children) > 0 {
		n.Child = make([]*Node_t, len(children))
		for i, c := range children {
			n.Child[i] = c
			c.Parent = n
		}
	}
	return n
}

// NewPropertyNode creates a property Node_t for a service parameter.
func NewPropertyNode(name, datatype, description string) *Node_t {
	return &Node_t{Name: name, NodeType: PROPERTY, Datatype: datatype, Description: description}
}

// NewSignalNode creates a signal Node_t (sensor, actuator, or attribute).
// nodeType must be one of the SENSOR, ACTUATOR, ATTRIBUTE constants.
func NewSignalNode(name, nodeType, datatype, description, min, max, unit string) *Node_t {
	return &Node_t{
		Name:        name,
		NodeType:    nodeType,
		Datatype:    datatype,
		Description: description,
		Min:         min,
		Max:         max,
		Unit:        unit,
	}
}

func CreatePathListFile(pListPath string) {
	j := 1
	for i:=0; i < len(himForest); i++ {
		if himForest[i].Handle != nil {
			pListFile := pListPath + "pathlist" + strconv.Itoa(j) + ".json"
			os.Remove(pListFile)
			VSSGetLeafNodesList(himForest[i].Handle, himForest[i].RootName, pListFile)
			sortPathList(pListFile)
			Info.Printf(pListFile + " created.")
			j++
		}
	}
}

func PopulateDefault() {
	j := 1
	for i:=0; i < len(himForest); i++ {
		if himForest[i].Handle != nil {
			dListFile := "defaultList" + strconv.Itoa(j) + ".json"
			numOfDefaults := VSSGetDefaultList(himForest[i].Handle, himForest[i].RootName, dListFile)
			if numOfDefaults == 0 {
				os.Remove(dListFile)
			} else {
				Info.Printf(dListFile + " created with %d defaults.", numOfDefaults)
				j++
			}
		}
	}
}

func sortPathList(listFname string) {
	data, err := os.ReadFile(listFname)
	if err != nil {
		Error.Printf("Error reading %s: %s", listFname, err)
		return
	}
	var pathList LeafPathList

	err = json.Unmarshal([]byte(data), &pathList)
	if err != nil {
		Error.Printf("Error unmarshal json=%s", err)
		return
	}
	sort.Strings(pathList.LeafPaths)
	file, _ := json.Marshal(pathList)
	_ = os.WriteFile(listFname, file, 0644)
}

func initReadMetadata() {
	readTreeMetadata.CurrentDepth = 0
	readTreeMetadata.MaxTreeDepth = 0
	readTreeMetadata.TotalNodes = 0
}

func updateReadMetadata(increment bool) {
	if (increment == true) {
		readTreeMetadata.TotalNodes++
		readTreeMetadata.CurrentDepth++
		if (readTreeMetadata.CurrentDepth > readTreeMetadata.MaxTreeDepth) {
			readTreeMetadata.MaxTreeDepth++
		}
	} else {
		readTreeMetadata.CurrentDepth--
	}
}

func printReadMetadata() {
	fmt.Printf("\nTotal number of nodes in VSS tree = %d\n", readTreeMetadata.TotalNodes)
	fmt.Printf("Max depth of VSS tree = %d\n", readTreeMetadata.MaxTreeDepth)
}

// allowed string has format "XXallowed1XXallowed2...XXallowedx",
// where XX are hex values; X=[0-9,A-F]. Returns the count of
// elements, or an error if the format is malformed.
//
// Bug-12 fix: the previous countAllowedElements / extractAllowedElement
// blindly trusted the hex bytes from the file. A non-hex byte (e.g.
// a space or lowercase letter) made hexToInt return a negative or
// large number, which then made the index walk past the buffer end
// and panic on the next slice. Both helpers now validate every hex
// digit and bounds-check every slice operation.
func countAllowedElements(allowedStr string) int {
	n, _ := countAllowedElementsE(allowedStr)
	return n
}

func countAllowedElementsE(allowedStr string) (int, error) {
	nrOfAllowed := 0
	index := 0
	for index < len(allowedStr) {
		if index+2 > len(allowedStr) {
			return 0, fmt.Errorf("allowed buffer truncated at index %d (need 2 hex bytes for length prefix)", index)
		}
		allowedLen, err := decodeAllowedLen(allowedStr[index], allowedStr[index+1])
		if err != nil {
			return 0, fmt.Errorf("at index %d: %w", index, err)
		}
		next := index + allowedLen + 2
		if next > len(allowedStr) {
			return 0, fmt.Errorf("allowed element at index %d declares len=%d but buffer ends at %d", index, allowedLen, len(allowedStr))
		}
		index = next
		nrOfAllowed++
	}
	return nrOfAllowed, nil
}

// decodeAllowedLen decodes two hex digits into the [0,255] length
// they encode. Validates that each byte is a real hex digit and
// returns an error otherwise.
func decodeAllowedLen(hi, lo byte) (int, error) {
	h, err := hexToIntStrict(hi)
	if err != nil {
		return 0, err
	}
	l, err := hexToIntStrict(lo)
	if err != nil {
		return 0, err
	}
	return h*16 + l, nil
}

// hexToInt remains the legacy (unchecked) variant — kept for
// backward compat with any caller that wants the old behaviour.
// New code should use hexToIntStrict.
func hexToInt(hexDigit byte) int {
	if hexDigit <= '9' {
		return int(hexDigit - '0')
	}
	return int(hexDigit - 'A' + 10)
}

// hexToIntStrict validates the input is one of [0-9A-F] before
// decoding. Lowercase a-f is also accepted for robustness. Returns
// an error on any other byte.
func hexToIntStrict(hexDigit byte) (int, error) {
	switch {
	case hexDigit >= '0' && hexDigit <= '9':
		return int(hexDigit - '0'), nil
	case hexDigit >= 'A' && hexDigit <= 'F':
		return int(hexDigit-'A') + 10, nil
	case hexDigit >= 'a' && hexDigit <= 'f':
		return int(hexDigit-'a') + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex digit %q (0x%02x)", hexDigit, hexDigit)
	}
}

func intToHex(intVal int) []byte {
	if intVal < 0 || intVal > 255 {
		// Bug note (related to but not part of #12): the original
		// returned nil here, which the writer then passed straight
		// to treeFp.Write(nil) — silently corrupting the output by
		// dropping a length byte. Return two zero hex digits ("00")
		// so the writer always produces a syntactically valid
		// length, and log loudly. Callers that produce lengths
		// >255 must change to a wider length field.
		Error.Printf("intToHex: value %d out of [0,255] range; writing 00 instead", intVal)
		return []byte{'0', '0'}
	}
	hexVal := make([]byte, 2)
	hexVal[0] = hexDigit(intVal / 16)
	hexVal[1] = hexDigit(intVal % 16)
	return hexVal
}

func hexDigit(value int) byte {
	if value < 10 {
		return byte(value + '0')
	}
	return byte(value - 10 + 'A')
}

func extractAllowedElement(allowedBuf string, elemIndex int) string {
	s, _ := extractAllowedElementE(allowedBuf, elemIndex)
	return s
}

func extractAllowedElementE(allowedBuf string, elemIndex int) (string, error) {
	if elemIndex < 0 {
		return "", fmt.Errorf("negative element index %d", elemIndex)
	}
	bufIndex := 0
	for alloweds := 0; alloweds <= elemIndex; alloweds++ {
		if bufIndex+2 > len(allowedBuf) {
			return "", fmt.Errorf("element %d: buffer truncated at %d (need 2 hex bytes for length)", alloweds, bufIndex)
		}
		allowedLen, err := decodeAllowedLen(allowedBuf[bufIndex], allowedBuf[bufIndex+1])
		if err != nil {
			return "", fmt.Errorf("element %d: %w", alloweds, err)
		}
		allowedStart := bufIndex + 2
		allowedEnd := allowedStart + allowedLen
		if allowedEnd > len(allowedBuf) {
			return "", fmt.Errorf("element %d declares len=%d but buffer ends at %d", alloweds, allowedLen, len(allowedBuf))
		}
		if alloweds == elemIndex {
			return allowedBuf[allowedStart:allowedEnd], nil
		}
		bufIndex = allowedEnd
	}
	return "", fmt.Errorf("element %d not found", elemIndex)
}

// traverseAndReadNode recursively reads nodes from treeFp into an
// in-memory tree. Returns an error on:
//   - short read / EOF / I/O error (bug-1 fix: readBytesE)
//   - depth exceeding MAX_TREE_DEPTH (bug-2 fix)
//   - total node count exceeding MAX_TREE_NODES (bug-3 fix)
// On error, the partially-built tree is discarded by the caller.
func traverseAndReadNode(parentNode *Node_t) (*Node_t, error) {
	updateReadMetadata(true)
	defer updateReadMetadata(false)
	if readTreeMetadata.CurrentDepth > MAX_TREE_DEPTH {
		return nil, fmt.Errorf("VSS tree depth exceeds MAX_TREE_DEPTH=%d (refusing potentially malicious input)", MAX_TREE_DEPTH)
	}
	if readTreeMetadata.TotalNodes > MAX_TREE_NODES {
		return nil, fmt.Errorf("VSS tree node count exceeds MAX_TREE_NODES=%d (refusing potentially malicious input)", MAX_TREE_NODES)
	}
	var thisNode Node_t
	if err := populateNode(&thisNode); err != nil {
		return nil, fmt.Errorf("populateNode at depth=%d node=%d: %w", readTreeMetadata.CurrentDepth, readTreeMetadata.TotalNodes, err)
	}
	thisNode.Parent = parentNode
	if thisNode.Children > 0 {
		thisNode.Child = make([]*Node_t, thisNode.Children)
	}
	var childNo uint8
	for childNo = 0; childNo < thisNode.Children; childNo++ {
		child, err := traverseAndReadNode(&thisNode)
		if err != nil {
			return nil, err
		}
		thisNode.Child[childNo] = child
	}
	return &thisNode, nil
}

func traverseAndWriteNode(node *Node_t) {
	writeNode(node)
	var childNo uint8
	for childNo = 0 ; childNo < node.Children ; childNo++ {
		traverseAndWriteNode(node.Child[childNo])
	}
}

func traverseNode(thisNode *Node_t, context *SearchContext_t) int {
	speculationSucceded := 0
	if thisNode == nil { // bug-11 defensive guard
		return 0
	}
	incDepth(thisNode, context)
	// CurrentDepth=1 tested as rootNode name is already verified, and does not have to match
	if context.CurrentDepth == 1 || compareNodeName(VSSgetName(thisNode), getPathSegment(0, context)) {
		var done bool
		speculationSucceded = saveMatchingNode(thisNode, context, &done)
		if done == false {
			numOfChildren := VSSgetNumOfChildren(thisNode)
			childPathName := getPathSegment(1, context)
			for i := 0; i < numOfChildren; i++ {
				child := VSSgetChild(thisNode, i)
				if child == nil { // bug-11: skip nil children from a partially-populated tree
					continue
				}
				if compareNodeName(VSSgetName(child), childPathName) {
					speculationSucceded += traverseNode(child, context)
				}
			}
		}
	}
	decDepth(speculationSucceded, context)
	return speculationSucceded
}

func saveMatchingNode(thisNode *Node_t, context *SearchContext_t, done *bool) int {
	if (getPathSegment(0, context) == "*") {
		context.SpeculationIndex++
	}
	context.MaxValidation = getMaxValidation(VSSgetValidation(thisNode), context.MaxValidation)
	if (VSSgetType(thisNode) != BRANCH || context.LeafNodesOnly == false) {
		if ( isGetLeafNodeList == false && isGetDefaultList == false) {
			context.SearchData[context.NumOfMatches].NodePath = context.MatchPath
			context.SearchData[context.NumOfMatches].NodeHandle = thisNode
		} else {
			if (isGetLeafNodeList == true) {
			    if (context.NumOfMatches == 0) {
				    context.ListFp.Write([]byte("\""))
			    } else {
				    context.ListFp.Write([]byte(", \""))
			    }
			    context.ListFp.Write([]byte(context.MatchPath))
			    context.ListFp.Write([]byte("\""))
			} else {
			    defaultValue := VSSgetDefault(thisNode)
			    if len(defaultValue) == 0 {
				context.NumOfMatches--  // even out the inc below
			    } else {
				    if (context.NumOfMatches == 0) {
					    context.ListFp.Write([]byte("{\"path\":\""))
				    } else {
					    context.ListFp.Write([]byte(", {\"path\":\""))
				    }
				    context.ListFp.Write([]byte(context.MatchPath))
				    context.ListFp.Write([]byte("\", \"default\":\""))
				    context.ListFp.Write([]byte(defaultValue))
				    context.ListFp.Write([]byte("\"}"))
			    }
			}
		}
		context.NumOfMatches++
		if (context.SpeculationIndex >= 0) {
			context.SpeculativeMatches[context.SpeculationIndex]++
		}
	}
	if (VSSgetNumOfChildren(thisNode) == 0 || context.CurrentDepth == context.MaxDepth  || isEndOfScope(context) == true) {
		*done = true
	} else {
		*done = false
	}
	if (context.SpeculationIndex >= 0 && ((VSSgetNumOfChildren(thisNode) == 0 && context.CurrentDepth >= countSegments(context.SearchPath)) || context.CurrentDepth == context.MaxDepth)) {
		return 1
	}
	return 0
}

func GetFirstDotIndex(path string) int {  // points to the dot
	dotIndex := strings.Index(path, ".")
	if dotIndex == -1 {
		dotIndex = len(path) // only root name in path
	}
	return dotIndex
}

func GetLastDotSegment(path string) string {
	dotIndex := strings.LastIndex(path, ".")
	if dotIndex == len(path) - 1 {
		return ""  // should not happen...
	}
	return path[dotIndex+1:]
}

func isEndOfScope(context *SearchContext_t) bool {
    if (context.ListSize == 0) {
        return false
    }
    for i := 0 ; i < context.ListSize ; i++ {
        if (context.MatchPath == context.NoScopeList[i]) {
            return true
        }
    }
    return false
}

func compareNodeName(nodeName string, pathName string) bool {
	//fmt.Printf("compareNodeName(): nodeName=%s, pathName=%s\n", nodeName, pathName)
	if (nodeName == pathName || pathName == "*") {
		return true
	}
	return false
}

func pushPathSegment(name string, context *SearchContext_t) {
	if (context.CurrentDepth > 0) {
		context.MatchPath += "."
	}
	context.MatchPath += name
}

func popPathSegment(context *SearchContext_t) {
	delim := strings.LastIndex(context.MatchPath, ".")
	if (delim == -1) {
		context.MatchPath = ""
	} else {
		context.MatchPath = context.MatchPath[:delim]
	}
}

func getPathSegment(offset int, context *SearchContext_t) string {
	frontDelimiter := 0
	for i := 1 ; i < context.CurrentDepth + offset ; i++ {
		frontDelimiter += strings.Index(context.SearchPath[frontDelimiter+1:], ".") + 1
		if (frontDelimiter == -1) {
			if (context.SearchPath[len(context.SearchPath)-1] == '*' && context.CurrentDepth < context.MaxDepth) {
				return "*"
			} else {
				return ""
			}
		}
	}
	endDelimiter := strings.Index(context.SearchPath[frontDelimiter+1:], ".") + frontDelimiter + 1
	if (endDelimiter == frontDelimiter) {
		endDelimiter = len(context.SearchPath)
	}
	if (context.SearchPath[frontDelimiter] == '.') {
		frontDelimiter++
	}
	return context.SearchPath[frontDelimiter:endDelimiter]
}

func incDepth(thisNode *Node_t, context *SearchContext_t) {
	pushPathSegment(VSSgetName(thisNode), context)
	context.CurrentDepth++
}

/**
 * decDepth() shall reverse speculative wildcard matches that have failed, and also decrement currentDepth.
 **/
func decDepth(speculationSucceded int, context *SearchContext_t) {
	//fmt.Printf("decDepth():speculationSucceded=%d\n", speculationSucceded)
	if (context.SpeculationIndex >= 0 && context.SpeculativeMatches[context.SpeculationIndex] > 0) {
		if (speculationSucceded == 0) {  // it failed so remove a saved match
			context.NumOfMatches--
			context.SpeculativeMatches[context.SpeculationIndex]--
		}
	}
	if (getPathSegment(0, context) == "*") {
		context.SpeculationIndex--
	}
	popPathSegment(context)
	context.CurrentDepth--
}

// readBytes is kept for callers that don't care about errors (legacy).
// New code should use readBytesE which propagates short-read / EOF
// errors. (Bug-1: the original silently ignored io errors.)
func readBytes(numOfBytes uint32) []byte {
	b, _ := readBytesE(numOfBytes)
	return b
}

// readBytesE reads exactly numOfBytes from treeFp using io.ReadFull,
// so a truncated file produces an error instead of zero-padded
// silent corruption. (Bug-1 fix.)
func readBytesE(numOfBytes uint32) ([]byte, error) {
	if numOfBytes == 0 {
		return nil, nil
	}
	if treeFp == nil {
		return nil, errors.New("readBytesE: treeFp is nil (parser called without an open file)")
	}
	buf := make([]byte, numOfBytes)
	if _, err := io.ReadFull(treeFp, buf); err != nil {
		return nil, fmt.Errorf("readBytesE: %w", err)
	}
	return buf, nil
}

// readUint8 reads a single byte as uint8 with error propagation.
func readUint8() (uint8, error) {
	b, err := readBytesE(1)
	if err != nil {
		return 0, err
	}
	return deSerializeUInt(b).(uint8), nil
}

// readUint16 reads two bytes as uint16 with error propagation.
func readUint16() (uint16, error) {
	b, err := readBytesE(2)
	if err != nil {
		return 0, err
	}
	return deSerializeUInt(b).(uint16), nil
}

// readLenPrefixedString reads a uint8 length prefix and then that
// many bytes, returning them as a string. Error propagated.
func readLenPrefixedString8() (string, error) {
	n, err := readUint8()
	if err != nil {
		return "", err
	}
	b, err := readBytesE(uint32(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// The reading order must be synchronized with the writing order in the binary tool
func populateNode(thisNode *Node_t) error {
	var err error
	if thisNode.Name, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if thisNode.NodeType, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("nodeType: %w", err)
	}
	if thisNode.Uuid, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("uuid: %w", err)
	}
	descrLen, err := readUint16()
	if err != nil {
		return fmt.Errorf("descrLen: %w", err)
	}
	descrBytes, err := readBytesE(uint32(descrLen))
	if err != nil {
		return fmt.Errorf("description: %w", err)
	}
	thisNode.Description = string(descrBytes)

	// Bug-6 fix: read the datatype bytes UNCONDITIONALLY. The
	// original code only consumed the bytes when NodeType != BRANCH,
	// but the writer always writes them (just usually empty for
	// BRANCH). A malformed/malicious tree where a BRANCH had a
	// non-empty datatype length would desync the read stream and
	// turn the rest of the file into garbage. Always consuming the
	// declared bytes keeps reader/writer symmetric.
	datatypeLen, err := readUint8()
	if err != nil {
		return fmt.Errorf("datatypeLen: %w", err)
	}
	datatypeBytes, err := readBytesE(uint32(datatypeLen))
	if err != nil {
		return fmt.Errorf("datatype: %w", err)
	}
	if thisNode.NodeType != BRANCH {
		thisNode.Datatype = string(datatypeBytes)
	}

	if thisNode.Min, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("min: %w", err)
	}
	if thisNode.Max, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("max: %w", err)
	}
	if thisNode.Unit, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("unit: %w", err)
	}

	allowedStrLen, err := readUint16()
	if err != nil {
		return fmt.Errorf("allowedStrLen: %w", err)
	}
	allowedBytes, err := readBytesE(uint32(allowedStrLen))
	if err != nil {
		return fmt.Errorf("allowedStr: %w", err)
	}
	allowedStr := string(allowedBytes)
	count, err := countAllowedElementsE(allowedStr)
	if err != nil {
		return fmt.Errorf("countAllowedElements: %w", err)
	}
	thisNode.Allowed = uint8(count)
	if thisNode.Allowed > 0 {
		thisNode.AllowedDef = make([]string, thisNode.Allowed)
	}
	for i := 0; i < int(thisNode.Allowed); i++ {
		elem, err := extractAllowedElementE(allowedStr, i)
		if err != nil {
			return fmt.Errorf("extractAllowedElement[%d]: %w", i, err)
		}
		thisNode.AllowedDef[i] = elem
	}

	if thisNode.DefaultValue, err = readLenPrefixedString8(); err != nil {
		return fmt.Errorf("default: %w", err)
	}

	validate, err := readLenPrefixedString8()
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	thisNode.Validate = ValidateToInt(validate)

	if thisNode.Children, err = readUint8(); err != nil {
		return fmt.Errorf("children: %w", err)
	}

	return nil
}

// The reading order must be synchronized with the writing order in the binary tool
func writeNode(thisNode *Node_t) {
    treeFp.Write(serializeUInt((uint8)(len(thisNode.Name))))
    treeFp.Write([]byte(thisNode.Name))

    treeFp.Write(serializeUInt((uint8)(len(thisNode.NodeType))))
    treeFp.Write([]byte(thisNode.NodeType))

    treeFp.Write(serializeUInt((uint8)(len(thisNode.Uuid))))
    treeFp.Write([]byte(thisNode.Uuid))

    treeFp.Write(serializeUInt((uint16)(len(thisNode.Description))))
    treeFp.Write([]byte(thisNode.Description))

    treeFp.Write(serializeUInt((uint8)(len(thisNode.Datatype))))
    if (len(thisNode.Datatype) > 0) {
        treeFp.Write([]byte(thisNode.Datatype))
    }

    treeFp.Write(serializeUInt((uint8)(len(thisNode.Min))))
    if (len(thisNode.Min) > 0) {
        treeFp.Write([]byte(thisNode.Min))
    }

    treeFp.Write(serializeUInt((uint8)(len(thisNode.Max))))
    if (len(thisNode.Max) > 0) {
        treeFp.Write([]byte(thisNode.Max))
    }

    treeFp.Write(serializeUInt((uint8)(len(thisNode.Unit))))
    if (len(thisNode.Unit) > 0) {
        treeFp.Write([]byte(thisNode.Unit))
    }

    // Bug-8 fix: iterate len(AllowedDef) on both sides instead of
    // mixing len(AllowedDef) for the length prefix and Allowed for
    // the loop. If those two ever disagreed (manual node
    // construction), the writer wrote the wrong number of bytes
    // and the reader desynced.
    allowedStrLen := calculatAllowedStrLen(thisNode.AllowedDef)
    treeFp.Write(serializeUInt((uint16)(allowedStrLen)))
    for i := 0; i < len(thisNode.AllowedDef); i++ {
        allowedWrite(thisNode.AllowedDef[i])
    }

    treeFp.Write(serializeUInt((uint8)(len(thisNode.DefaultValue))))
    if (len(thisNode.DefaultValue) > 0) {
        treeFp.Write([]byte(thisNode.DefaultValue))
    }

    Validate := ValidateToString(thisNode.Validate)
    treeFp.Write(serializeUInt((uint8)(len(Validate))))
    if (len(Validate) > 0) {
        treeFp.Write([]byte(Validate))
    }

    treeFp.Write(serializeUInt((uint8)(thisNode.Children)))

//    fmt.Printf("writeNode: %s\n", thisNode.Name)
}

func calculatAllowedStrLen(allowedDef []string) int {
    strLen := 0
    for i := 0 ; i < len(allowedDef) ; i++ {
        strLen += len(allowedDef[i]) + 2
    }
    return strLen
}

func allowedWrite(allowed string) {
    treeFp.Write(intToHex(len(allowed)))
//fmt.Printf("allowedHexLen: %s\n", string(intToHex(len(allowed))))
    treeFp.Write([]byte(allowed))
}

func serializeUInt(intVal interface{}) []byte {
    switch intVal.(type) {
      case uint8:
        buf := make([]byte, 1)
        buf[0] = intVal.(byte)
        return buf
      case uint16:
        buf := make([]byte, 2)
        buf[1] = byte((intVal.(uint16) & 0xFF00)/256)
        buf[0] = byte(intVal.(uint16) & 0x00FF)
        return buf
      case uint32:
        buf := make([]byte, 4)
        buf[3] = byte((intVal.(uint32) & 0xFF000000)/16777216)
        buf[2] = byte((intVal.(uint32) & 0xFF0000)/65536)
        buf[1] = byte((intVal.(uint32) & 0xFF00)/256)
        buf[0] = byte(intVal.(uint32) & 0x00FF)
        return buf
      default:
        fmt.Println(intVal, "is of an unknown type")
        return nil
    }
}

func deSerializeUInt(buf []byte) interface{} {
    switch len(buf) {
      case 1:
        var intVal uint8
        intVal = (uint8)(buf[0])
        return intVal
      case 2:
        var intVal uint16
        intVal = (uint16)((uint16)((uint16)(buf[1])*256) + (uint16)(buf[0]))
        return intVal
      case 4:
        var intVal uint32
        intVal = (uint32)((uint32)((uint32)(buf[3])*16777216) + (uint32)((uint32)(buf[2])*65536) + (uint32)((uint32)(buf[1])*256) + (uint32)(buf[0]))
        return intVal
      default:
        fmt.Printf("Buffer length=%d is of an unknown size", len(buf))
        return nil
    }
}

func countSegments(path string) int {
    count := strings.Count(path, ".")
    return count + 1
}

func initContext(context *SearchContext_t, searchPath string, rootNode *Node_t, maxFound int, searchData []SearchData_t, anyDepth bool, leafNodesOnly bool, listSize int, noScopeList []string) {
	context.SearchPath = searchPath
	/*    if (anyDepth == true && context.SearchPath[len(context.SearchPath)-1] != '*') {
		  context.SearchPath = append(context.SearchPath, ".*")
		  } */
	context.RootNode = rootNode
	context.RootNodeName = searchPath[:GetFirstDotIndex(searchPath)]
	if context.RootNodeName != rootNode.Name {
		context.SwitchName = true
	} else {
		context.SwitchName = false
	}
	context.MaxFound = maxFound
	context.SearchData = searchData
	if (anyDepth == true) {
		context.MaxDepth = 100  //jan 2020 max tree depth = 8
	} else {
		context.MaxDepth = countSegments(context.SearchPath)
	}
	context.LeafNodesOnly = leafNodesOnly
	context.ListSize = listSize
 	context.NoScopeList = nil
	if (listSize > 0) {
  	    context.NoScopeList = noScopeList
	}
	context.MaxValidation = 0
	context.CurrentDepth = 0
	context.MatchPath = ""
	context.NumOfMatches = 0
	context.SpeculationIndex = -1
	for i := 0 ; i < 20 ; i++ {
		context.SpeculativeMatches[i] = 0
	}
}

func initContext_LNL(context *SearchContext_t, searchPath string, rootNode *Node_t, anyDepth bool, leafNodesOnly bool, listSize int, noScopeList []string) {
	context.SearchPath = searchPath
	context.RootNode = rootNode
	context.RootNodeName = searchPath[:GetFirstDotIndex(searchPath)]
	if context.RootNodeName != rootNode.Name {
		context.SwitchName = true
	} else {
		context.SwitchName = false
	}
	context.MaxFound = 0
	context.SearchData = nil
	context.ListFp = treeFp
	if (anyDepth == true) {
		context.MaxDepth = 100  //jan 2020 max tree depth = 8
	} else {
		context.MaxDepth = countSegments(context.SearchPath)
	}
	context.LeafNodesOnly = leafNodesOnly
	context.ListSize = listSize
 	context.NoScopeList = nil
	if (listSize > 0) {
  	    context.NoScopeList = noScopeList
	}
	context.MaxValidation = 0
	context.CurrentDepth = 0
	context.MatchPath = ""
	context.NumOfMatches = 0
	context.SpeculationIndex = -1
	for i := 0 ; i < 20 ; i++ {
		context.SpeculativeMatches[i] = 0
	}
}

func VSSsearchNodes(searchPath string, rootNode *Node_t, maxFound int, anyDepth bool, leafNodesOnly bool, listSize int, noScopeList []string, validation *int) ([]SearchData_t, int) {
	var context SearchContext_t
	searchData := make([]SearchData_t, maxFound)
	isGetLeafNodeList = false
	isGetDefaultList = false

	initContext(&context, searchPath, rootNode, maxFound, searchData, anyDepth, leafNodesOnly, listSize, noScopeList)
	traverseNode(rootNode, &context)
	if (validation != nil) {
		*validation = context.MaxValidation
	}
	return searchData, context.NumOfMatches
}

// Bug-9 + bug-10 fix: VSSGetLeafNodesList and VSSGetDefaultList both
// take the treeFpMu mutex while they own the treeFp handle AND the
// isGet* mode flags. Previously these globals were mutated without
// any synchronisation; concurrent calls (or concurrent calls with
// VSSReadTree) would clobber each other.
func VSSGetLeafNodesList(rootNode *Node_t, rootNodeName string, listFname string) int {
	treeFpMu.Lock()
	defer treeFpMu.Unlock()
	var context SearchContext_t
	isGetLeafNodeList = true
	defer func() { isGetLeafNodeList = false }()
	var err error
	treeFp, err = os.OpenFile(listFname, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open %s for writing tree data\n", listFname)
		return 0
	}
	defer treeFp.Close()
	treeFp.Write([]byte("{\"leafpaths\":["))
	initContext_LNL(&context, rootNodeName+".*", rootNode, true, true, 0, nil) // anyDepth = true, leafNodesOnly = true
	traverseNode(rootNode, &context)
	treeFp.Write([]byte("]}"))
	return context.NumOfMatches
}

func VSSGetDefaultList(rootNode *Node_t, rootNodeName string, listFname string) int {
	treeFpMu.Lock()
	defer treeFpMu.Unlock()
	var context SearchContext_t
	isGetDefaultList = true
	defer func() { isGetDefaultList = false }()
	var err error
	treeFp, err = os.OpenFile(listFname, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open %s for writing tree data\n", listFname)
		return 0
	}
	defer treeFp.Close()
	treeFp.Write([]byte("["))
	initContext_LNL(&context, rootNodeName+".*", rootNode, true, true, 0, nil) // anyDepth = true, leafNodesOnly = true
	traverseNode(rootNode, &context)
	treeFp.Write([]byte("]"))
	return context.NumOfMatches
}

// VSSReadTree opens a binary VSS tree file and returns the in-memory
// root node, or nil on any I/O / parse error (logged before return).
//
// Bug-5 fix: the previous code returned a (possibly zero-initialized)
// *Node_t even when the parse failed, because traverseAndReadNode
// didn't propagate errors. With error propagation in place we now
// return nil on parse failure so InitForest's nil-check actually
// catches corrupt files.
//
// Bug-9 fix: serialize against the treeFp global with treeFpMu, so
// a concurrent VSSWriteTree / CreatePathListFile call doesn't
// clobber the file handle mid-parse.
func VSSReadTree(fname string) *Node_t {
	treeFpMu.Lock()
	defer treeFpMu.Unlock()
	var err error
	treeFp, err = os.OpenFile(fname, os.O_RDONLY, 0644)
	if err != nil {
		fmt.Printf("Could not open %s for reading tree. Error= %s\n", fname, err)
		return nil
	}
	defer treeFp.Close()
	initReadMetadata()
	root, err := traverseAndReadNode(nil)
	if err != nil {
		fmt.Printf("VSSReadTree: parse failed: %s\n", err)
		return nil
	}
	printReadMetadata()
	return root
}

// VSSWriteTree writes root to a binary tree file. (Bug-9 fix: takes
// the treeFp mutex.)
func VSSWriteTree(fname string, root *Node_t) {
	treeFpMu.Lock()
	defer treeFpMu.Unlock()
	var err error
	treeFp, err = os.OpenFile(fname, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open %s for writing tree data\n", fname)
		return
	}
	defer treeFp.Close()
	traverseAndWriteNode(root)
}

func VSSgetName(nodeHandle *Node_t) string {
	if nodeHandle == nil { // defensive (bug-11 family)
		return ""
	}
	return nodeHandle.Name
}

func VSSgetParent(nodeHandle *Node_t) *Node_t {
	return nodeHandle.Parent
}

func VSSgetNumOfChildren(nodeHandle *Node_t) int {
	return (int)(nodeHandle.Children)
}

func VSSgetChild(nodeHandle *Node_t, childNo int) *Node_t {
	if (VSSgetNumOfChildren(nodeHandle) > childNo) {
		return nodeHandle.Child[childNo]
	}
	return nil
}

func VSSgetType(nodeHandle *Node_t) string {
	return nodeHandle.NodeType
}

func VSSgetDatatype(nodeHandle *Node_t) string {
	return nodeHandle.Datatype
}

func VSSgetUUID(nodeHandle *Node_t) string {
	return nodeHandle.Uuid
}

func VSSgetDefault(nodeHandle *Node_t) string {
	return nodeHandle.DefaultValue
}

func VSSgetValidation(nodeHandle *Node_t) int {
	return (int)(nodeHandle.Validate)
}

func VSSgetDescr(nodeHandle *Node_t) string {
	return nodeHandle.Description
}

func VSSgetNumOfAllowedElements(nodeHandle *Node_t) int {
	nodeType := VSSgetType(nodeHandle);
	if (nodeType != BRANCH && nodeType != STRUCT) {
		return (int)(nodeHandle.Allowed)
	}
	return 0
}

func VSSgetAllowedElement(nodeHandle *Node_t, index int) string {
	return nodeHandle.AllowedDef[index]
}

func VSSgetUnit(nodeHandle *Node_t) string {
	nodeType := VSSgetType(nodeHandle)
	if (nodeType != BRANCH && nodeType != STRUCT) {
		return nodeHandle.Unit
	}
	return ""
}
