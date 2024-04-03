/**
* (C) 2022 Geotab Inc
* (C) 2020 Mitsubishi Electric Automotive
* (C) 2019 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"encoding/json"
	"github.com/covesa/vissr/utils"
	"net/http"
)

/*
* Handler for the vsspathlist server
 */
func (pathList *PathList) VssPathListHandler(w http.ResponseWriter, r *http.Request) {
	bytes, err := json.Marshal(pathList)
	if err != nil {
		utils.Error.Printf("problems with json.Marshal, ", err)
		http.Error(w, "Unable to fetch vsspathlist", http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
	//	truncatedIndex := min(len(bytes), 101)
	//	utils.Info.Printf("initVssPathListServer():Response=%s...(truncated to %d bytes)", truncatedIndex-1, bytes[0:truncatedIndex])
	utils.Info.Printf("initVssPathListServer():Response length=%d", len(bytes))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
