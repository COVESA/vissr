/**
* (C) 2021 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package serviceMgr

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/covesa/vissr/utils"
	_ "github.com/mattn/go-sqlite3"
)

type CLPack struct {
	DataPack       string
	SubscriptionId int
}
type SubThreads struct {
	NumofThreads   int
	SubscriptionId int
}

var CLChannel chan CLPack
var threadsChan chan SubThreads

var closeClSubId int = -1
var mcloseClSubId = &sync.Mutex{}

type RingElement struct {
	Value     string
	Timestamp string
}

type RingBuffer struct {
	bufSize  int
	RingElem []RingElement
	Head     int
	Tail     int
}

type CLBufElement struct {
	Value     float64
	Timestamp float64
}

// posxType values: >0 => saved by PDR algo, 0 => not saved by PDR, -1 => empty
type PostProcessBufElement1dim struct {
	Data CLBufElement
	Dp   string
	Type int
}

type PostProcessBufElement2dim struct {
	Data1 CLBufElement
	Data2 CLBufElement
	Dp1   string
	Dp2   string
	Type  int
}

type PostProcessBufElement3dim struct {
	Data1 CLBufElement
	Data2 CLBufElement
	Data3 CLBufElement
	Dp1   string
	Dp2   string
	Dp3   string
	Type  int
}

const MAXCLBUFSIZE = 240  // something large...
const MAXCLSESSIONS = 100 // This value depends on the HW memory and performance
var numOfClSessions int = 0

func createRingBuffer(bufSize int) RingBuffer {
	var aRingBuffer RingBuffer
	aRingBuffer.bufSize = bufSize
	aRingBuffer.Head = 0
	aRingBuffer.Tail = 0
	aRingBuffer.RingElem = make([]RingElement, bufSize)
	return aRingBuffer
}

func getRingHead(aRingBuffer *RingBuffer) int {
	return aRingBuffer.Head
}

func setRingTail(aRingBuffer *RingBuffer, tail int) {
	aRingBuffer.Tail = aRingBuffer.Head - (tail + 1) // Head points to next to be written
}

func writeRing(aRingBuffer *RingBuffer, value string, timestamp string) {
	//utils.Info.Printf("writeRing: value=%s, ts=%s\n", value, timestamp)
	aRingBuffer.RingElem[aRingBuffer.Head].Value = value
	aRingBuffer.RingElem[aRingBuffer.Head].Timestamp = timestamp
	aRingBuffer.Head++
	if aRingBuffer.Head == aRingBuffer.bufSize {
		aRingBuffer.Head = 0
	}
}

func readRing(aRingBuffer *RingBuffer, headOffset int) (string, string) {
	currentHead := aRingBuffer.Head - (headOffset + 1) // Head points to next to write to
	if currentHead < 0 {
		currentHead += aRingBuffer.bufSize
	}
	//utils.Info.Printf("value=%s,timestamp=%s, currentHead=%d,", aRingBuffer.RingElem[currentHead].Value, aRingBuffer.RingElem[currentHead].Timestamp, currentHead)
	return aRingBuffer.RingElem[currentHead].Value, aRingBuffer.RingElem[currentHead].Timestamp
}

func getNumOfPopulatedRingElements(aRingBuffer *RingBuffer) int {
	head := aRingBuffer.Head
	tail := aRingBuffer.Tail
	if head < tail {
		head += aRingBuffer.bufSize
	}
	return head - tail
}

type Dim2Elem struct {
	Path1 string `json:"path1"`
	Path2 string `json:"path2"`
}

type Dim3Elem struct {
	Path1 string `json:"path1"`
	Path2 string `json:"path2"`
	Path3 string `json:"path3"`
}

type SignalDimensionLists struct {
	dim2List []Dim2Elem `json:"dim2"`
	dim3List []Dim3Elem `json:"dim3"`
}

type PathDimElem struct {
	Dim       int
	Id        int
	Populated bool
}

func unpacksignalDimensionMap(signalDimensionMap map[string]interface{}, signalDimensionLists SignalDimensionLists) SignalDimensionLists {
	for dimKey, v := range signalDimensionMap {
		switch vv := v.(type) {
		case map[string]interface{}:
			utils.Info.Println(dimKey, "is a map:")
			if dimKey == "dim2" {
				signalDimensionLists.dim2List = make([]Dim2Elem, 1)
			} else if dimKey == "dim3" {
				signalDimensionLists.dim3List = make([]Dim3Elem, 1)
			}
			unPackDimSignalsLevel1(0, vv, dimKey, signalDimensionLists)
		case []interface{}:
			utils.Info.Println(dimKey, "is an array:, len=", strconv.Itoa(len(vv)))
			if dimKey == "dim2" {
				signalDimensionLists.dim2List = make([]Dim2Elem, len(vv))
			} else if dimKey == "dim3" {
				signalDimensionLists.dim3List = make([]Dim3Elem, len(vv))
			}
			for k, v := range vv {
				unPackDimSignalsLevel1(k, v.(map[string]interface{}), dimKey, signalDimensionLists)
			}
		default:
			utils.Info.Println(dimKey, "is of an unknown type")
		}
	}
	return signalDimensionLists
}

func unPackDimSignalsLevel1(index int, signalDimMap map[string]interface{}, dimKey string, signalDimensionLists SignalDimensionLists) {
	for pathKey, v := range signalDimMap {
		switch vv := v.(type) {
		case string:
			utils.Info.Println(vv, "is string")
			if dimKey == "dim2" {
				if pathKey == "path1" {
					signalDimensionLists.dim2List[index].Path1 = vv
				} else {
					signalDimensionLists.dim2List[index].Path2 = vv
				}
			} else {
				if pathKey == "path1" {
					signalDimensionLists.dim3List[index].Path1 = vv
				} else if pathKey == "path2" {
					signalDimensionLists.dim3List[index].Path2 = vv
				} else {
					signalDimensionLists.dim3List[index].Path3 = vv
				}
			}
		default:
			utils.Info.Println(pathKey, "is of an unknown type")
		}
	}
}

func jsonToStructList(data string) *SignalDimensionLists {
	var signalDimensionMap map[string]interface{}
	var signalDimensionLists SignalDimensionLists
	err := json.Unmarshal([]byte(data), &signalDimensionMap)
	if err != nil {
		utils.Error.Printf("Error unmarshal signal dimension list=%s", err)
		return nil
	}
	signalDimensionLists = unpacksignalDimensionMap(signalDimensionMap, signalDimensionLists)
	return &signalDimensionLists
}

func readSignalDimensions(fname string) *SignalDimensionLists {
	data, err := os.ReadFile(fname)
	if err != nil {
		utils.Error.Printf("Error reading signal dimension file=%s", err)
		return nil
	}
	return jsonToStructList(string(data))
}

func populateDimLists(paths []string) ([]string, []Dim2Elem, []Dim3Elem) {
	var dim1List []string
	var dim2List []Dim2Elem
	var dim3List []Dim3Elem

	signalDimensionList := readSignalDimensions("signaldimension.json")
	pathDimList := analyzeSignalDimensions(paths, signalDimensionList)

	for i := 0; i < len(paths); i++ {
		if pathDimList[i].Dim == 1 {
			dim1List = append(dim1List, paths[i])
		} else if pathDimList[i].Dim == 2 && pathDimList[i].Populated == false {
			for j := i + 1; j < len(paths); j++ {
				if pathDimList[j].Dim == 2 && pathDimList[j].Id == pathDimList[i].Id {
					var dim2Elem Dim2Elem
					dim2Elem.Path1 = paths[i]
					dim2Elem.Path2 = paths[j]
					dim2List = append(dim2List, dim2Elem)
					pathDimList[j].Populated = true
				}
			}

		} else if pathDimList[i].Dim == 3 && pathDimList[i].Populated == false {
			for j := i + 1; j < len(paths); j++ {
				if pathDimList[j].Dim == 3 && pathDimList[j].Id == pathDimList[j].Id {
					for k := j + 1; k < len(paths); k++ {
						if pathDimList[k].Dim == 3 && pathDimList[k].Id == pathDimList[i].Id {
							var dim3Elem Dim3Elem
							dim3Elem.Path1 = paths[i]
							dim3Elem.Path2 = paths[j]
							dim3Elem.Path3 = paths[k]
							dim3List = append(dim3List, dim3Elem)
							pathDimList[j].Populated = true
							pathDimList[k].Populated = true
						}
					}
				}
			}
		}
	}
	return dim1List, dim2List, nil
}

func analyzeSignalDimensions(paths []string, signalDimensionList *SignalDimensionLists) []PathDimElem {
	pathDimList := make([]PathDimElem, len(paths))
	dim2Id := 0
	dim3Id := 0
	for i := 0; i < len(paths); i++ {
		pathDimList[i].Dim = 1
		pathDimList[i].Id = -1
		pathDimList[i].Populated = false
	}
	for i := 0; i < len(paths); i++ {
		if is2dim(paths[i], 1, signalDimensionList.dim2List) == true {
			for j := i + 1; j < len(paths); j++ {
				if is2dim(paths[j], 2, signalDimensionList.dim2List) == true {
					pathDimList[i].Dim = 2
					pathDimList[i].Id = dim2Id
					pathDimList[j].Dim = 2
					pathDimList[j].Id = dim2Id
					dim2Id++
					break
				}
			}
		} else if is3dim(paths[i], 1, signalDimensionList.dim3List) == true {
			done := false
			for j := i + 1; j < len(paths); j++ {
				if is3dim(paths[j], 2, signalDimensionList.dim3List) == true {
					for k := j + 1; k < len(paths); k++ {
						if is3dim(paths[k], 3, signalDimensionList.dim3List) == true {
							pathDimList[i].Dim = 3
							pathDimList[i].Id = dim3Id
							pathDimList[j].Dim = 3
							pathDimList[j].Id = dim3Id
							pathDimList[k].Dim = 3
							pathDimList[k].Id = dim3Id
							dim3Id++
							done = true
							break
						} else {
							pathDimList[i].Dim = 2
							pathDimList[i].Id = dim2Id
							pathDimList[j].Dim = 2
							pathDimList[j].Id = dim2Id
							dim2Id++
							done = true
							break
						}
					}
				}
				if done == true {
					break
				}
			}
		}
	}
	return pathDimList
}

func is2dim(path string, index int, dim2List []Dim2Elem) bool {
	var listPath string
	for i := 0; i < len(dim2List); i++ {
		if index == 1 {
			listPath = dim2List[i].Path1
		} else if index == 2 {
			listPath = dim2List[i].Path2
		} else {
			return false
		}
		if listPath == path {
			utils.Info.Printf("is2dim=true")
			return true
		}
	}
	return false
}

func is3dim(path string, index int, dim3List []Dim3Elem) bool {
	var listPath string
	for i := 0; i < len(dim3List); i++ {
		if index == 1 {
			listPath = dim3List[i].Path1
		} else if index == 2 {
			listPath = dim3List[i].Path2
		} else if index == 3 {
			listPath = dim3List[i].Path3
		} else {
			return false
		}
		if listPath == path {
			return true
		}
	}
	return false
}

func getSleepDuration(newTime time.Time, oldTime time.Time, wantedDuration int) time.Duration {
	workDuration := newTime.Sub(oldTime)
	sleepDuration := time.Duration(wantedDuration) * time.Millisecond
	return sleepDuration - workDuration
}

func curveLoggingServer(clChan chan CLPack, threadsChan chan SubThreads, subscriptionId int, opValue string, paths []string) {
	maxError, bufSize := getCurveLoggingParams(opValue)
	if bufSize > MAXCLBUFSIZE {
		bufSize = MAXCLBUFSIZE
	}
	dim1List, dim2List, dim3List := populateDimLists(paths)
	for i := 0; i < len(dim1List); i++ {
		if numOfClSessions > MAXCLSESSIONS {
			utils.Error.Printf("Curve logging: All resources are utilized.")
			break
		}
		returnSingleDp(clChan, subscriptionId, dim1List[i])
		go clCapture1dim(clChan, subscriptionId, dim1List[i], bufSize, maxError)
		numOfClSessions++
	}
	for i := 0; i < len(dim2List); i++ {
		if numOfClSessions > MAXCLSESSIONS {
			utils.Error.Printf("Curve logging: All resources are utilized.")
			break
		}
		returnSingleDp2(clChan, subscriptionId, dim2List[i])
		go clCapture2dim(clChan, subscriptionId, dim2List[i], bufSize, maxError)
		numOfClSessions++
	}
	for i := 0; i < len(dim3List); i++ {
		if numOfClSessions > MAXCLSESSIONS {
			utils.Error.Printf("Curve logging: All resources are utilized.")
			break
		}
		returnSingleDp3(clChan, subscriptionId, dim3List[i])
		go clCapture3dim(clChan, subscriptionId, dim3List[i], bufSize, maxError)
		numOfClSessions++
	}
	var subThreads SubThreads
	subThreads.NumofThreads = len(dim1List) + len(dim2List) + len(dim3List)
	subThreads.SubscriptionId = subscriptionId
	threadsChan <- subThreads

}

func clCapture1dim(clChan chan CLPack, subscriptionId int, path string, bufSize int, maxError float64) {
	aRingBuffer := createRingBuffer(bufSize + 1) // logic requires buffer to have a size of one larger than needed
	var dpMap = make(map[string]interface{})
	closeClSession := false
	oldTime := getCurrentUtcTime()
	lastSelected := 0 // index into ringBuffer; zero points to last dp stored in buffer, increasing values goes backwards in time
	postProc := make([]PostProcessBufElement1dim, 3)
	//    { CLBufElement{0, 0}, "", -1, CLBufElement{0, 0}, "", -1, CLBufElement{0, 0}, "", -1 }
	for {
		newTime := getCurrentUtcTime()
		sleepPeriod := getSleepDuration(newTime, oldTime, 800) // TODO: Iteration period should be configurable, set to less than sample freq of signal.
		if sleepPeriod < 0 {
			utils.Warning.Printf("Curve logging may have missed to capture.")
		}
		time.Sleep(sleepPeriod)
		oldTime = getCurrentUtcTime()
		mcloseClSubId.Lock()
		if closeClSubId == subscriptionId {
			closeClSession = true
		}
		mcloseClSubId.Unlock()
		dp := getVehicleData(path)
		utils.Info.Printf("dp=%s", dp)
		utils.MapRequest(dp, &dpMap)
		_, ts := readRing(&aRingBuffer, 0) // read latest written
		if ts != dpMap["ts"].(string) {
			writeRing(&aRingBuffer, dpMap["value"].(string), dpMap["ts"].(string))
		}
		currentBufSize := getNumOfPopulatedRingElements(&aRingBuffer)
		if (currentBufSize == bufSize) || (closeClSession == true) {
			var data string
			var extraData string  // the last dp in buffers with no other saved, if returned by postProcess1dim
			var firstSelected int // needed inpostProcess1dim
			data, lastSelected, firstSelected = clAnalyze1dim(&aRingBuffer, currentBufSize, maxError)
			extraData, postProc = postProcess1dim(&aRingBuffer, firstSelected, lastSelected, postProc, maxError)
			var clPack CLPack
			clPack.SubscriptionId = subscriptionId
			if len(extraData) > 0 {
				clPack.DataPack = `{"path":"` + path + `","dp":` + extraData + "}"
				clChan <- clPack
			}
			if lastSelected > 0 {
				clPack.DataPack = `{"path":"` + path + `","dp":` + data + "}"
				clChan <- clPack
			}
			setRingTail(&aRingBuffer, lastSelected) // update tail pointer
		}
		if closeClSession == true {
			break
		}
	}
	//    if (lastSelected > 0) {  // last datapoint in the buffer has not been saved
	returnSingleDp(clChan, subscriptionId, path)
	//    }
}

func clAnalyze1dim(aRingBuffer *RingBuffer, bufSize int, maxError float64) (string, int, int) { // [{"value":"X","ts":"Y"},..{}] ; square brackets optional
	clBuffer := make([]CLBufElement, bufSize) // array holds transformed value/ts pairs, from latest to first captured
	clBuffer = transformDataPoints(aRingBuffer, clBuffer, bufSize)
	savedIndex := clReduction1Dim(clBuffer, 0, bufSize-1, maxError)
	dataPoint := ""
	lastSelected := 0  // index for last dp in ring buffer that is selected by RDP-algo, or last value in buffer if none selected
	firstSelected := 0 // index for first dp in ring buffer that is selected by RDP-algo, or last value in buffer if none selected
	if savedIndex != nil {
		sort.Sort(sort.Reverse(sort.IntSlice(savedIndex)))
		lastSelected = savedIndex[len(savedIndex)-1]
		if len(savedIndex) > 1 {
			dataPoint += "["
		}
		for i := 0; i < len(savedIndex); i++ {
			//utils.Info.Printf("clAnalysis1dim:savedIndex[%d]=%d", i, savedIndex[i])
			val, ts := readRing(aRingBuffer, savedIndex[i])
			dataPoint += `{"value":"` + val + `","ts":"` + ts + `"},`
		}
		dataPoint = dataPoint[:len(dataPoint)-1]
		if len(savedIndex) > 1 {
			dataPoint += "]"
		}
		firstSelected = savedIndex[0]
	} else {
		val, ts := readRing(aRingBuffer, 0) // return latest sample (= head sample)
		dataPoint += `{"value":"` + val + `","ts":"` + ts + `"}`
	}
	return dataPoint, lastSelected, firstSelected
}

func clReduction1Dim(clBuffer []CLBufElement, firstIndex int, lastIndex int, maxError float64) []int {
	//utils.Info.Printf("clReduction:firstIndex=%d, lastIndex=%d, maxError=%f, ", firstIndex, lastIndex, maxError)
	if lastIndex-firstIndex <= 1 {
		return nil
	}
	var maxMeasuredError float64 = 0.0
	indexOfMaxMeasuredError := firstIndex
	var measuredError float64

	linearSlope := (clBuffer[lastIndex].Value - clBuffer[firstIndex].Value) / (float64)(clBuffer[lastIndex].Timestamp-clBuffer[firstIndex].Timestamp)

	for i := 0; i <= lastIndex-firstIndex; i++ {
		measuredError = clBuffer[firstIndex+i].Value - (clBuffer[firstIndex].Value + linearSlope*(float64)(clBuffer[firstIndex+i].Timestamp-clBuffer[firstIndex].Timestamp))
		if measuredError < 0 {
			measuredError = -measuredError
		}
		if measuredError > maxMeasuredError {
			maxMeasuredError = measuredError
			indexOfMaxMeasuredError = firstIndex + i
		}
	}

	if maxMeasuredError > maxError {
		var savedIndex1, savedIndex2 []int
		savedIndex1 = append(savedIndex1, clReduction1Dim(clBuffer, firstIndex, indexOfMaxMeasuredError, maxError)...)
		savedIndex2 = append(savedIndex2, clReduction1Dim(clBuffer, indexOfMaxMeasuredError, lastIndex, maxError)...)
		savedIndex1 = append(savedIndex1, savedIndex2...)
		return append(savedIndex1, indexOfMaxMeasuredError)
	}
	return nil
}

/*
* firstSelected/lastSelected = 0 => dp not saved by PDR algorithm
* firstSelected/lastSelected > 0 => dp saved by PDR algorithm
 */
