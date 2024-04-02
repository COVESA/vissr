/**
* (C) 2021 Mitsubishi Electrics Automotive
* (C) 2021 Geotab Inc
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

package main

import (
	"fmt"
	"net"
	"os"

	"github.com/covesa/vissr/utils"
)

func main() {
	buf := make([]byte, 128)
	//	conn, err := net.Dial("unix", "/tmp/vissv2/histctrlserver.sock")
	conn, err := net.Dial("unix", utils.GetUdsPath("Vehicle", "history"))
	if err != nil {
		utils.Error.Printf("HistCtrlClient:Accept failed, err = %s", err)
		os.Exit(-1)
	}
	defer conn.Close()

	var command string
	fmt.Printf("********************* History control client ****************\n")
	for {
		fmt.Printf("Select command: c(reate)/s(tart)/(sto)p/d(elete)/q(uit): ")
		fmt.Scanf("%s\n", &command)
		payLoad := ""
		switch command[0] {
		case 'c':
			fallthrough
		case 'C': // {"action":"create", "path": X, "buf-size":"Y"}
			var path string
			var bufSize string
			fmt.Printf("Path=")
			fmt.Scanf("%s\n", &path)
			fmt.Printf("Buffer size=")
			fmt.Scanf("%s\n", &bufSize)
			payLoad = `{"action": "create", "path":"` + path + `", "buf-size":"` + bufSize + `"}`
		case 's':
			fallthrough
		case 'S': // {"action":"start", "path": X, "frequency":"Z"}
			var path string
			var freq string
			fmt.Printf("Path=")
			fmt.Scanf("%s\n", &path)
			fmt.Printf("Frequency (captures/hr)=")
			fmt.Scanf("%s\n", &freq)
			payLoad = `{"action": "start", "path":"` + path + `", "frequency":"` + freq + `"}`
		case 'p':
			fallthrough
		case 'P': // {"action":"stop", "path": X}
			var path string
			fmt.Printf("Path=")
			fmt.Scanf("%s\n", &path)
			payLoad = `{"action": "stop", "path":"` + path + `"}`
		case 'd':
			fallthrough
		case 'D': // {"action":"delete", "path": X}
			var path string
			fmt.Printf("Path=")
			fmt.Scanf("%s\n", &path)
			payLoad = `{"action": "delete", "path":"` + path + `"}`
		default: // quit
			conn.Close()
			os.Exit(0)
		}
		_, err := conn.Write([]byte(payLoad))
		if err != nil {
			utils.Error.Printf("HistCtrlClient:Write failed, err = %s", err)
			os.Exit(-1)
		}
		n, err := conn.Read(buf)
		if err != nil {
			utils.Error.Printf("HistCtrlClient:Read failed, err = %s", err)
			os.Exit(-1)
		}
		fmt.Printf("Server response: %s\n", string(buf[:n]))
	}
}
