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
	"sort"
	"strings"
	"strconv"
	"fmt"
)

var treeFp *os.File

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

type NodeTypes_t uint8

const (   // allowed elements of nodeTypes_t
    SENSOR = 1
    ACTUATOR = 2
    ATTRIBUTE = 3
    BRANCH = 4
    STRUCT = 5
    PROPERTY = 6
)

type Node_t struct {
    Name string
    NodeType NodeTypes_t
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

func StringToNodetype(nodeType string) uint8 {
    if (nodeType == "branch") {
        return BRANCH
    }
    if (nodeType == "sensor") {
        return SENSOR
    }
    if (nodeType == "actuator") {
        return ACTUATOR
    }
    if (nodeType == "attribute") {
        return ATTRIBUTE
    }
    if (nodeType == "struct") {
        return STRUCT
    }
    if (nodeType == "propery") {
        return PROPERTY
    }
    fmt.Printf("Unknown type! |%s|\n", nodeType);
    return 0
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

func NodetypeToString(nodeType NodeTypes_t) string {
    if (nodeType == BRANCH) {
        return "branch"
    }
    if (nodeType == SENSOR) {
        return "sensor"
    }
    if (nodeType == ACTUATOR) {
        return "actuator"
    }
    if (nodeType == ATTRIBUTE) {
        return "attribute"
    }
    if (nodeType == STRUCT) {
        return "struct"
    }
    if (nodeType == PROPERTY) {
        return "propery"
    }
    fmt.Printf("Unknown type! |%d|\n", nodeType);
    return ""
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
			sameHandle := searchForSameTree(himForest)
			if sameHandle == nil {
				himForest[i].Handle = VSSReadTree(localPath)
			} else {
				himForest[i].Handle = sameHandle
			}
			if himForest[i].Handle == nil {
				Error.Printf("Error parsing %s", localPath)
				return false
			}
		}
		
	}
	file.Close()
	return true
}

func searchForSameTree(himForest []HimTree) *Node_t {
	lastIndex := len(himForest) - 1
	for i := 0; i < lastIndex; i++ {
		if himForest[i].Domain == himForest[lastIndex].Domain && himForest[i].Version == himForest[lastIndex].Version {
			return himForest[i].Handle
		}
	}
	return nil
}