func postProcess1dim(aRingBuffer *RingBuffer, firstSelected int, lastSelected int, postProc []PostProcessBufElement1dim, maxError float64) (string, []PostProcessBufElement1dim) {
	if postProc[0].Type == -1 { // init at startup
		postProc = writePostProcElement1dim(aRingBuffer, lastSelected, postProc, 0)
		return "", postProc
	} else {
		if postProc[1].Type == -1 {
			pos := 0
			if firstSelected == 0 {
				pos = 1
			}
			postProc = writePostProcElement1dim(aRingBuffer, lastSelected, postProc, pos)
			return "", postProc
		} else {
			postProc = writePostProcElement1dim(aRingBuffer, firstSelected, postProc, 2)
			if saveNonPdrDp(postProc, maxError) == true {
				if firstSelected == 0 {
					postProc = movePostProcElement1dim(postProc, 1, 0)
					postProc = movePostProcElement1dim(postProc, 2, 1)
					postProc[2].Type = -1
					return postProc[0].Dp, postProc
				} else {
					postProc = writePostProcElement1dim(aRingBuffer, lastSelected, postProc, 0) //move from 2 to 0, change to lastSelected
					postProc[1].Type = -1
					postProc[2].Type = -1
					return postProc[1].Dp, postProc
				}
			} else {
				if firstSelected == 0 {
					postProc = movePostProcElement1dim(postProc, 1, 0)
					postProc = movePostProcElement1dim(postProc, 2, 1)
					postProc[2].Type = -1
				} else {
					postProc = writePostProcElement1dim(aRingBuffer, lastSelected, postProc, 0) //move from 2 to 0, change to lastSelected
					postProc[1].Type = -1
					postProc[2].Type = -1
				}
				return "", postProc
			}
		}
	}
	return "", postProc // should not happen
}

