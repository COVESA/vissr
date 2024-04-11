/**
* (C) 2023 Ford Motor Company
* (C) 2023 Volvo Cars
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE file in this repository.
*
**/
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"github.com/akamensky/argparse"
	pb "github.com/covesa/vissr/grpc_pb"
	utils "github.com/covesa/vissr/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var clientCert tls.Certificate
var caCertPool x509.CertPool

const (
	address = "0.0.0.0"
	name    = "VISSv2-gRPC-client"
)

var grpcCompression utils.Compression

var commandList []string

func initCommandList() {
	commandList = make([]string, 4) // 0->GET, 1->SET, 2->SUBSCRIBE, X%10=3->UNSUBSCRIBE

	commandList[0] = `{"action":"get","path":"Vehicle/Speed","requestId":"232"}`
	commandList[1] = `{"action":"set", "path":"Vehicle/Body/Lights/IsLeftIndicatorOn", "value":"true", "requestId":"245"}`
	//commandList[2] = `{"action":"subscribe","path":"Vehicle","filter":[{"type":"paths","parameter":["Speed", "Chassis.Accelerator.PedalPosition"]},{"type":"timebased","parameter":	{"period":"5000"}}],"requestId":"246"}`
	commandList[2] = `{"action":"subscribe","path":"Vehicle.Speed","filter":{"type":"curvelog","parameter":{"maxerr":"2","bufsize":"15"}},"requestId":"285"}`
	/*	commandList[2] = `{"action":"subscribe","path":"Vehicle","filter":[{"type":"paths","parameter":["Speed", "Chassis.Accelerator.PedalPosition"]},{"type":"timebased","parameter":	{"period":"5000"}}],"requestId":"246"}`*/
	commandList[3] = `{"action":"unsubscribe","subscriptionId":"X","requestId":"240"}` // X is replaced according to input
	//commandList[2] = `{"action":"subscribe","path":"Vehicle.Speed","filter":{"type":"curvelog","parameter":{"maxerr":"5","bufsize":"60"}},"requestId":"285"}`
	/* different variants
	commandList[2] = `{"action":"subscribe","path":"Vehicle","filter":[{"type":"paths","parameter":["Speed","CurrentLocation.Latitude", "CurrentLocation.Longitude"]}, {"type":"timebased","parameter":{"period":"100"}}],"requestId":"285"}`
	commandList[1] = `{"action":"subscribe","path":"Vehicle/Speed","filter":{"type":"timebased","parameter":{"period":"100"}},"requestId":"246"}`
		commandList[0] = `{"action":"get","path":"Vehicle/Cabin/Door/Row1/Right/IsOpen","requestId":"232"}`
	commandList[0] = `{"action":"get","path":"Vehicle/Cabin/Door","filter":{"type":"paths","parameter":"*.*.IsOpen"},"requestId":"235"}`
		commandList[1] = `{"action":"subscribe","path":"Vehicle/Cabin/Door/Row1/Right/IsOpen","filter":{"type":"timebased","parameter":{"period":"3000"}},"requestId":"246"}`
	commandList[1] = `{"action":"subscribe","path":"Vehicle","filter":{"type":"paths","parameter":["Speed", "Chassis.Accelerator.PedalPosition"]},"requestId":"246"}`
	commandList[1] = `{"action":"subscribe","path":"Vehicle/Speed","requestId":"258"}`
	commandList[1] = `{"action":"subscribe","path":"Vehicle","filter":[{"type":"paths","parameter":["Body.Lights.IsLeftIndicatorOn", "Chassis.Accelerator.PedalPosition"]}, {"type":"change","parameter":{"logic-op":"ne", "diff": "0"}}],"requestId":"285"}`
	commandList[1] = {"action":"subscribe","path":"Vehicle","filter":{"type":"paths","parameter":["Speed", "Chassis.Accelerator.PedalPosition"]},"requestId":"246"}`
	*/
}

