/**
* (C) 2023 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"bufio"
	"encoding/json"
	"os"
	//    "io/ioutil"
	"database/sql"
	"fmt"
	"github.com/akamensky/argparse"
	_ "github.com/mattn/go-sqlite3"
	"sort"
	"strconv"
	"strings"
)

const MAXUINT16 = 65535 // used to indicate that conversion index is uninitialized

var branchPathList []string

var db *sql.DB

var otherTables []string = []string{"ConversionPreparation", "InternalTool"}

type DomainData struct {
	Path        string
	Type        string
	Unit        string
	Datatype    string
	EnumValues  string
	Min         string
	Max         string
	Default     string
	Description string
	Uid         string
	Comment     string
}

var domainData []DomainData

type FeederConversionData struct {
	MapIndex     uint16
	Name         string
	Type         int8
	Datatype     int8
	ConvertIndex uint16
}

type ToolConversionData struct {
	Name       string
	Type       string
	Datatype   string
	Unit       string
	EnumValues string
}

var scaleDataList []string

type UnitScaleElem struct {
	Unit1 string
	Unit2 string
	A     string
	B     string
}

var unitScaleList []UnitScaleElem

type SignalMapElem struct {
	North string
	South string
}

func initDb(dbFile string, db *sql.DB) *sql.DB {
	var err error
	db, err = sql.Open("sqlite3", dbFile)
	if !fileExists(dbFile) {
		createTables(db)
	}
	if err != nil {
		fmt.Printf("\novdsServer: Unable to init db = %s, err = %s", dbFile, err)
		os.Exit(1)
	}
	return db
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

func createTables(db *sql.DB) {
	createConversionDataTable(db)
	createInternalToolTable(db)
	initializeInternalToolTable(db)
}

func createDomainTableIfNotExist(db *sql.DB, tableName string) {
	tableNames := getDomainTableNames()
	if !domainTable(tableNames, tableName) {
		createDomainTable(db, tableName)
	}
}

func createDomainTable(db *sql.DB, tableName string) {
	stmt, err := db.Prepare(`CREATE TABLE "` + tableName + `" ("name" TEXT NOT NULL UNIQUE,  "type" TEXT, "datatype" TEXT, "unit" TEXT NOT NULL, "enumValues" TEXT, "min" TEXT, "max" TEXT, "deflt" TEXT, "uid" TEXT, "description" TEXT, "comment" TEXT)`)
	if err != nil {
		fmt.Printf("Error when preparing %s table, err = %s\n", tableName, err)
		os.Exit(1)
	}

	_, err = stmt.Exec()
	if err != nil {
		fmt.Printf("Error when creating %s table, err = %s\n", tableName, err)
		os.Exit(1)
	}
}

func createConversionDataTable(db *sql.DB) {
	stmt, err := db.Prepare(`CREATE TABLE "ConversionPreparation" ("id" INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT, "name" TEXT NOT NULL, "mapIndex" INTEGER, "type" TEXT, "datatype" TEXT, "conversionIndex" INTEGER)`)
	if err != nil {
		fmt.Printf("Error when preparing ConversionPreparation table, err = %s\n", err)
		os.Exit(1)
	}

	_, err = stmt.Exec()
	if err != nil {
		fmt.Printf("Error when creating ConversionPreparation table, err = %s\n", err)
		os.Exit(1)
	}
	return
}

func createInternalToolTable(db *sql.DB) {
	stmt, err := db.Prepare(`CREATE TABLE "InternalTool" ("nbdTableName" TEXT, "sbdTableName" TEXT, "structDef_Go" TEXT, "structDef_C" TEXT)`)
	if err != nil {
		fmt.Printf("Error when preparing Internal tool table, err = %s\n", err)
		os.Exit(1)
	}

	_, err = stmt.Exec()
	if err != nil {
		fmt.Printf("Error when creating Internal tool table, err = %s\n", err)
		os.Exit(1)
	}
	return
}

func initializeInternalToolTable(db *sql.DB) {
	structDefGo := `type  FeederConversionData struct {
	MapIndex uint16
	Name string
	Type int8
	Datatype uint8
	ConvertIndex uint16
}`
	structDefC := `typedef struct {
	uint16 mapIndex;
	char name[MAXPATHLEN];
	int8 type;
	uint8 datatype;
	uint16 ConvertIndex;
} FeederConversionData;`
	sqlString := "INSERT INTO InternalTool (structDef_Go, structDef_C) values(?, ?)"
	stmt, err := db.Prepare(sqlString)
	if err != nil {
		fmt.Printf("initializeInternalToolTable: SQL insert prepare error=%s\n", err)
		return
	}

	_, err = stmt.Exec(structDefGo, structDefC)
	if err != nil {
		fmt.Printf("initializeInternalToolTable: SQL insert execute error=%s\n", err)
		return
	}
}

func getInternalToolNbdTableNames() (string, string) {
	sqlQuery := "SELECT nbdTableName, sbdTableName FROM InternalTool;"
	rows, err := db.Query(sqlQuery)
	if err != nil {
		fmt.Printf("getInternalToolNbdTableNames: SQL query error=%s\n", err)
		return "", ""
	}
	var nbdTableName string
	var sbdTableName string

	rows.Next()
	err = rows.Scan(&nbdTableName, &sbdTableName)
	if err != nil {
		fmt.Printf("getInternalToolNbdTableNames: SQL result scan error=%s\n", err)
		return "", ""
	}
	rows.Close()
	return nbdTableName, sbdTableName
}

func updateInternalToolTableNames(nbdTableName string, sbdTableName string) {
	sqlString := "UPDATE InternalTool SET nbdTableName=?, sbdTableName=? WHERE rowid=1"
	stmt, err := db.Prepare(sqlString)
	if err != nil {
		fmt.Printf("updateInternalToolTableNames: SQL insert prepare error=%s\n", err)
		return
	}

	_, err = stmt.Exec(nbdTableName, sbdTableName)
	if err != nil {
		fmt.Printf("updateInternalToolTableNames: SQL insert execute error=%s\n", err)
		return
	}
}

func showDomains() {
	fmt.Printf("List names of existing domains in the DB.\n")
	fmt.Printf("Existing domain tables=%s\n", getTableNameList(getDomainTableNames()))
}

func populateTable() {
	var yamlFileName string
	fmt.Printf("Populate a DB table using data from a YAML file.\n")
	fmt.Printf("Please enter path and file name: ")
	fmt.Scanf("%s", &yamlFileName)
	if !fileExists(yamlFileName) {
		fmt.Printf("%s does not exist. Bye.\n", yamlFileName)
		return
	}
	file, err := os.Open(yamlFileName)
	if err != nil {
		fmt.Printf("Error reading %s: %s\n", yamlFileName, err)
		return
	}
	fmt.Printf("Importing data...")
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	domainName := ""
	var domainData DomainData
	clearDomainData(&domainData)
	var text string
	firstIteration := true
	lineAfterArrayRead := ""
	deferScan := false
	continueScan := true
	signalCount := 0
	for continueScan {
		if !deferScan {
			continueScan = scanner.Scan()
			text = scanner.Text()
		} else {
			text = lineAfterArrayRead
			deferScan = false
		}
		if strings.Contains(text, "Domain:") {
			if domainName == "" {
				domainName = readValue(text)
				createDomainTableIfNotExist(db, domainName)
			}
		} else if len(text) > 0 && text[len(text)-1] == ':' && text[0] != ' ' { // signal name
			if !firstIteration {
				if domainName == "" {
					fmt.Printf("DB table name not found in YAML file, temporary name is nonametable.\n")
					domainName = "nonametable"
					createDomainTableIfNotExist(db, domainName)
				}
				if domainData.Type != "branch" {
					insertTableRow(domainData, domainName)
					signalCount++
				}
				clearDomainData(&domainData)
			}
			domainData.Path = text[:len(text)-1]
			firstIteration = false
		} else if strings.Contains(text, "unit:") {
			domainData.Unit = readValue(text)
		} else if strings.Contains(text, "datatype:") {
			domainData.Datatype = readValue(text)
		} else if strings.Contains(text, "type:") {
			domainData.Type = readValue(text)
		} else if strings.Contains(text, "allowed:") {
			domainData.EnumValues, lineAfterArrayRead = readArray(scanner)
			deferScan = true
		} else if strings.Contains(text, "min:") {
			domainData.Min = readValue(text)
		} else if strings.Contains(text, "max:") {
			domainData.Max = readValue(text)
		} else if strings.Contains(text, "description:") {
			domainData.Description = readValue(text)
		} else if strings.Contains(text, "comment:") {
			domainData.Comment = readValue(text)
		} else if strings.Contains(text, "uuid:") {
			domainData.Uid = readValue(text)
		} else if strings.Contains(text, "default:") {
			if text[len(text)-1] != ':' {
				domainData.Default = readValue(text)
			} else {
				domainData.Default, lineAfterArrayRead = readArray(scanner)
				deferScan = true
			}
		} else {
			if len(domainData.Comment) > 0 {
				domainData.Comment += readValue(text) //most likely comments...
			} else if !isEmptyLine(text) {
				fmt.Printf("\nRow not saved in DB: %s", text)
			}
		}
	}
	// save last entry in YAML file
	insertTableRow(domainData, domainName)
	file.Close()
	fmt.Printf("\n%d signals imported.\n", signalCount+1)

}

func checkThisTable(db *sql.DB, tableName string) bool {
	sqlQuery := "SELECT name FROM sqlite_master WHERE type='table' AND name='" + tableName + "';"
	_, err := db.Query(sqlQuery)
	if err != nil {
		return false
	}
	return true
}

func clearDomainData(domainData *DomainData) {
	domainData.Path = ""
	domainData.Type = ""
	domainData.Unit = ""
	domainData.Datatype = ""
	domainData.EnumValues = ""
	domainData.Min = ""
	domainData.Max = ""
	domainData.Default = ""
	domainData.Uid = ""
	domainData.Description = ""
	domainData.Comment = ""
}

func createConversionTable() {
	fmt.Printf("Create conversion table.\n")
	var fname string
	fmt.Printf("Name of the signal mapping file: ")
	fmt.Scanf("%s", &fname)
	northBoundDomain, southBoundDomain, signalMappingList := readSignalMappingFile(fname)
	if northBoundDomain == "" {
		fmt.Printf("%s is not found. Bye.\n")
		os.Exit(-1)
	}
	tableNames := getDomainTableNames()
	fmt.Printf("Name of table for the northbound domain = %s\n", northBoundDomain)
	if !domainTable(tableNames, northBoundDomain) {
		fmt.Printf("Existing domain tables=%s\n", getTableNameList(tableNames))
		fmt.Printf("%s is not an existing table name. Bye.\n", northBoundDomain)
		return
	}
	fmt.Printf("Name of table for the southbound domain = %s\n", southBoundDomain)
	if !domainTable(tableNames, southBoundDomain) {
		fmt.Printf("Existing domain tables=%s\n", getTableNameList(tableNames))
		fmt.Printf("%s is not an existing table name. Bye.\n", southBoundDomain)
		return
	}
	updateInternalToolTableNames(northBoundDomain, southBoundDomain)
	truncateConversionTable()
	populateConversionTable(northBoundDomain, southBoundDomain, signalMappingList)
	writescaleDataList(northBoundDomain, southBoundDomain) // scaleDataList is populated by populateConversionTable()
	nbdNameArray := make([]string, len(signalMappingList))
	for i := 0; i < len(signalMappingList); i++ {
		nbdNameArray[i] = signalMappingList[i].North
	}
	sort.Strings(nbdNameArray)
	createMapDatamodel(northBoundDomain, "Datamodel-"+northBoundDomain+"-"+southBoundDomain+".yaml", nbdNameArray)
}

func domainTable(tableNames []string, name string) bool {
	for i := 0; i < len(tableNames); i++ {
		if tableNames[i] == name {
			return true
		}
	}
	return false
}

func getTableNameList(nameArray []string) string {
	tableNameList := ""
	for i := 0; i < len(nameArray); i++ {
		tableNameList += nameArray[i] + ", "
	}
	return tableNameList[:len(tableNameList)-2]
}

func getDomainTableNames() []string {
	sqlQuery := "SELECT name FROM sqlite_schema WHERE type ='table' AND name NOT LIKE 'sqlite_%';"
	rows, err := db.Query(sqlQuery)
	if err != nil {
		fmt.Printf("getDomainTableNames: SQL query error=%s\n", err)
		return nil
	}
	var nameArray []string

	var name string
	for rows.Next() {
		name = ""
		err = rows.Scan(&(name))
		if err != nil {
			fmt.Printf("getDomainTableNames: SQL result scan error=%s\n", err)
			return nil
		}
		if domainTableName(name) {
			nameArray = append(nameArray, name)
		}
	}
	rows.Close()
	return nameArray
}

func domainTableName(tableName string) bool {
	for i := 0; i < len(otherTables); i++ {
		if otherTables[i] == tableName {
			return false
		}
	}
	return true
}

func writescaleDataList(northBoundDomain string, southBoundDomain string) {
	fileName := "Scaling-" + northBoundDomain + "-" + southBoundDomain + ".json"
	treeFp, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open %s for writing scaling data\n", fileName)
		return
	}
	treeFp.Write([]byte("[\n"))
	treeFp.Write([]byte(`"{\"false\":\"0\", \"true\":\"1\"}",`))
	for i := 0; i < len(scaleDataList); i++ {
		if i == len(scaleDataList)-1 {
			treeFp.Write([]byte(`"` + insertEscape(scaleDataList[i]) + `"`))
		} else {
			treeFp.Write([]byte(`"` + insertEscape(scaleDataList[i]) + `",`))
		}
	}
	treeFp.Write([]byte("]\n"))
	treeFp.Close()
	fmt.Printf("Scaling data list file %s created.\n", fileName)
}

func insertEscape(jsonString string) string {
	escapeCount := 0
	for i := 0; i < len(jsonString); i++ {
		if jsonString[i] == '"' {
			escapeCount++
		}
	}
	var escapedString string
	for i := 0; i < len(jsonString); i++ {
		if jsonString[i] == '"' {
			escapedString += "\\"
			escapedString += "\""
		} else {
			escapedString += string(jsonString[i])
		}
	}
	return escapedString
}

func readSignalMappingFile(fileName string) (string, string, []SignalMapElem) {
	var signalMapList []SignalMapElem
	if !fileExists(fileName) {
		fmt.Printf("%s does not exist.\n", fileName)
		return "", "", nil
	}
	file, err := os.Open(fileName)
	if err != nil {
		fmt.Printf("Error reading %s: %s\n", fileName, err)
		return "", "", nil
	}
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var nbd string
	var sbd string
	var text string
	continueScan := true
	for continueScan {
		continueScan = scanner.Scan()
		text = scanner.Text()
		if strings.Contains(text, "#") || isEmptyLine(text) {
			continue
		} else if strings.Contains(text, "NorthBoundDomain:") {
			nbd = readValue(text)
		} else if strings.Contains(text, "SouthBoundDomain:") {
			sbd = readValue(text)
		} else if strings.Contains(text, "Mapping:") {
			signalMapList = readMapping(scanner)
			return nbd, sbd, signalMapList
		} else if continueScan {
			fmt.Printf("Error unknown key value=%s\n", text)
		}
	}
	file.Close()
	return "", "", nil
}

func readMapping(scanner *bufio.Scanner) []SignalMapElem {
	var signalMapList []SignalMapElem
	var signalMapElem SignalMapElem
	var text string
	readCompleted := false
	for scanner.Scan() {
		text = scanner.Text()
		if strings.Contains(text, "- North:") {
			signalMapElem.North = readValue(text)
		} else if strings.Contains(text, "South:") {
			signalMapElem.South = readValue(text)
			readCompleted = true
		}
		if readCompleted {
			signalMapList = append(signalMapList, signalMapElem)
			readCompleted = false
		}
	}
	return signalMapList
}

func truncateConversionTable() {
	sqlCommand := `DROP TABLE ConversionPreparation;`
	stmt, err := db.Prepare(sqlCommand)
	if err != nil {
		fmt.Printf("truncateConversionTable: SQL prepare error=%s\n", err)
		return
	}

	_, err = stmt.Exec()
	if err != nil {
		fmt.Printf("truncateConversionTable: SQL execute error=%s\n", err)
		return
	}
	createConversionDataTable(db)
}

func populateConversionTable(nbd string, sbd string, signalMapList []SignalMapElem) { // nbd = northBoundDomain, sbd = southBoundDomain
	var nbdToolData ToolConversionData
	var sbdToolData ToolConversionData
	numofitems := 0
	for i := 0; i < len(signalMapList); i++ {
		nbdToolData = getDomainData(nbd, signalMapList[i].North)
		sbdToolData = getDomainData(sbd, signalMapList[i].South)
		if nbdToolData.Name != "" && sbdToolData.Name != "" {
			createFeederConversionData(nbdToolData, sbdToolData, i)
			numofitems++
		} else {
			fmt.Printf("Incomplete domain data. Index=%d\n", i)
		}
	}
	fmt.Printf("Number of created conversion entries=%d\n", numofitems)
}

func getDomainData(tbl string, signalName string) ToolConversionData {
	var data ToolConversionData
	sqlQuery := `SELECT "name", "type", "datatype", "unit", "enumValues" FROM "` + tbl + `" WHERE "name"=?`
	rows, err := db.Query(sqlQuery, signalName)
	//	fmt.Printf("sqlQuery=%s\n", sqlQuery)
	if err != nil {
		fmt.Printf("getDomainData: SQL query error=%s\n", err)
		return data
	}

	rows.Next()
	err = rows.Scan(&(data.Name), &(data.Type), &(data.Datatype), &(data.Unit), &(data.EnumValues))
	if err != nil {
		fmt.Printf("getDomainData: SQL result scan error=%s\n", err)
		return data
	}
	rows.Close()
	return data
}

func createFeederConversionData(nbdtData ToolConversionData, sbdtData ToolConversionData, mapIndex int) {
	conversionIndex := MAXUINT16
	// hardcoded conversions: 0=no conversion, 1=boolean. For all others see funcIndex.list (funcList.go in feeder code)
	if sbdtData.Datatype == nbdtData.Datatype && nbdtData.Datatype == "boolean" {
		conversionIndex = 1
		//	} else if sbdtData.Unit == nbdtData.Unit && nbdtData.Datatype != "state_encoded" && nbdtData.EnumValues == "" {
	} else if sbdtData.Unit == nbdtData.Unit && nbdtData.EnumValues == "" {
		conversionIndex = 0
	} else if nbdtData.EnumValues != "" && sbdtData.EnumValues != "" {
		conversionIndex = getConversionTypeForEnum(nbdtData.EnumValues, sbdtData.EnumValues) + 2 // 0 and 1 reserved for none and boolean
	} else if nbdtData.Unit != "" && sbdtData.Unit != "" && sbdtData.Unit != nbdtData.Unit {
		conversionIndex = getConversionTypeForLinear(nbdtData.Unit, sbdtData.Unit) + 2 // 0 and 1 reserved for none and boolean
	}
	insertFeederData(nbdtData.Name, nbdtData.Type, nbdtData.Datatype, mapIndex, conversionIndex)
	insertFeederData(sbdtData.Name, sbdtData.Type, sbdtData.Datatype, mapIndex, conversionIndex)
}

func insertFeederData(name string, signalType string, datatype string, mapIndex int, conversionIndex int) {
	//	fmt.Printf("insertFeederData(name=%s, type=%s, datatype=%s, mapIndex=%d, conversionIndex=%d\n\n", name, signalType, datatype, mapIndex, conversionIndex)
	sqlString := "INSERT INTO ConversionPreparation (name, type, datatype, mapIndex, conversionIndex) values(?, ?, ?, ?, ?)"
	stmt, err := db.Prepare(sqlString)
	if err != nil {
		fmt.Printf("insertFeederData: SQL insert prepare error=%s\n", err)
		return
	}

	_, err = stmt.Exec(name, signalType, datatype, mapIndex, conversionIndex)
	if err != nil {
		fmt.Printf("insertFeederData: SQL insert execute error=%s\n", err)
		return
	}
}

func getConversionTypeForEnum(nbdEnums string, sbdEnums string) int {
	var nbdEnumMap []string
	var sbdEnumMap []string
	err := json.Unmarshal([]byte(nbdEnums), &nbdEnumMap)
	if err != nil {
		fmt.Printf("nbdEnumMap:Error unmarshal json=%s\n", err)
		return 65535 - 2
	}
	err = json.Unmarshal([]byte(sbdEnums), &sbdEnumMap)
	if err != nil {
		fmt.Printf("sbdEnumMap:Error unmarshal json=%s\n", err)
		return 65535 - 2
	}
	if len(nbdEnumMap) != len(sbdEnumMap) {
		fmt.Printf("getConversionTypeForEnum:Number of enum values inconsistent. nbt=%d, sbt=%d\n", len(nbdEnumMap), len(sbdEnumMap))
		return 65535 - 2
	}
	enumMap := "{"
	for i := 0; i < len(nbdEnumMap); i++ {
		enumMap += `"` + nbdEnumMap[i] + `":"` + sbdEnumMap[i] + `", `
	}
	enumMap = enumMap[:len(enumMap)-2] + "}"
	for i := 0; i < len(scaleDataList); i++ {
		if scaleDataList[i] == enumMap {
			return i
		}
	}
	scaleDataList = append(scaleDataList, enumMap)
	return len(scaleDataList) - 1
}

func getConversionTypeForLinear(nbdUnit string, sbdUnit string) int {
	var conversionCoefficients string
	for i := 0; i < len(unitScaleList); i++ {
		if unitScaleList[i].Unit1 == nbdUnit && unitScaleList[i].Unit2 == sbdUnit ||
			unitScaleList[i].Unit1 == sbdUnit && unitScaleList[i].Unit2 == nbdUnit {
			if unitScaleList[i].Unit1 == sbdUnit { // invert the coefficients
				var A float64
				var B float64
				var err error
				if A, err = strconv.ParseFloat(unitScaleList[i].A, 64); err != nil {
					fmt.Printf("getConversionTypeForLinear: Coeff A=%s cannot be converted to float.\n", unitScaleList[i].A)
					return 65535 - 2
				}
				if B, err = strconv.ParseFloat(unitScaleList[i].B, 64); err != nil {
					fmt.Printf("getConversionTypeForLinear: Coeff B=%s cannot be converted to float.\n", unitScaleList[i].B)
					return 65535 - 2
				}
				unitScaleList[i].A = strconv.FormatFloat(1/A, 'f', -1, 32)
				unitScaleList[i].B = strconv.FormatFloat(-B/A, 'f', -1, 32)
			}
			conversionCoefficients = `[` + unitScaleList[i].A + `, ` + unitScaleList[i].B + `]` // JSON float64 array
			for j := 0; j < len(scaleDataList); j++ {
				if scaleDataList[j] == conversionCoefficients {
					return j
				}
			}
			scaleDataList = append(scaleDataList, conversionCoefficients)
			return len(scaleDataList) - 1
		}
	}
	return 65535 - 2
}

func getTypeIndex(VSStype string) int8 { //TODO: use it when writing the struct array
	switch VSStype {
	case "sensor":
		return 0
	case "actuator":
		return 1
	case "attribute":
		return 2
	case "not used":
		return -1 //is ok to happen
	}
	fmt.Printf("getTypeIndex: unknown type=%s\n", VSStype)
	return -1
}

func getDatatypeIndex(VSSDatatype string) int8 { //TODO: use it when writing the struct array
	switch VSSDatatype {
	case "uint8":
		return 0
	case "uint16":
		return 1
	case "uint32":
		return 2
	case "uint64":
		return 3
	case "int8":
		return 4
	case "int16":
		return 5
	case "int32":
		return 6
	case "int64":
		return 7
	case "float":
		return 8
	case "float32":
		return 8
	case "double":
		return 9
	case "float64":
		return 9
	case "boolean":
		return 10
	case "string":
		return 11
	case "uint8[]":
		return 12
	case "uint16[]":
		return 13
	case "uint32[]":
		return 14
	case "uint64[]":
		return 15
	case "int8[]":
		return 16
	case "int16[]":
		return 17
	case "int32[]":
		return 18
	case "int64[]":
		return 19
	case "float32[]":
		return 20
	case "float64[]":
		return 21
	case "boolean[]":
		return 22
	case "string[]":
		return 23
	}
	fmt.Printf("getDatatypeIndex: unknown datatype=%s\n", VSSDatatype)
	return -1
}

func insertTableRow(domainData DomainData, tableName string) {
	sqlString := "INSERT INTO `" + tableName + "` (name, type, datatype, unit, enumValues, min, max, deflt, uid, description, comment) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	stmt, err := db.Prepare(sqlString)
	if err != nil {
		fmt.Printf("insertTableRow: SQL insert prepare error=%s, SQL=%s\n", err, sqlString)
		return
	}

	_, err = stmt.Exec(domainData.Path, domainData.Type, domainData.Datatype, domainData.Unit, domainData.EnumValues,
		domainData.Min, domainData.Max, domainData.Default, domainData.Uid, domainData.Description, domainData.Comment)
	if err != nil {
		fmt.Printf("insertTableRow: SQL insert execute error=%s\n", err)
		return
	}
	return
}

func createConversionFiles() {
	fmt.Printf("Create the files for feeder conversion.\n")
	createFeederArray()
}

func createFeederArray() {
	sqlQuery := "SELECT name, type, datatype, conversionIndex, mapIndex FROM ConversionPreparation ORDER BY name ASC;"
	rows, err := db.Query(sqlQuery)
	if err != nil {
		fmt.Printf("createFeederArray: SQL query error=%s\n", err)
		return
	}
	var feederMap []FeederConversionData
	var element FeederConversionData
	var elementType string
	var elementDataType string

	for rows.Next() {

		err = rows.Scan(&(element.Name), &elementType, &elementDataType, &(element.ConvertIndex), &(element.MapIndex))
		if err != nil {
			fmt.Printf("createFeederArray: SQL result scan error=%s\n", err)
			return
		}
		element.Type = getTypeIndex(elementType)
		element.Datatype = getDatatypeIndex(elementDataType)
		feederMap = append(feederMap, element)
	}
	rows.Close()
	//	printArray(feederMap)
	reorderMapIndex(feederMap)
	//	printArray(feederMap)
	nbtTable, sbtTable := getInternalToolNbdTableNames()
	fmt.Printf("Conversion data file=%s\n", nbtTable+"-"+sbtTable+".cvt")
	writeArrayToFile(feederMap, nbtTable+"-"+sbtTable+".cvt")
}

func reorderMapIndex(feederMap []FeederConversionData) {
	reorderMap := make([]uint16, len(feederMap))
	for i := 0; i < len(feederMap); i++ {
		reorderMap[i] = getCorrespondingIndex(i, feederMap[i].MapIndex, feederMap)
		if reorderMap[i] == MAXUINT16 {
			fmt.Printf("Reordering of mapIndex is not possible. Array cannot be constructed.\n")
			return
		}
	}
	for i := 0; i < len(feederMap); i++ {
		feederMap[i].MapIndex = reorderMap[i]
	}
}

func getCorrespondingIndex(thisIndex int, thisMapIndex uint16, feederMap []FeederConversionData) uint16 {
	for i := 0; i < len(feederMap); i++ {
		if feederMap[i].MapIndex == thisMapIndex && i != thisIndex {
			return (uint16)(i)
		}
	}
	fmt.Printf("getCorrespondingIndex: No corresponding index found for thisIndex=%d\n", thisIndex)
	return MAXUINT16
}

func writeArrayToFile(feederMap []FeederConversionData, fileName string) {
	treeFp, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open %s for writing map data\n", fileName)
		return
	}
	for i := 0; i < len(feederMap); i++ {
		writeElement(feederMap[i], treeFp)
	}
	treeFp.Close()
}

// The writing order must be aligned with the reading order by the feeder
func writeElement(mapElement FeederConversionData, treeFp *os.File) {
	treeFp.Write(serializeUInt((uint16)(mapElement.MapIndex)))

	treeFp.Write(serializeUInt((uint8)(len(mapElement.Name))))
	treeFp.Write([]byte(mapElement.Name))

	treeFp.Write(serializeUInt((uint8)(mapElement.Type)))

	treeFp.Write(serializeUInt((uint8)(mapElement.Datatype)))

	treeFp.Write(serializeUInt((uint16)(mapElement.ConvertIndex)))
}

func serializeUInt(intVal interface{}) []byte {
	switch intVal.(type) {
	case uint8:
		buf := make([]byte, 1)
		buf[0] = intVal.(byte)
		return buf
	case uint16:
		buf := make([]byte, 2)
		buf[1] = byte((intVal.(uint16) & 0xFF00) / 256)
		buf[0] = byte(intVal.(uint16) & 0x00FF)
		return buf
	case uint32:
		buf := make([]byte, 4)
		buf[3] = byte((intVal.(uint32) & 0xFF000000) / 16777216)
		buf[2] = byte((intVal.(uint32) & 0xFF0000) / 65536)
		buf[1] = byte((intVal.(uint32) & 0xFF00) / 256)
		buf[0] = byte(intVal.(uint32) & 0x00FF)
		return buf
	default:
		fmt.Println(intVal, "is of an unknown type")
		return nil
	}
}

func printArray(feederMap []FeederConversionData) {
	for i := 0; i < len(feederMap); i++ {
		fmt.Printf("feederMap[%d].Name=%s, feederMap[%d].MapIndex=%d\n", i, feederMap[i].Name, i, feederMap[i].MapIndex)
	}
}

func createDomainDatamodel() {
	var treeContent string
	var domainName string
	fmt.Printf("Create a VSS tree file in YAML format.\n")
	tableNames := getDomainTableNames()
	fmt.Printf("Existing domains=%s. Select domain: ", getTableNameList(tableNames))
	fmt.Scanf("%s", &domainName)
	if !domainTable(tableNames, domainName) {
		fmt.Printf("Existing domain tables=%s\n", getTableNameList(tableNames))
		fmt.Printf("%s is not an existing table name. Bye.\n", domainName)
		return
	}
	nodeArray := readDomainDatamodel(domainName)
	fmt.Printf("Complete tree (complete), or leafs only (leaf): ")
	fmt.Scanf("%s", &treeContent)
	if treeContent != "complete" && treeContent != "leaf" {
		fmt.Printf("Invalid input%s. Bye.\n", treeContent)
		return
	}
	createYamlFile(nodeArray, treeContent, "Datamodel-"+domainName+".yaml")
}

func createMapDatamodel(nbd string, fileName string, mapNameList []string) {
	var node DomainData
	var nodeArray []DomainData
	for i := 0; i < len(mapNameList); i++ {
		node = readMapElementDatamodel(nbd, mapNameList[i])
		if node != (DomainData{}) {
			nodeArray = append(nodeArray, node)
		}
	}
	createYamlFile(nodeArray, "complete", fileName)
}

func readMapElementDatamodel(nbd string, mapSignalName string) DomainData {
	sqlQuery := `SELECT "` + nbd + `".name, "` + nbd + `".type, "` + nbd + `".unit, "` + nbd + `".datatype, "` + nbd + `".enumValues, ` +
		`"` + nbd + `".min, "` + nbd + `".max, "` + nbd + `".deflt, "` + nbd + `".uid, "` + nbd + `".description, "` + nbd + `".comment` +
		` FROM "` + nbd + `" WHERE "` + nbd + `".name = "` + mapSignalName + `";`

	//	sqlQuery := `SELECT name, type, unit, datatype, enumValues, min, max, deflt, uid, description, comment FROM "` + nbd + `" ORDER BY name ASC;`
	rows, err := db.Query(sqlQuery)
	if err != nil {
		fmt.Printf("readMapElementDatamodel: SQL query error=%s\n", err)
		return (DomainData{})
	}

	rows.Next()
	var node DomainData
	err = rows.Scan(&(node.Path), &(node.Type), &(node.Unit), &(node.Datatype),
		&(node.EnumValues), &(node.Min), &(node.Max), &(node.Default), &(node.Uid), &(node.Description), &(node.Comment))
	if err != nil {
		fmt.Printf("readMapElementDatamodel: SQL result scan error=%s\n", err)
		return (DomainData{})
	}
	rows.Close()
	return node
}

func readDomainDatamodel(nbd string) []DomainData {
	sqlQuery := `SELECT name, type, unit, datatype, enumValues, min, max, deflt, uid, description, comment FROM "` + nbd + `" ORDER BY name ASC;`
	rows, err := db.Query(sqlQuery)
	if err != nil {
		fmt.Printf("readDomainDatamodel: SQL query error=%s\n", err)
		return nil
	}
	var nodeArray []DomainData

	for rows.Next() {
		var node DomainData
		err = rows.Scan(&(node.Path), &(node.Type), &(node.Unit), &(node.Datatype),
			&(node.EnumValues), &(node.Min), &(node.Max), &(node.Default), &(node.Uid), &(node.Description), &(node.Comment))
		if err != nil {
			fmt.Printf("readDomainDatamodel: SQL result scan error=%s\n", err)
			return nil
		}
		nodeArray = append(nodeArray, node)
	}
	rows.Close()
	return nodeArray
}

func createYamlFile(nodeArray []DomainData, treeContent string, fileName string) {
	includeNodes := false
	if treeContent == "complete" {
		includeNodes = true
	}
	treeFp, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE, 0755)
	if err != nil {
		fmt.Printf("Could not open %s for writing yaml data\n", fileName)
		return
	}
	for i := 0; i < len(nodeArray); i++ {
		if includeNodes {
			writeBranches(treeFp, nodeArray[i].Path)
		}
		writeYamlNode(treeFp, nodeArray[i])
	}
	treeFp.Close()
	fmt.Printf("VSS file %s created.\n ", fileName)
}

func writeBranches(treeFp *os.File, path string) {
	branchPaths := decomposePath(path)
	for i := 0; i < len(branchPaths); i++ {
		if !inBranchList(branchPaths[i]) {
			writeBranch(treeFp, branchPaths[i])
			addToBranchList(branchPaths[i])
		}
	}
}

func inBranchList(branchPath string) bool {
	for i := 0; i < len(branchPathList); i++ {
		if branchPath == branchPathList[i] {
			return true
		}
	}
	return false
}

func addToBranchList(branchPath string) {
	branchPathList = append(branchPathList, branchPath)
}

func writeBranch(treeFp *os.File, branchPath string) {
	treeFp.Write([]byte(branchPath + ":\n"))
	treeFp.Write([]byte("  type: branch\n"))
	treeFp.Write([]byte("  description: " + branchPath + "\n"))
	treeFp.Write([]byte("\n"))
}

func decomposePath(path string) []string {
	var branchPaths []string
	segments := strings.Count(path, ".")
	lead := 0
	for i := 0; i <= segments; i++ {
		trail := strings.Index(path[lead:], ".") + lead
		branchPaths = append(branchPaths, path[:trail])
		lead = trail + 1
	}
	return branchPaths
}

func writeYamlNode(treeFp *os.File, node DomainData) {
	treeFp.Write([]byte(node.Path + ":\n"))
	if len(node.Type) > 0 {
		treeFp.Write([]byte("  type: " + node.Type + "\n"))
	}
	if len(node.Datatype) > 0 {
		datatype := node.Datatype
		if datatype == "state_encoded" {
			datatype = "string"
		}
		treeFp.Write([]byte("  datatype: " + datatype + "\n"))
	}
	if len(node.Min) > 0 {
		treeFp.Write([]byte("  min: " + node.Min + "\n"))
	}
	if len(node.Max) > 0 {
		treeFp.Write([]byte("  max: " + node.Max + "\n"))
	}
	if len(node.Unit) > 0 {
		treeFp.Write([]byte("  unit: " + node.Unit + "\n"))
	}
	if len(node.Default) > 0 {
		if strings.Contains(node.Default, "[") {
			treeFp.Write([]byte("  default:\n"))
			defaultValues := extractJsonData(node.Default)
			for i := 0; i < len(defaultValues); i++ {
				treeFp.Write([]byte("  - '" + defaultValues[i] + "'\n")) //python needs ' around integer to treat it as string
			}
		} else {
			treeFp.Write([]byte("  default: " + node.Default + "\n"))
		}
	}
	if len(node.Uid) > 0 {
		treeFp.Write([]byte("  uuid: " + node.Uid + "\n"))
	}
	if len(node.EnumValues) > 0 && node.Datatype != "boolean" {
		treeFp.Write([]byte("  allowed:\n"))
		enumValues := extractJsonData(node.EnumValues)
		for i := 0; i < len(enumValues); i++ {
			treeFp.Write([]byte("  - '" + enumValues[i] + "'\n")) //python needs ' around integer to treat it as string
		}
	}
	if len(node.Description) > 0 {
		treeFp.Write([]byte("  description: " + node.Description + "\n"))
	} else {
		treeFp.Write([]byte("  description: " + node.Path + "\n")) // mandatory property in VSS-Tools
	}
	if len(node.Comment) > 0 {
		treeFp.Write([]byte("  comment: " + node.Comment + "\n"))
	}
	treeFp.Write([]byte("\n"))
}

func extractJsonData(jsonLiteral string) []string { // {"Key1":"value1", .., "KeyN":"valueN"} or ["value1", .., ++++++++-++"valueN"]
	if jsonLiteral[0] == '[' {
		var jsonArray []string
		err := json.Unmarshal([]byte(jsonLiteral), &jsonArray)
		if err != nil {
			fmt.Printf("extractJsonData:Error unmarshal json=%s\n", err)
			return nil
		}
		return jsonArray
	} else {
		var jsonMap interface{}
		err := json.Unmarshal([]byte(jsonLiteral), &jsonMap)
		if err != nil {
			fmt.Printf("extractJsonData:Error unmarshal json=%s\n", err)
			return nil
		}
		return extractJsonDataLevel1(jsonMap)
	}
}

func extractJsonDataLevel1(jsonMap interface{}) []string {
	switch vv := jsonMap.(type) {
	case map[string]interface{}:
		//		fmt.Println(vv, "is an object")
		return extractJsonDataLevel2(vv)
	default:
		fmt.Printf("extractJsonDataLevel1: %s is of an unknown type\n", vv)
	}
	return nil
}

func extractJsonDataLevel2(jsonObject map[string]interface{}) []string {
	valueArray := make([]string, len(jsonObject))
	i := 0
	for k, v := range jsonObject {
		switch v.(type) {
		case string:
			//			fmt.Println(k, "is a string:")
			valueArray[i] = k // key holds the VSS enum value
		default:
			fmt.Printf("extractJsonDataLevel2: %s is of an unknown type\n", k)
		}
		i++
	}
	sort.Strings(valueArray)
	return valueArray
}

func readUnitScaleData(yamlFileName string) {
	if !fileExists(yamlFileName) {
		fmt.Printf("%s does not exist.\n", yamlFileName)
		return
	}
	file, err := os.Open(yamlFileName)
	if err != nil {
		fmt.Printf("Error reading %s: %s\n", yamlFileName, err)
		return
	}
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var unitConversionElem UnitScaleElem
	var text string
	continueScan := true
	firstIteration := true
	for continueScan {
		continueScan = scanner.Scan()
		text = scanner.Text()
		if strings.Contains(text, "#") || strings.Contains(text, "coefficients") {
			continue
		} else if strings.Contains(text, "unit1") {
			if !firstIteration {
				unitScaleList = append(unitScaleList, unitConversionElem)
			}
			unitConversionElem.Unit1 = readValue(text)
			firstIteration = false
		} else if strings.Contains(text, "unit2") {
			unitConversionElem.Unit2 = readValue(text)
		} else if strings.Contains(text, "A:") {
			unitConversionElem.A = readValue(text)
		} else if strings.Contains(text, "B:") {
			unitConversionElem.B = readValue(text)
		} else if continueScan {
			fmt.Printf("Error unknown key value=%s\n", text)
		}
	}
	unitScaleList = append(unitScaleList, unitConversionElem) // add last scaling element
	file.Close()
}

func readArray(scanner *bufio.Scanner) (string, string) {
	var text string
	firstLineAfterArrayElem := ""
	array := "["
	for scanner.Scan() {
		text = scanner.Text()
		if strings.Contains(text, " - ") {
			array += `"` + readArrayValue(text) + `", `
		} else if !isEmptyLine(text) {
			firstLineAfterArrayElem = text
			//			fmt.Printf("firstLineAfterArrayElem=%s\n", firstLineAfterArrayElem)
			break
		}
	}
	if len(array) > 1 {
		array = array[:len(array)-2] + "]"
	} else {
		return "", ""
	}
	return array, firstLineAfterArrayElem
}

func isEmptyLine(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' {
			return false
		}
	}
	return true
}

func readArrayValue(line string) string {
	start := strings.Index(line, " - ")
	end := len(line)
	tmpIndex := strings.Index(line[start+3:], ",")
	if tmpIndex != -1 {
		end = tmpIndex + start + 3
	} else {
		tmpIndex := strings.Index(line[start+3:], "#")
		if tmpIndex != -1 {
			end = tmpIndex + start + 3
		}
	}
	return strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(line[start+3:end]), "'"), "'")
}

func readValue(line string) string {
	start := strings.Index(line, ":")
	return strings.TrimSpace(line[start+1:])
}

func main() {
	// Create new parser object
	parser := argparse.NewParser("print", "Domain Conversion Tool")
	taskSelector := parser.Selector("t", "taskSelector", []string{"domains", "datamodel", "import", "join", "createfiles"}, &argparse.Options{Required: false,
		Help: "Tasks to select between are domains, datamodel, import, join, or createfiles", Default: "import"})
	DctDb := parser.String("d", "dbfile", &argparse.Options{
		Required: false,
		Help:     "DCT database filename",
		Default:  "DCT.db"})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
	}

	readUnitScaleData("UnitScaling.yaml")
	domainData = make([]DomainData, 2)
	db = initDb(*DctDb, db)
	defer db.Close()
	fmt.Printf("Opened database %s for the task %s\n", *DctDb, *taskSelector)
	switch *taskSelector {
	case "domains":
		showDomains()
	case "datamodel":
		createDomainDatamodel()
	case "import":
		populateTable()
	case "join":
		createConversionTable()
	case "createfiles":
		createConversionFiles()
	default:
		fmt.Printf("Unsupported task.\n")
	}
}