func writePostProcElement1dim(aRingBuffer *RingBuffer, firstSelected int, postProc []PostProcessBufElement1dim, pos int) []PostProcessBufElement1dim {
	val, ts := readRing(aRingBuffer, firstSelected)
	postProc[pos].Dp = `{"value":"` + val + `","ts":"` + ts + `"}`
	postProc[pos].Data, _ = transformDataPoint(aRingBuffer, firstSelected, time.Now()) // time base not used
	postProc[pos].Type = firstSelected
	return postProc
}

func movePostProcElement1dim(postProc []PostProcessBufElement1dim, source int, dest int) []PostProcessBufElement1dim {
	postProc[dest].Dp = postProc[source].Dp
	postProc[dest].Data = postProc[source].Data
	postProc[dest].Type = postProc[source].Type
	return postProc
}

func saveNonPdrDp(postProc []PostProcessBufElement1dim, maxError float64) bool {
	fraction := (postProc[1].Data.Timestamp - postProc[0].Data.Timestamp) / (postProc[2].Data.Timestamp - postProc[0].Data.Timestamp)
	pos2InterpolatedValue := postProc[0].Data.Value + (postProc[2].Data.Value-postProc[0].Data.Value)*fraction
	pos2Error := postProc[1].Data.Value - pos2InterpolatedValue
	if pos2Error < 0 {
		pos2Error = -pos2Error
	}
	return pos2Error > maxError
}

