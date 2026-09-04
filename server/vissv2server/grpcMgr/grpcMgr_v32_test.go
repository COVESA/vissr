/**
* (C) 2026 Ford Motor Company
*
* All files and artifacts in the repository at https://github.com/covesa/vissr
* are licensed under the provisions of the license provided by the LICENSE
* file in this repository.
*
* ----------------------------------------------------------------------------
*
* Tests for the VISSv3.2 gRPC server methods added alongside GetRequest /
* SetRequest / UnsubscribeRequest in grpcMgr.go:
*
*   - MultiGetRequest    (CORE section 5.1.1.2, Multiple Tree Addressability Read)
*   - MultiSetRequest    (CORE section 5.2.2, Multiple Data Update)
*   - DiscoverRequest    (CORE section 5.5, Discovery)
*
* Same shape as TestGetRequest_ForwardsAndResponds /
* TestSetRequest_ForwardsAndResponds in grpcMgr_test.go: simulate the
* manager hub with makeHubSimulator, drive the unary RPC method
* directly, and confirm it doesn't error or panic on the round trip.
**/
package grpcMgr

import (
	"context"
	"testing"

	pb "github.com/covesa/vissr/grpc_pb"
)

// TestMultiGetRequest_ForwardsAndResponds: a MultiGetRequest RPC
// converts the proto message to JSON, dispatches it, and converts the
// response back, mirroring TestGetRequest_ForwardsAndResponds.
func TestMultiGetRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"get","requestId":"999","data":[{"path":"Vehicle.Speed","dp":{"value":"1","ts":"2026-01-01T00:00:00Z"}}],"ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.MultiGetRequestMessage{Data: []string{"Vehicle.Speed", "Vehicle.Cabin.Door.Row1.DriverSide.IsOpen"}, RequestId: "999"}
	resp, err := srv.MultiGetRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("MultiGetRequest returned error: %v", err)
	}
	if resp == nil {
		t.Fatalf("MultiGetRequest returned nil response")
	}
	if resp.GetStatus() != pb.ResponseStatus_SUCCESS {
		t.Errorf("Status = %v; want SUCCESS", resp.GetStatus())
	}
	data := resp.GetSuccessResponse().GetDataPack().GetData()
	if len(data) != 1 || data[0].GetPath() != "Vehicle.Speed" {
		t.Errorf("unexpected DataPack: %+v", data)
	}
}

// TestMultiGetRequest_ErrorResponse confirms an error response coming
// back from the hub is converted to a proto ERROR status rather than
// panicking.
func TestMultiGetRequest_ErrorResponse(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"get","requestId":"999","error":{"number":"404","reason":"unavailable_data","description":"x"},"ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.MultiGetRequestMessage{Data: []string{"NoSuchPath"}, RequestId: "999"}
	resp, err := srv.MultiGetRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("MultiGetRequest returned error: %v", err)
	}
	if resp.GetStatus() != pb.ResponseStatus_ERROR {
		t.Errorf("Status = %v; want ERROR", resp.GetStatus())
	}
}

// TestMultiSetRequest_ForwardsAndResponds: mirrors
// TestSetRequest_ForwardsAndResponds for the additive MultiSetRequest
// RPC, which reuses SetResponseMessage as its return type.
func TestMultiSetRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"set","requestId":"999","ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.MultiSetRequestMessage{
		Data:      []*pb.MultiSetRequestMessage_SetData{{Path: "Vehicle.Powertrain.Transmission.PerformanceMode", Value: "sport"}},
		RequestId: "999",
	}
	resp, err := srv.MultiSetRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("MultiSetRequest returned error: %v", err)
	}
	if resp == nil {
		t.Fatalf("MultiSetRequest returned nil response")
	}
	if resp.GetStatus() != pb.ResponseStatus_SUCCESS {
		t.Errorf("Status = %v; want SUCCESS", resp.GetStatus())
	}
}

// TestDiscoverRequest_ForwardsAndResponds drives the unary
// DiscoverRequest RPC end to end against a simulated hub response
// containing a "metadata" object, confirming no panic and a correctly
// classified SUCCESS status.
func TestDiscoverRequest_ForwardsAndResponds(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"discover","requestId":"999","metadata":{"FuelSystem":{"type":"branch"}},"ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.DiscoverRequestMessage{Path: "Vehicle.Powertrain.FuelSystem", Depth: "2", RequestId: "999"}
	resp, err := srv.DiscoverRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("DiscoverRequest returned error: %v", err)
	}
	if resp == nil {
		t.Fatalf("DiscoverRequest returned nil response")
	}
	if resp.GetStatus() != pb.ResponseStatus_SUCCESS {
		t.Errorf("Status = %v; want SUCCESS", resp.GetStatus())
	}
	if resp.GetMetadata() == "" {
		t.Errorf("expected non-empty Metadata")
	}
}

// TestDiscoverRequest_ErrorResponse confirms an error response coming
// back from the hub is converted to a proto ERROR status.
func TestDiscoverRequest_ErrorResponse(t *testing.T) {
	initLists()
	defer initLists()

	fakeResp := `{"action":"discover","requestId":"999","error":{"number":"400","reason":"invalid_data","description":"x"},"ts":"2026-01-01T00:00:00Z"}`
	done := makeHubSimulator(fakeResp)

	srv := &Server{}
	in := &pb.DiscoverRequestMessage{Path: "NoSuchTree", Depth: "0", RequestId: "999"}
	resp, err := srv.DiscoverRequest(context.Background(), in)
	<-done

	if err != nil {
		t.Fatalf("DiscoverRequest returned error: %v", err)
	}
	if resp.GetStatus() != pb.ResponseStatus_ERROR {
		t.Errorf("Status = %v; want ERROR", resp.GetStatus())
	}
}
