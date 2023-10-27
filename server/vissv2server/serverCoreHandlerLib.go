/**
* (C) 2022 Geotab Inc
* (C) 2020 Mitsubishi Electric Automotive
* (C) 2019 Geotab Inc
* (C) 2019 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/w3c/automotive-viss2
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"encoding/json"
	"github.com/w3c/automotive-viss2/utils"
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
	utils.Info.Printf("initVssPathListServer():Response=%s...(truncated to 100 bytes)", bytes[0:101])
}