func transformDataPoints(aRingBuffer *RingBuffer, clBuffer []CLBufElement, bufSize int) []CLBufElement {
	var status bool
	_, tsBaseStr := readRing(aRingBuffer, bufSize-1) // get ts for first dp in buffer
	tsBase, _ := time.Parse(time.RFC3339, tsBaseStr)
	for index := 0; index < bufSize; index++ {
		clBuffer[index], status = transformDataPoint(aRingBuffer, index, tsBase)
		if status == false {
			return nil
		}
	}
	return clBuffer
}

func transformDataPoint(aRingBuffer *RingBuffer, index int, tsBase time.Time) (CLBufElement, bool) {
	var cLBufElement CLBufElement
	val, ts := readRing(aRingBuffer, index)
	value, err := strconv.ParseFloat(val, 64)
	if err != nil {
		utils.Error.Printf("Curve logging failed to convert value=%s to float err=%s", val, err)
		return cLBufElement, false
	}
	cLBufElement.Value = (float64)(value)
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t2, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			utils.Error.Printf("Curve logging failed to convert time to Unix time err=%s", err)
			return cLBufElement, false
		}
		tstr := time.UnixMilli(t2).Format(time.RFC3339)
		t, err = time.Parse(time.RFC3339, tstr)
		if err != nil {
			utils.Error.Printf("Curve logging failed to convert time to Unix time err=%s", err)
			return cLBufElement, false
		}
	}
	cLBufElement.Timestamp = t.Sub(tsBase).Seconds()
	return cLBufElement, true
}

