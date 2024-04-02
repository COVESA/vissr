/******** Peter Winzell (c), 3/22/24 *********************************************/

package main

import (
	"context"
	"fmt"
	pb "github.com/covesa/vissr/grpc_pb"
	"github.com/covesa/vissr/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"sync"
	"time"
)

var grpcCompr utils.Compression
var commandStr []string

func InitCommandStr() {
	commandStr = make([]string, 4)

	commandStr[0] = `{"action":"subscribe","path":"Vehicle","filter":[{"type":"paths","parameter":["Speed"]}, {"type":"timebased","parameter":{"period":"100"}}],"requestId":"285"}`
	commandStr[1] = `{"action":"subscribe","path":"Vehicle","filter":[{"type":"paths","parameter":["Speed"]}, {"type":"timebased","parameter":{"period":"100"}}],"requestId":"286"}`
}

// grpc connection no tls
func getGRPCConnectionToVISSServer() (*grpc.ClientConn, error) {

	conn, err := grpc.Dial("0.0.0.0"+":8887", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())

	if err != nil {
		fmt.Printf("did not connect: %v", err)
		return nil, err
	}
	return conn, nil
}

func gRPCThread(done chan interface{}, wg *sync.WaitGroup, id int) error {
	defer wg.Done()
	c, err := getGRPCConnectionToVISSServer()

	if err != nil {
		wg.Done()
		return err
	}

	// setup grpc stream
	client := pb.NewVISSv2Client(c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	vssRequest := commandStr[0]
	pbRequest := utils.SubscribeRequestJsonToPb(vssRequest, grpcCompr)
	stream, err := client.SubscribeRequest(ctx, pbRequest)
	working := true

	for working {
		select {
		case <-done:
			working = false
			// c.Close()
		default:
			pbResponse, err := stream.Recv()
			if err != nil {
				fmt.Printf("Error=%v when issuing request=:%s", err, vssRequest)
				wg.Done()
			} else {
				fmt.Printf("THREAD ID =%d Received response:%s\n", id, pbResponse.String())
			}
		}
	}
	return nil
}

func main() {

	utils.InitLog("servercore-log.txt", "./logs", false, "Info")
	InitCommandStr()
	done := make(chan interface{})
	var wg sync.WaitGroup

	for i := 0; i < 1; i++ {
		wg.Add(1)
		go gRPCThread(done, &wg, i)
	}

	time.Sleep(60 * time.Second)
	//close the done channel
	close(done)
	//wait for all go routines to quit
	wg.Wait()
	log.Printf(" grpc subscribe done.")
	time.Sleep(180 * time.Second)
}
