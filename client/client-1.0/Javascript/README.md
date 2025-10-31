**(C) 2019 Volvo Cars**<br>
**(C) 2020 Geotab Inc**<br>

# Project: VISSR: client/client-1.0

Client implementations for communication with the server found at the server/server-1.0 directory.<br>
The functionality in the VISS specs shall be supported, with features such as:<br>
 HTTP and WebSockets transport protocols<br>
-- Queries<br>
-- Access control <br><br>
The clients can be executed either locally on the same machine as the server, or remotely on a different machine.<br>
The project plans to deploy a publicly accessible server, allowing interested testers to skip building the GO based server. <br>
The client examples in this directory could then just be opened in any browser.<br><br>
Current limitations: <br>
- The following client limitations are due to server limitations. <br>
- The transport protocols are not the secure versions of Websockets and HTTP. <br>
- Max twenty parallel app-clients for the Websocket protocol, any number for HTTP. <br>
- Access control is implemented with the following restrictions. <br>
-- Client authentication is not implemented, and always returns a successful verification.<br>
- Responses for error cases may not be completely according to spec.<br>
- If the server does not find a statestorage DB in its directory from where a value can be read, then the server returns a dummy value.<br>
-- The dummy valu is read from a counter [0-999] that is incremented every 47 msec..<br>
- Set requests do not lead to an update with the provided value.<br>

The available clients are:<br>
- httpclient.html              // uses the HTTP protocol to communicate with the VISS server. Payload encoding not invoked.<br>
- wsclient.html   // uses the Websocket protocol to communicate with the VISS server. Payload encoding not invoked.<br>
- wsclient_protoencode.html   // uses the Websocket protocol to communicate with the VISS server. Protobuf payload encoding invoked.<br>
- agtclient.html   // uses the Websocket protocol to communicate with the Access Grant server.<br>
- atclient.html   // uses the Websocket protocol to communicate with the Access token server.<br>

The appclient_commands.txt file contains some example payloads that a client may use. Depending on the VSS tree used by the VISS server, 
the paths used in the examples may not address existing nodes in the tree.

## Build instructions:
If the server to be used is not the one deployed by the project for public access, see below, then it must be built first, see build instructions in the root directory. <br>
The HTML clients found here can be opened in any browser. <br>

## Usage instructions:
Before issuing any requests to the server, the server IP address (or domain name) must be set. <br>
Even if the server runs on the same machine the machine outbound IP must be set, followed by a click on the Server IP button. <br>
If the server to be used is the one deployed by the project for public access, then the URL is:<br>
!!!Deployment details not yet resolved. Will be shown here when available!!! <br>
After the server URL is set, then requests can be issued to the server. <br>
For request examples, see the file appclient_commands.txt that contains request examples for both Websockets and HTTP transport protocols.  <br>
The VSS paths shown in the examples can be replaced by any path that the VSS tree at the VSS Github project supports (https://github.com/GENIVI/vehicle_signal_specification). <br><br>

The VISS access control model describes two authorization servers, the Access Grant Token (AGT) server, and the Access Token (AT) server. <br>
To obtain an Access Grant token the agtclient.html can be used. The IP address is the same as for the VISS server, the path is "agtserver",<br> 
and the request to the agtserver must be a JSON formatted message with the following format<br>
{"vin":"XXX", "context":"Independent+OEM+Cloud", "proof":"ABC", "key":"DEF"}<br>
where the "vin" value can be replaced by any (fictious) VIN number, the "context" value with any role triplet that the VISS spec supports,
 the "proof" value with anything as authentication is not implemented.<br>
 If a "key" parameter is included, to initiate a Long Term Flow, it may have any value as it is currently not used.<br>
The response contains the Access Grant Token, that is used as input in the following request to the AT server.<br>
To issue this request, the atclient.html can be opened in a browser, input the same IP address, and as path "atserver", then the request to the AT server shall have the following JSON format:<br>
{"token":"ag-token", "purpose":"short-name-from-purpose-list", "pop":"GHI"}<br>
where the "token" value is the token received from the AGT server, 
the "purpose" value must be a short name on the Purpose list (where only "fuel-status" is available as an example).<br>
The "pop" parameter shall only be included if a "key" parameter was included in the AGT request, the value can be anything as it is currently not used.<br>
If the AGT token is verified as valid, which it should be if it comes from the AGT server, the response contains the AT token that can then be used in requests to the VISS server.<br><br>
The access control model implements the Access control selection model described in the VISS specification,so to enable testing of access control, the VSS tree must be updated with access control metadata. This is done by adding the key-value pair "validate:X" to a node in a vspec file, 
where X must be either "write-only" or "read-write". 
This can be inserted into any node, also branch nodes, in which case the access control is inherited by all nodes being descendants of that node.<br>
E.g if in the branch node "Body" the metadata "validate:read-write" is inserted, then all signals in the subtree having "Vehicle.Body" as the root will require a token with ReadWrite scope for any read or write requests.<br>
Please see the <a href="https://github.com/COVESA/vehicle-information-service-specification">COVESA VISS CORE spec, Access Control chapter</a> for more info.