func returnSingleDp(clChan chan CLPack, subscriptionId int, path string) {
	dp := getVehicleData(path)
	var clPack CLPack
	clPack.DataPack = `{"path":"` + path + `","dp":` + dp + "}"
	clPack.SubscriptionId = subscriptionId
	clChan <- clPack
}

func returnSingleDp2(clChan chan CLPack, subscriptionId int, paths Dim2Elem) {
	dp1 := getVehicleData(paths.Path1)
	dp2 := getVehicleData(paths.Path2)
	var clPack CLPack
	clPack.DataPack = `[{"path":"` + paths.Path1 + `","dp":` + dp1 + "}," + `{"path":"` + paths.Path2 + `","dp":` + dp2 + "}]"
	clPack.SubscriptionId = subscriptionId
	clChan <- clPack
}

func returnSingleDp3(clChan chan CLPack, subscriptionId int, paths Dim3Elem) {
	dp1 := getVehicleData(paths.Path1)
	dp2 := getVehicleData(paths.Path2)
	dp3 := getVehicleData(paths.Path3)
	var clPack CLPack
	clPack.DataPack = `[{"path":"` + paths.Path1 + `","dp":` + dp1 + `},{"path":"` + paths.Path2 + `","dp":` + dp2 + `},{"path":"` + paths.Path3 + `","dp":` + dp3 + "}]"
	clPack.SubscriptionId = subscriptionId
	clChan <- clPack
}

