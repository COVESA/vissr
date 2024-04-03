/******** Peter Winzell (c), 3/19/24 *********************************************/

package grpcMgr

import (
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"os/exec"
	"testing"
)

// set up grpc connection to VISS server
func getGRPCConnectionToVISSServer() (*grpc.ClientConn, error) {

	conn, err := grpc.Dial("0.0.0.0"+":8887", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())

	if err != nil {
		fmt.Printf("did not connect: %v", err)
		return nil, err
	}
	return conn, nil
}

// Test multiple grpc connections, viss server need to be up locally.
func TestMultGrpcConnections(t *testing.T) {
	c1, err_1 := getGRPCConnectionToVISSServer()
	c2, err_2 := getGRPCConnectionToVISSServer()

	if err_1 != nil || err_2 != nil {
		t.Errorf("Error in grpc connection")
	}
	c1.Close()
	c2.Close()
}

// Test one sub one get at the same time
func TestMultSubscription(t *testing.T) {

	exec.Command(`go run server/vissv2server/grpcMgr/testprocess/gRPCClient.go`).Run()
	exec.Command(`go run server/vissv2server/grpcMgr/testprocess/gRPCClient.go`).Run()
	t.Log("test done")
}
