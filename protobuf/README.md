**(C) 2024 Foed Motor Cmpany**<br>
**(C) 2022 Geotab Inc**<br>

# Protobuf implementation of the VISSv2 payload messages

The VISSv3messages.proto file contains a definition that encompasses all payload messages that the VISSv3 standard defines for the Websocket, and MQTT protocols. For HTTP the requests carry most parts of this not as a payload but explicitly in the protocol, so this design may need some tweaks for supporting HTTP. At a minimum the code that transforms between JSON and protobuf would need modifications.<br>

The VISSv3messages.proto file is used as input to the protoc tool. To generate a Golang output file, the following command can be used:<br>
$ protoc --go_out=protoc-out VISSv3messages.proto<br>
which creates the VISSv3messages.pb.go file in the protoc-out directory.<br>

The different type of messages that serialised protobuf blob supports are the following:<br>

Request and response messages for the actions get/set/subscribe/unsubscribe.<br>
Notification messages for the action subscribe.<br>
Response and notification messages can be either be success, or error messages.<br>
This computes to thirteen different message types that need to be supported, and this is signalled within the protobuf blob by the enums MessageMethod, MessageType, and ResponseStatus.<br>
The protobuf design also supports two levels of compression, one where all data is of string type, and one where paths and timestamps are encoded into int32 format, see README in the utils directory for more information.