func clCapture2dim(clChan chan CLPack, subscriptionId int, paths Dim2Elem, bufSize int, maxError float64) {
	aRingBuffer1 := createRingBuffer(bufSize + 1)
	aRingBuffer2 := createRingBuffer(bufSize + 1)
	var dpMap1 = make(map[string]interface{})
	var dpMap2 = make(map[string]interface{})
	closeClSession := false
	oldTime := getCurrentUtcTime()
	updatedTail := 0
	for {
		newTime := getCurrentUtcTime()
		sleepPeriod := getSleepDuration(newTime, oldTime, 800) // TODO: Iteration period should be configurable, set to less than sample freq of signal.
		if sleepPeriod < 0 {
			utils.Warning.Printf("Curve logging may have missed to capture.")
		}
		time.Sleep(sleepPeriod)
		oldTime = getCurrentUtcTime()
		mcloseClSubId.Lock()
		if closeClSubId == subscriptionId {
			closeClSession = true
		}
		mcloseClSubId.Unlock()
		dp1 := getVehicleData(paths.Path1)
		dp2 := getVehicleData(paths.Path2)
		utils.MapRequest(dp1, &dpMap1)
		utils.MapRequest(dp2, &dpMap2)
		_, ts1 := readRing(&aRingBuffer1, 0)
		_, ts2 := readRing(&aRingBuffer2, 0)
		if ts1 != dpMap1["ts"].(string) && ts2 != dpMap2["ts"].(string) && dpMap1["ts"].(string) == dpMap2["ts"].(string) {
			writeRing(&aRingBuffer1, dpMap1["value"].(string), dpMap1["ts"].(string))
			writeRing(&aRingBuffer2, dpMap2["value"].(string), dpMap2["ts"].(string))
		}
		currentBufSize := getNumOfPopulatedRingElements(&aRingBuffer1)
		if (currentBufSize == bufSize) || (closeClSession == true) {
			data1, data2, updatedTail := clAnalyze2dim(&aRingBuffer1, &aRingBuffer2, currentBufSize, maxError)
			var clPack CLPack
			clPack.DataPack = `[{"path":"` + paths.Path1 + `","dp":` + data1 + "}," + `{"path":"` + paths.Path2 + `","dp":` + data2 + "}]"
			clPack.SubscriptionId = subscriptionId
			clChan <- clPack
			setRingTail(&aRingBuffer1, updatedTail)
			setRingTail(&aRingBuffer2, updatedTail)
		}
		if closeClSession == true {
			break
		}
	}
	if updatedTail > 0 {
		returnSingleDp2(clChan, subscriptionId, paths)
	}
}

func clAnalyze2dim(aRingBuffer1 *RingBuffer, aRingBuffer2 *RingBuffer, bufSize int, maxError float64) (string, string, int) {
	clBuffer1 := make([]CLBufElement, bufSize)
	clBuffer2 := make([]CLBufElement, bufSize)
	clBuffer1 = transformDataPoints(aRingBuffer1, clBuffer1, bufSize)
	clBuffer2 = transformDataPoints(aRingBuffer2, clBuffer2, bufSize)
	savedIndex := clReduction2Dim(clBuffer1, clBuffer2, 0, bufSize-1, maxError)
	dataPoint1 := ""
	dataPoint2 := ""
	updatedTail := 0
	if savedIndex != nil {
		sort.Sort(sort.Reverse(sort.IntSlice(savedIndex)))
		updatedTail = savedIndex[len(savedIndex)-1]
		if len(savedIndex) > 1 {
			dataPoint1 += "["
			dataPoint2 += "["
		}
		for i := 0; i < len(savedIndex); i++ {
			val1, ts1 := readRing(aRingBuffer1, savedIndex[i])
			dataPoint1 += `{"value":"` + val1 + `","ts":"` + ts1 + `"},`
			val2, ts2 := readRing(aRingBuffer2, savedIndex[i])
			dataPoint2 += `{"value":"` + val2 + `","ts":"` + ts2 + `"},`
		}
		dataPoint1 = dataPoint1[:len(dataPoint1)-1]
		dataPoint2 = dataPoint2[:len(dataPoint2)-1]
		if len(savedIndex) > 1 {
			dataPoint1 += "]"
			dataPoint2 += "]"
		}
	} else {
		val1, ts1 := readRing(aRingBuffer1, 0)
		dataPoint1 += `{"value":"` + val1 + `","ts":"` + ts1 + `"}`
		val2, ts2 := readRing(aRingBuffer2, 0)
		dataPoint2 += `{"value":"` + val2 + `","ts":"` + ts2 + `"}`
	}
	return dataPoint1, dataPoint2, updatedTail
}

