---
title: "VISSv2 Clients"
---

## Client deployment options
As is shown in the figure above the VISS interface is well suited to serve clients deployed in different environments:
* Cloud deployment. Typically connected via Internet connectivity.
* Proximity deployment. Typcially in a mobile phone connected via any short range connectivity such as Bluetooth or WiFi.
* In-vehicle deployment. Typically as an app in the infotainment environment.

The payloads handled by the clients at any of these deployments are identical.

The MQTT transport protocol option, with the broker deployed in the cloud,
is well suited for the client cloud deployment as the communication can traverse across subnets.
The thin application layer protocol on top of MQTT that VISS defines makes it possible
to keep the client-server communication, and payload compatibility.

## Client implementations
There are a number of different clients avaliable on this repo, mainly in the client/client-1.0 directory.

### gRPC client
The gRPC implementation uses the protobuf encoding in the VISSv2messages.proto file found [here](https://github.com/covesa/vissr/tree/master/grpc_pb).
The server currently only supports the protobuf level 1 encoding.

### MQTT client
The [MQTT client](https://github.com/UlfBj/vissr/tree/master/client/client-1.0/mqtt_client) implements the application level protocol described in the
[specification](https://raw.githack.com/COVESA/vehicle-information-service-specification/main/spec/VISSv2_Transport.html#application-level-protocol).

### CSV client
The [CSV client]() is developed for testing the [curve logging algorithm](https://www.geotab.com/blog/gps-logging-curve-algorithm/) that Geotab has opened for public cuse.
A client can equest it to be aplied to data by using a [filter](https://raw.githack.com/COVESA/vehicle-information-service-specification/main/spec/VISSv2_Core.html#curvelog-filter-operation) option.

It generates a comma separated (CSV) file in which it saves the curve logged data that it has reuested from the server.
The CSV format makes it easy to import it into an Excel sheet and visualize it as a graph which allows it e. g. to be compared with the original, non-curved data.

### Compression client
The [compression client](https://github.com/covesa/vissr/blob/master/client/client-1.0/compress_client/compress_client.go) can be used for testing three payload compression variants.
* Proprietary compression algorithm
* Protobuf encoding, level 1
* Protobuf encoding, level 2

### Proprietary compression algorithm
This compression variant builds on a proprietary algorithm that takes advantage of knowing the VISSv2 payload format.
Due to its strong dependence on the payload format it might require rewrites if/when the payload format is updated.
It is not kept up to date on this and is therefore likely to crash if an unsupported payload is applied.

### Protobuf encoding
The encoding uses the VISSv2messages.proto file found [here](https://github.com/covesa/vissr/tree/master/protobuf).
The server supports this only for the Websocket protocol, where the websocket transport manager encodes payloads before sending them,
and decodes payloads directly after receiving them. The client follows the same encoding/decoding behavior,
so that the use of protobuf encoding is abstracted before other layers in both the server and the client get access to the payloads.
Two levels of protobuf encoding are available, level one in which paths and timestamps have the standardized text format,
and level two where these fields are compressed.

#### Level 2
Level 2 compresses the VSS paths by using the [VSS path list](/vissr/server#pathlist-file-generation) as a lookup table.
Instead of using the string paths in the encoded payload the index into the array is used.
Finding the index in the array for a given path is done by applying a binary search, as the array paths are sorted by the server.
Going the other way, the array is simply indexed by the integer value from the protobuf encoded payload.
The string based timestamps are replaced by an int32 as shown in the CompressTS() procedure found in [computils.go](https://github.com/covesa/vissr/blob/master/utils/computils.go).
Level 2 achieves compression rates of around 5 or better.

## Java script clients
There are a few clients that are written in Javascript, and thus when started opens in a browser.
Thes clients can be quite handy for quick testing of the server functionality.
Example payloads that can be used as input are found in the [appclient_commands.txt](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/appclient_commands.txt) file.

### HTTP client
The [HTTP Client](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/httpclient.html)
requires the server IP address/URL to be written into the field for it, and the IP address button to be pushed.
Thereafter paths can be written into the Get field, followed by a push of the Get button.
In the case of the client writing a value to the server, the path is written into the Set field, with the value in the following field, before pushing the Post button.
If the data associated with the path is access controlled, then the access token that must have been obtained via the dialogues with the two autorization servers
must first be written into the field for the token. Klicking the button to the right of it preserves the token for use in multple requests.

### Websocket client
The [Websocket Client](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/wsclient_uncompressed.html)
requires the server IP address/URL to be written into the field for it, and the IP address button to be pushed.
Thereafter JSON based payloads can be written into the Sed field, followed by a push of the Send button.

### Websocket client (using compression)
The [Websocket Client](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/wsclient_compressed.html)
requires the server IP address/URL to be written into the field for it, and the IP address button to be pushed.
Thereafter JSON based payloads can be written into the Sed field, followed by a push of the Send button.
The difference to the uncompressed Websocket client is that this client opens a Websocket session with the erver in which it requests a session in which
proprietary compression is applied to the payloads (see chapter above).

### Access Grant Token Server client
The [AGT client](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/agtclient.html)
requests an Access Grant Token from the Access Grant Token server.

It requires the AGT server IP address/URL to be written into the field for it, and the Server IP button to be pushed.
In the leftmost field below "agtserver" (no quotes) must be written, then in the rightmost field a request payload shall be written.
A payload example can be found in the [appclient_commands.txt](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/appclient_commands.txt) file.
The proof value must be "ABC" for a positive validation, in which case an Access Grant token is returned.

### Access Token Server client
The [AT client](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/atclient.html)
requests an Access Token from the Access Token server.

It requires the AT server IP address/URL to be written into the field for it, and the Server IP button to be pushed.
In the leftmost field below "atserver" (no quotes) must be written, then in the rightmost field a request payload shall be written.
A payload example can be found in the [appclient_commands.txt](https://github.com/covesa/vissr/blob/master/client/client-1.0/Javascript/appclient_commands.txt) file.
The token that is provided in the request must include an Access Grant token from the response of a successful reuquest to the AGT server.

## Android app client
The client/android/covesa-vissr-app-demo directory contains an Android app that realizes a VISSv2.0 client.
This was demoed at the COVESA spring 2024 AMM in Gothenburg.
[Presentation](https://wiki.covesa.global/download/attachments/98271360/Empowering%20digital%20services%20-%20COVESA%20AMM%20Gbg%202024.pdf?version=1&modificationDate=1714401997052&api=v2),
[Demo recording](https://wiki.covesa.global/download/attachments/98271360/Empowering%20digital%20services%20-%20COVESA%20AMM%20Gbg%202024_Demo.mp4?version=1&modificationDate=1714634868645&api=v2)

## Clients on other repos

### VISS Web Client
The [VISS web client](https://github.com/nicslabdev/viss-web-client) exposes a sophisticated UI that includes support for the dialogues that a client needs
to have with the authorization server in scenarios where access control is required.

### CCS client
The [CCS client](https://github.com/COVESA/ccs-components/blob/master/ovds/client/ccs-client.go) uses either HTTP (for Get requests), or Websocket (for subscribe requests)
to access data according to a list of paths in a config file, and then requesting an OVDS server to write this data into the OVDS database.

### CCS MQTT client
The [CCS MQTT client](https://github.com/COVESA/ccs-components/blob/master/ovds/client/mqtt-client/mqtt_client.go) uses the VISSv2 MQTT based protocol to subscribe to
data according to a list of paths in a config file, and then requesting an OVDS server to write this data into the OVDS database.
