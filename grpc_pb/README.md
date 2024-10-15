**(C) 2023 Ford Motor Company**

## gRPC

The gRPC implementation is payload compatible with the Websocket and MQTT implementations.

The following command builds the VISSv3.proto file:

protoc --go_out=. --go_opt=paths=source_relative     --go-grpc_out=. --go-grpc_opt=paths=source_relative     VISSv3.proto