func clReduction2Dim(clBuffer1 []CLBufElement, clBuffer2 []CLBufElement, firstIndex int, lastIndex int, maxError float64) []int {
	if lastIndex-firstIndex <= 1 {
		return nil
	}
	var maxMeasuredError float64 = 0.0
	indexOfMaxMeasuredError := firstIndex
	var measuredError float64

	linearSlope1 := (clBuffer1[lastIndex].Value - clBuffer1[firstIndex].Value) / (float64)(clBuffer1[lastIndex].Timestamp-clBuffer1[firstIndex].Timestamp)
	linearSlope2 := (clBuffer2[lastIndex].Value - clBuffer2[firstIndex].Value) / (float64)(clBuffer2[lastIndex].Timestamp-clBuffer2[firstIndex].Timestamp)

	for i := 0; i <= lastIndex-firstIndex; i++ {
		errorDim1 := clBuffer1[firstIndex+i].Value - (clBuffer1[firstIndex].Value + linearSlope1*(float64)(clBuffer1[firstIndex+i].Timestamp-clBuffer1[firstIndex].Timestamp))
		errorDim2 := clBuffer2[firstIndex+i].Value - (clBuffer2[firstIndex].Value + linearSlope2*(float64)(clBuffer2[firstIndex+i].Timestamp-clBuffer2[firstIndex].Timestamp))
		measuredError = errorDim1*errorDim1 + errorDim2*errorDim2 // sqrt omitted, instead maxError squared below
		if measuredError > maxMeasuredError {
			maxMeasuredError = measuredError
			indexOfMaxMeasuredError = firstIndex + i
		}
	}

	if maxMeasuredError > maxError*maxError { // squared as sqrt omitted above
		var savedIndex1, savedIndex2 []int
		savedIndex1 = append(savedIndex1, clReduction2Dim(clBuffer1, clBuffer2, firstIndex, indexOfMaxMeasuredError, maxError)...)
		savedIndex2 = append(savedIndex2, clReduction2Dim(clBuffer1, clBuffer2, indexOfMaxMeasuredError, lastIndex, maxError)...)
		savedIndex1 = append(savedIndex1, savedIndex2...)
		return append(savedIndex1, indexOfMaxMeasuredError)
	}
	return nil
}

func clCapture3dim(clChan chan CLPack, subscriptionId int, paths Dim3Elem, bufSize int, maxError float64) {
	aRingBuffer1 := createRingBuffer(bufSize + 1)
	aRingBuffer2 := createRingBuffer(bufSize + 1)
	aRingBuffer3 := createRingBuffer(bufSize + 1)
	var dpMap1 = make(map[string]interface{})
	var dpMap2 = make(map[string]interface{})
	var dpMap3 = make(map[string]interface{})
	closeClSession := false
	oldTime := getCurrentUtcTime()
	updatedTail := 0
	for {
		newTime := getCurrentUtcTime()
		sleepPeriod := getSleepDuration(newTime, oldTime, 800) // TODO: Iteration period should be configurable, set to less than sample freq of signal.
		if sleepPeriod < 0 {
			utils.Warning.Printf("Curve logging may have missed to capture.")
		}
		time.Sleep(sleepPeriod)
		oldTime = getCurrentUtcTime()
		mcloseClSubId.Lock()
		if closeClSubId == subscriptionId {
			closeClSession = true
		}
		mcloseClSubId.Unlock()
		dp1 := getVehicleData(paths.Path1)
		dp2 := getVehicleData(paths.Path2)
		dp3 := getVehicleData(paths.Path3)
		utils.MapRequest(dp1, &dpMap1)
		utils.MapRequest(dp2, &dpMap2)
		utils.MapRequest(dp3, &dpMap3)
		_, ts1 := readRing(&aRingBuffer1, 0)
		_, ts2 := readRing(&aRingBuffer2, 0)
		_, ts3 := readRing(&aRingBuffer3, 0)
		if ts1 != dpMap1["ts"].(string) && ts2 != dpMap2["ts"].(string) && ts3 != dpMap3["ts"].(string) &&
			dpMap1["ts"].(string) == dpMap2["ts"].(string) && dpMap2["ts"].(string) == dpMap3["ts"].(string) {
			writeRing(&aRingBuffer1, dpMap1["value"].(string), dpMap1["ts"].(string))
			writeRing(&aRingBuffer2, dpMap2["value"].(string), dpMap2["ts"].(string))
			writeRing(&aRingBuffer3, dpMap3["value"].(string), dpMap3["ts"].(string))
		}
		currentBufSize := getNumOfPopulatedRingElements(&aRingBuffer1)
		if (currentBufSize == bufSize) || (closeClSession == true) {
			data1, data2, data3, updatedTail := clAnalyze3dim(&aRingBuffer1, &aRingBuffer2, &aRingBuffer3, currentBufSize, maxError)
			var clPack CLPack
			clPack.DataPack = `[{"path":"` + paths.Path1 + `","dp":` + data1 + `},{"path":"` + paths.Path2 + `","dp":` + data2 + `},{"path":"` + paths.Path3 + `","dp":` + data3 + "}]"
			clPack.SubscriptionId = subscriptionId
			clChan <- clPack
			setRingTail(&aRingBuffer1, updatedTail)
			setRingTail(&aRingBuffer2, updatedTail)
			setRingTail(&aRingBuffer3, updatedTail)
		}
		if closeClSession == true {
			break
		}
	}
	if updatedTail > 0 {
		returnSingleDp3(clChan, subscriptionId, paths)
	}
}

