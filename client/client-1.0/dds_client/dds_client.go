/**
* (C) 2026 Matt Jones
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/

// dds_client is a VISS v2 command-line client that uses DDS as its transport.
//
// Wire protocol (mirrors VISS over MQTT):
//
//	Request  topic : /<VIN>/Vehicle
//	Payload        : {"replyTopic":"<unique>","request":{...VISS JSON...}}
//	Response topic : <unique>
//
// The client subscribes to a randomly-generated reply topic before entering
// the request loop. Each request is published to /<VIN>/Vehicle wrapped in the
// envelope above. Responses arrive asynchronously on the reply topic.
//
// By default the in-process mock DDS implementation is used (no system library
// required). Rebuild with -tags cyclone (libcyclonedds-dev required) for a real
// network transport.
//
// Integration-only entry points (main, subscription loop) are not unit-tested;
// only buildEnvelope is unit-testable and is covered in dds_client_test.go.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/akamensky/argparse"
	"github.com/covesa/vissr/utils"
	"github.com/google/uuid"

	dds "github.com/SoundMatt/go-DDS"
)

// newParticipant is set by the build-tag-conditional backend file
// (dds_client_mock.go or dds_client_cyclone.go). Tests may override it.
var newParticipant func() (dds.Participant, error)

// clientDomain is the DDS domain to connect to. Set from --dds-domain before
// newParticipant is called; backend init() closures read it at call time.
var clientDomain = dds.Domain(0)

// buildEnvelope wraps a raw VISS JSON request in the DDS wire envelope:
//
//	{"replyTopic":"<replyTopic>","request":{...}}
//
// Returns ("", false) when request is not valid JSON so callers can log the
// error and skip publishing.
func buildEnvelope(replyTopic, request string) (string, bool) {
	if !json.Valid([]byte(request)) {
		utils.Error.Printf("dds_client: request is not valid JSON: %s", request)
		return "", false
	}
	envelope := map[string]interface{}{
		"replyTopic": replyTopic,
		"request":    json.RawMessage(request),
	}
	// json.Marshal on a map[string]interface{} with a validated RawMessage
	// value cannot fail; ignore the error.
	b, _ := json.Marshal(envelope)
	return string(b), true
}

func main() {
	parser := argparse.NewParser("dds_client", "VISS v2 DDS client")
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info",
	})
	vin := parser.String("", "vin", &argparse.Options{
		Required: true,
		Help:     "Vehicle VIN — determines the DDS request topic (/<VIN>/Vehicle)",
		Default:  "ULFB0",
	})
	ddsDomainFlag := parser.Int("", "dds-domain", &argparse.Options{
		Required: false,
		Help:     "DDS domain ID (default 0)",
		Default:  0,
	})

	if err := parser.Parse(os.Args); err != nil {
		fmt.Print(parser.Usage(err))
		os.Exit(1)
	}

	clientDomain = dds.Domain(*ddsDomainFlag)
	utils.InitLog("dds-client-log.txt", "./logs", *logFile, *logLevel)

	participant, err := newParticipant()
	if err != nil {
		utils.Error.Printf("dds_client: cannot create DDS participant: %v", err)
		os.Exit(1)
	}
	defer participant.Close()

	// Unique reply topic so multiple clients on the same host don't cross-receive.
	replyTopic := "/client/reply/" + uuid.New().String()

	sub, err := participant.NewSubscriber(replyTopic, dds.DefaultQoS)
	if err != nil {
		utils.Error.Printf("dds_client: subscribe to %q failed: %v", replyTopic, err)
		os.Exit(1)
	}
	defer sub.Close()

	// Print responses as they arrive.
	go func() {
		for sample := range sub.C() {
			fmt.Printf("\nResponse: %s\n", string(sample.Payload))
			fmt.Printf("VISSv2 request (or q to quit): ")
		}
	}()

	vehicleTopic := "/" + *vin + "/Vehicle"
	pub, err := participant.NewPublisher(vehicleTopic, dds.DefaultQoS)
	if err != nil {
		utils.Error.Printf("dds_client: publish to %q failed: %v", vehicleTopic, err)
		os.Exit(1)
	}
	defer pub.Close()

	fmt.Printf("DDS VISS client — reply topic: %s\n", replyTopic)
	fmt.Printf("VISSv2 request (or q to quit): ")

	var request string
	count := 0
	for {
		if _, err := fmt.Scanf("%s", &request); err != nil || len(request) == 0 {
			continue
		}
		if request[0] == 'q' {
			break
		}
		envelope, ok := buildEnvelope(replyTopic, request)
		if !ok {
			fmt.Printf("Invalid JSON — enter a well-formed VISS request\nVISSv2 request (or q to quit): ")
			continue
		}
		if err := pub.Write([]byte(envelope)); err != nil {
			utils.Error.Printf("dds_client: publish failed: %v", err)
		}
		count++
		if count >= 25 {
			fmt.Println("Max requests reached. Goodbye.")
			break
		}
		time.Sleep(2 * time.Second)
	}
}