func noStreamCall(commandIndex int) {
	var conn *grpc.ClientConn
	var err error
	if secConfig.TransportSec == "yes" {
		config := &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      &caCertPool,
		}
		tlsCredentials := credentials.NewTLS(config)
		portNo := secConfig.GrpcSecPort
		conn, err = grpc.Dial(address+portNo, grpc.WithTransportCredentials(tlsCredentials), grpc.WithBlock())
	} else {
		// grpc.Dial

		utils.Info.Printf("connecting to port = 8887")
		conn, err = grpc.Dial(address+":8887", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	}
	if err != nil {
		fmt.Printf("did not connect: %v", err)
		return
	}
	defer conn.Close()
	client := pb.NewVISSv2Client(conn)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	vssRequest := commandList[commandIndex%10]
	var vssResponse string
	switch commandIndex % 10 {
	case 0: //get
		pbRequest := utils.GetRequestJsonToPb(vssRequest, grpcCompression)
		pbResponse, err := client.GetRequest(ctx, pbRequest)
		if err != nil {
			log.Fatal(err)
			return
		}
		vssResponse = utils.GetResponsePbToJson(pbResponse, grpcCompression)
	case 1: // set
		pbRequest := utils.SetRequestJsonToPb(vssRequest, grpcCompression)
		pbResponse, _ := client.SetRequest(ctx, pbRequest)
		vssResponse = utils.SetResponsePbToJson(pbResponse, grpcCompression)
	case 3: //unsubscribe
		subIdIndex := strings.Index(vssRequest, "X")
		vssRequest = vssRequest[:subIdIndex] + strconv.Itoa(commandIndex/10) + vssRequest[subIdIndex+1:]
		fmt.Printf("Unsubscribe request=:%s\n", vssRequest)
		pbRequest := utils.UnsubscribeRequestJsonToPb(vssRequest, grpcCompression)
		pbResponse, _ := client.UnsubscribeRequest(ctx, pbRequest)
		vssResponse = utils.UnsubscribeResponsePbToJson(pbResponse, grpcCompression)
	}
	if err != nil {
		fmt.Printf("Error when issuing request=:%s\n", vssRequest)
	} else {
		fmt.Printf("Received response:%s", vssResponse)
	}
}

func streamCall(commandIndex int) {
	var conn *grpc.ClientConn
	var err error
	if secConfig.TransportSec == "yes" {
		config := &tls.Config{
			Certificates: []tls.Certificate{clientCert},
			RootCAs:      &caCertPool,
		}
		tlsCredentials := credentials.NewTLS(config)
		portNo := secConfig.GrpcSecPort
		conn, err = grpc.Dial(address+portNo, grpc.WithTransportCredentials(tlsCredentials), grpc.WithBlock())
	} else {
		conn, err = grpc.Dial(address+":8887", grpc.WithInsecure(), grpc.WithBlock())
	}
	if err != nil {
		fmt.Printf("did not connect: %v", err)
		return
	}
	defer conn.Close()
	client := pb.NewVISSv2Client(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	vssRequest := commandList[commandIndex]
	pbRequest := utils.SubscribeRequestJsonToPb(vssRequest, grpcCompression)
	stream, err := client.SubscribeRequest(ctx, pbRequest)
	for {
		pbResponse, err := stream.Recv()
		if err != nil {
			fmt.Printf("Error=%v when issuing request=:%s", err, vssRequest)
			break
		}
		vssResponse := utils.SubscribeStreamPbToJson(pbResponse, grpcCompression)
		fmt.Printf("Received response:%s\n", vssResponse)
	}
}

func main() {
	// Create new parser object

	parser := argparse.NewParser("print", "gRPC client")
	// Create string flag
	logFile := parser.Flag("", "logfile", &argparse.Options{Required: false, Help: "outputs to logfile in ./logs folder"})
	logLevel := parser.Selector("", "loglevel", []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}, &argparse.Options{
		Required: false,
		Help:     "changes log output level",
		Default:  "info"})

	// Parse input
	err := parser.Parse(os.Args)
	if err != nil {
		fmt.Print(parser.Usage(err))
	}

	utils.InitLog("grpc_client-log.txt", "./logs", *logFile, *logLevel)
	grpcCompression = utils.PB_LEVEL1
	readTransportSecConfig()
	utils.Info.Printf("secConfig.TransportSec=%s", secConfig.TransportSec)
	if secConfig.TransportSec == "yes" {
		caCertPool = *prepareTransportSecConfig()
	}
	initCommandList()

	fmt.Printf("Command indicies: 0->GET, 1->SET, 2->SUBSCRIBE, X%10=3->UNSUBSCRIBE, any other value terminates.\nFor UNSUBSCRIBE X/10 is the subscriptionId.\n\n")
	var commandIndex int
	for {
		fmt.Printf("\nCommand index:")
		fmt.Scanf("%d", &commandIndex)
		if commandIndex < 0 || commandIndex%10 > 3 {
			break
		}
		fmt.Printf("Selected request:%s\n", commandList[commandIndex%10])
		if commandIndex == 2 { // subscribe
			go streamCall(commandIndex % 10)
		} else {
			go noStreamCall(commandIndex)
		}
		time.Sleep(1 * time.Second)
	}
}