func clAnalyze3dim(aRingBuffer1 *RingBuffer, aRingBuffer2 *RingBuffer, aRingBuffer3 *RingBuffer, bufSize int, maxError float64) (string, string, string, int) {
	clBuffer1 := make([]CLBufElement, bufSize)
	clBuffer2 := make([]CLBufElement, bufSize)
	clBuffer3 := make([]CLBufElement, bufSize)
	clBuffer1 = transformDataPoints(aRingBuffer1, clBuffer1, bufSize)
	clBuffer2 = transformDataPoints(aRingBuffer2, clBuffer2, bufSize)
	clBuffer3 = transformDataPoints(aRingBuffer3, clBuffer3, bufSize)
	savedIndex := clReduction3Dim(clBuffer1, clBuffer2, clBuffer3, 0, bufSize-1, maxError)
	dataPoint1 := ""
	dataPoint2 := ""
	dataPoint3 := ""
	updatedTail := 0
	if savedIndex != nil {
		sort.Sort(sort.Reverse(sort.IntSlice(savedIndex)))
		updatedTail = savedIndex[len(savedIndex)-1]
		if len(savedIndex) > 1 {
			dataPoint1 += "["
			dataPoint2 += "["
			dataPoint3 += "["
		}
		for i := 0; i < len(savedIndex); i++ {
			val1, ts1 := readRing(aRingBuffer1, savedIndex[i])
			dataPoint1 += `{"value":"` + val1 + `","ts":"` + ts1 + `"},`
			val2, ts2 := readRing(aRingBuffer2, savedIndex[i])
			dataPoint2 += `{"value":"` + val2 + `","ts":"` + ts2 + `"},`
			val3, ts3 := readRing(aRingBuffer3, savedIndex[i])
			dataPoint3 += `{"value":"` + val3 + `","ts":"` + ts3 + `"},`
		}
		dataPoint1 = dataPoint1[:len(dataPoint1)-1]
		dataPoint2 = dataPoint2[:len(dataPoint2)-1]
		dataPoint3 = dataPoint3[:len(dataPoint3)-1]
		if len(savedIndex) > 1 {
			dataPoint1 += "]"
			dataPoint2 += "]"
			dataPoint3 += "]"
		}
	} else {
		val1, ts1 := readRing(aRingBuffer1, 0)
		dataPoint1 += `{"value":"` + val1 + `","ts":"` + ts1 + `"}`
		val2, ts2 := readRing(aRingBuffer2, 0)
		dataPoint2 += `{"value":"` + val2 + `","ts":"` + ts2 + `"}`
		val3, ts3 := readRing(aRingBuffer3, 0)
		dataPoint3 += `{"value":"` + val3 + `","ts":"` + ts3 + `"}`
	}
	return dataPoint1, dataPoint2, dataPoint3, updatedTail
}

func clReduction3Dim(clBuffer1 []CLBufElement, clBuffer2 []CLBufElement, clBuffer3 []CLBufElement, firstIndex int, lastIndex int, maxError float64) []int {
	if lastIndex-firstIndex <= 1 {
		return nil
	}
	var maxMeasuredError float64 = 0.0
	indexOfMaxMeasuredError := firstIndex
	var measuredError float64

	linearSlope1 := (clBuffer1[lastIndex].Value - clBuffer1[firstIndex].Value) / (float64)(clBuffer1[lastIndex].Timestamp-clBuffer1[firstIndex].Timestamp)
	linearSlope2 := (clBuffer2[lastIndex].Value - clBuffer2[firstIndex].Value) / (float64)(clBuffer2[lastIndex].Timestamp-clBuffer2[firstIndex].Timestamp)
	linearSlope3 := (clBuffer3[lastIndex].Value - clBuffer3[firstIndex].Value) / (float64)(clBuffer3[lastIndex].Timestamp-clBuffer3[firstIndex].Timestamp)

	for i := 0; i <= lastIndex-firstIndex; i++ {
		errorDim1 := clBuffer1[firstIndex+i].Value - (clBuffer1[firstIndex].Value + linearSlope1*(float64)(clBuffer1[firstIndex+i].Timestamp-clBuffer1[firstIndex].Timestamp))
		errorDim2 := clBuffer2[firstIndex+i].Value - (clBuffer2[firstIndex].Value + linearSlope2*(float64)(clBuffer2[firstIndex+i].Timestamp-clBuffer2[firstIndex].Timestamp))
		errorDim3 := clBuffer3[firstIndex+i].Value - (clBuffer3[firstIndex].Value + linearSlope3*(float64)(clBuffer3[firstIndex+i].Timestamp-clBuffer3[firstIndex].Timestamp))
		measuredError = errorDim1*errorDim1 + errorDim2*errorDim2 + errorDim3*errorDim3 // sqrt omitted, instead maxError squared below
		if measuredError > maxMeasuredError {
			maxMeasuredError = measuredError
			indexOfMaxMeasuredError = firstIndex + i
		}
	}

	if maxMeasuredError > maxError*maxError { // squared as sqrt omitted above
		var savedIndex1, savedIndex2 []int
		savedIndex1 = append(savedIndex1, clReduction3Dim(clBuffer1, clBuffer2, clBuffer3, firstIndex, indexOfMaxMeasuredError, maxError)...)
		savedIndex2 = append(savedIndex2, clReduction3Dim(clBuffer1, clBuffer2, clBuffer3, indexOfMaxMeasuredError, lastIndex, maxError)...)
		savedIndex1 = append(savedIndex1, savedIndex2...)
		return append(savedIndex1, indexOfMaxMeasuredError)
	}
	return nil
}