func SetRootNodePointer(rootPath string) *Node_t {
	dotIndex := getDotIndex(rootPath)
	rootNodeName := rootPath[:dotIndex]
	for i:=0; i < len(himForest); i++ {
		if himForest[i].RootName == rootNodeName {
			return himForest[i].Handle
		}
	}
	return nil
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

func countAllowedElements(allowedStr string) int {  // allowed string has format "XXallowed1XXallowed2...XXallowedx", where XX are hex values; X=[0-9,A-F]
    nrOfAllowed := 0
    index := 0
    for  index < len(allowedStr) {
        hexLen := allowedStr[index:index+2]
        allowedLen := hexToInt(byte(hexLen[0])) * 16 + hexToInt(byte(hexLen[1]))
        index += allowedLen + 2
        nrOfAllowed++
    }
    return nrOfAllowed
}

func hexToInt(hexDigit byte) int {
    if (hexDigit <= '9') {
        return (int)(hexDigit - '0')
    }
    return (int)(hexDigit - 'A' + 10)
}

func intToHex(intVal int) []byte {
    if (intVal > 255) {
        return nil
    }
    hexVal := make([]byte, 2)
    hexVal[0] = hexDigit(intVal/16)
    hexVal[1] = hexDigit(intVal%16)
    return hexVal
}

func hexDigit(value int) byte {
    if (value < 10) {
        return byte(value + '0')
    }
    return byte(value - 10 + 'A')
}

func extractAllowedElement(allowedBuf string, elemIndex int) string {
    var allowedstart int
    var allowedend int
    bufIndex := 0
    for alloweds := 0 ; alloweds <= elemIndex ; alloweds++ {
        hexLen := allowedBuf[bufIndex:bufIndex+2]
        allowedLen := hexToInt(byte(hexLen[0])) * 16 + hexToInt(byte(hexLen[1]))
        allowedstart = bufIndex + 2
        allowedend = allowedstart + allowedLen
        bufIndex += allowedLen + 2
    }
    return allowedBuf[allowedstart:allowedend]
}

func traverseAndReadNode(parentNode *Node_t) *Node_t {
	var thisNode Node_t
	updateReadMetadata(true)
	populateNode(&thisNode)
	thisNode.Parent = parentNode
	if (thisNode.Children > 0) {
               thisNode.Child = make([]*Node_t, thisNode.Children)
	}
	var childNo uint8
	for childNo = 0 ; childNo < thisNode.Children ; childNo++ {
		thisNode.Child[childNo] = traverseAndReadNode(&thisNode)
	}
	updateReadMetadata(false)
	return &thisNode
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
	incDepth(thisNode, context)
	// CurrentDepth=1 tested as rootNode name is already verified, and does not have to match
	if context.CurrentDepth == 1 || compareNodeName(VSSgetName(thisNode), getPathSegment(0, context)) {
		var done bool
		speculationSucceded = saveMatchingNode(thisNode, context, &done)
		if (done == false) {
			numOfChildren := VSSgetNumOfChildren(thisNode)
			childPathName := getPathSegment(1, context)
			for i := 0 ; i < numOfChildren ; i++ {
				if compareNodeName(VSSgetName(VSSgetChild(thisNode, i)), childPathName) {
					speculationSucceded += traverseNode(VSSgetChild(thisNode, i), context)
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
	if (VSSgetType(thisNode) != BRANCH  && VSSgetType(thisNode) != STRUCT || context.LeafNodesOnly == false) {
		if ( isGetLeafNodeList == false && isGetDefaultList == false) {
			context.SearchData[context.NumOfMatches].NodePath = rootNodeNameUpdate(context.MatchPath, context)
			context.SearchData[context.NumOfMatches].NodeHandle = thisNode
		} else {
			if (isGetLeafNodeList == true) {
			    if (context.NumOfMatches == 0) {
				    context.ListFp.Write([]byte("\""))
			    } else {
				    context.ListFp.Write([]byte(", \""))
			    }
			    context.ListFp.Write([]byte(rootNodeNameUpdate(context.MatchPath, context)))
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
				    context.ListFp.Write([]byte(rootNodeNameUpdate(context.MatchPath, context)))
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

func rootNodeNameUpdate(origPath string, context *SearchContext_t) string {
	if !context.SwitchName {
		return origPath
	}
	dotIndex := getDotIndex(origPath)
	return context.RootNodeName + origPath[dotIndex:]
}

func getDotIndex(path string) int {
	dotIndex := strings.Index(path, ".")
	if dotIndex == -1 {
		dotIndex = len(path) // only root name in path
	}
	return dotIndex
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

func readBytes(numOfBytes uint32) []byte {
	if (numOfBytes > 0) {
	    buf := make([]byte, numOfBytes)
	    treeFp.Read(buf)
	    return buf
	}
	return nil
}

// The reading order must be synchronized with the writing order in the binary tool
func populateNode(thisNode *Node_t) {
	NameLen := deSerializeUInt(readBytes(1)).(uint8)
	thisNode.Name = string(readBytes((uint32)(NameLen)))

	NodeTypeLen := deSerializeUInt(readBytes(1)).(uint8)
	NodeType := string(readBytes((uint32)(NodeTypeLen)))
	thisNode.NodeType = (NodeTypes_t)(StringToNodetype(NodeType))

	UuidLen := deSerializeUInt(readBytes(1)).(uint8)
	thisNode.Uuid = string(readBytes((uint32)(UuidLen)))

	DescrLen := deSerializeUInt(readBytes(2)).(uint16)
	thisNode.Description = string(readBytes((uint32)(DescrLen)))

	DatatypeLen := deSerializeUInt(readBytes(1)).(uint8)
	if (thisNode.NodeType != BRANCH && thisNode.NodeType != STRUCT) {
	    thisNode.Datatype = string(readBytes((uint32)(DatatypeLen)))
	}

	MinLen := deSerializeUInt(readBytes(1)).(uint8)
	thisNode.Min = string(readBytes((uint32)(MinLen)))

	MaxLen := deSerializeUInt(readBytes(1)).(uint8)
	thisNode.Max = string(readBytes((uint32)(MaxLen)))

	UnitLen := deSerializeUInt(readBytes(1)).(uint8)
	thisNode.Unit = string(readBytes((uint32)(UnitLen)))

	allowedStrLen := deSerializeUInt(readBytes(2)).(uint16)
	allowedStr := string(readBytes((uint32)(allowedStrLen)))
	thisNode.Allowed = (uint8)(countAllowedElements(allowedStr))
	if (thisNode.Allowed > 0) {
            thisNode.AllowedDef = make([]string, thisNode.Allowed)
        }
	for i := 0 ; i < (int)(thisNode.Allowed) ; i++ {
	    thisNode.AllowedDef[i] = extractAllowedElement(allowedStr, i)
	}

	DefaultLen := deSerializeUInt(readBytes(1)).(uint8)
	thisNode.DefaultValue = string(readBytes((uint32)(DefaultLen)))

	ValidateLen := deSerializeUInt(readBytes(1)).(uint8)
	Validate := string(readBytes((uint32)(ValidateLen)))
	thisNode.Validate = ValidateToInt(Validate)

	thisNode.Children = deSerializeUInt(readBytes(1)).(uint8)

//	fmt.Printf("populateNode: %s\n", thisNode.Name)
}

// The reading order must be synchronized with the writing order in the binary tool
func writeNode(thisNode *Node_t) {
    treeFp.Write(serializeUInt((uint8)(len(thisNode.Name))))
    treeFp.Write([]byte(thisNode.Name))

    NodeType := NodetypeToString(thisNode.NodeType)
    treeFp.Write(serializeUInt((uint8)(len(NodeType))))
    treeFp.Write([]byte(NodeType))

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

    allowedStrLen := calculatAllowedStrLen(thisNode.AllowedDef)
    treeFp.Write(serializeUInt((uint16)(allowedStrLen)))
    if (thisNode.Allowed > 0) {
	for i := 0 ; i < (int)(thisNode.Allowed) ; i++ {
	    allowedWrite(thisNode.AllowedDef[i])
	}
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
	context.RootNodeName = searchPath[:getDotIndex(searchPath)]
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
	context.RootNodeName = searchPath[:getDotIndex(searchPath)]
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

func VSSGetLeafNodesList(rootNode *Node_t, rootNodeName string, listFname string) int {
    var context SearchContext_t
    isGetLeafNodeList = true
    var err error
    treeFp, err = os.OpenFile(listFname, os.O_RDWR|os.O_CREATE, 0755)
    if (err != nil) {
	fmt.Printf("Could not open %s for writing tree data\n", listFname)
	return 0
    }
    treeFp.Write([]byte("{\"leafpaths\":["))
    initContext_LNL(&context, rootNodeName+".*", rootNode, true, true, 0, nil)  // anyDepth = true, leafNodesOnly = true
    traverseNode(rootNode, &context)
    treeFp.Write([]byte("]}"))
    treeFp.Close()
    isGetLeafNodeList = false
    return context.NumOfMatches
}

func VSSGetDefaultList(rootNode *Node_t, rootNodeName string, listFname string) int {
    var context SearchContext_t
    isGetDefaultList = true
    var err error
    treeFp, err = os.OpenFile(listFname, os.O_RDWR|os.O_CREATE, 0755)
    if (err != nil) {
	fmt.Printf("Could not open %s for writing tree data\n", listFname)
	return 0
    }
    treeFp.Write([]byte("["))
    initContext_LNL(&context, rootNodeName+".*", rootNode, true, true, 0, nil)  // anyDepth = true, leafNodesOnly = true
    traverseNode(rootNode, &context)
    treeFp.Write([]byte("]"))
    treeFp.Close()
    isGetDefaultList = false
    return context.NumOfMatches
}

func VSSReadTree(fname string) *Node_t {
    var err error
    treeFp, err = os.OpenFile(fname, os.O_RDONLY, 0644)
    if (err != nil) {
        fmt.Printf("Could not open %s for writing of tree. Error= %s\n", fname, err)
        return nil
    }
    initReadMetadata()
    var root *Node_t = traverseAndReadNode(nil)
    printReadMetadata()
    treeFp.Close()
    return root
}

func VSSWriteTree(fname string, root *Node_t) {
    var err error
    treeFp, err = os.OpenFile(fname, os.O_RDWR|os.O_CREATE, 0755)
    if (err != nil) {
	fmt.Printf("Could not open %s for writing tree data\n", fname)
	return
    }
    traverseAndWriteNode(root)
    treeFp.Close()
}

func VSSgetName(nodeHandle *Node_t) string {
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

func VSSgetType(nodeHandle *Node_t) NodeTypes_t {
	return (NodeTypes_t)(nodeHandle.NodeType)
}

func VSSgetDatatype(nodeHandle *Node_t) string {
	nodeType := VSSgetType(nodeHandle)
	if (nodeType != BRANCH && nodeType != STRUCT) {
		return nodeHandle.Datatype
	}
	return ""
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
