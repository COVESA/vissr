---
title: "VISSv2 Server"
---

The VISSv2 server is the Sw component that implements the interface that is exposed to the clients, and that must conform to the COVESA VISSv2 specification.

### Build the server
Please check the chapter [VISSv2 Build System](/vissr/build-system) for general Golang information.

To build the server, open a erminal and go to the vissr/server/vissv2 directory and issue the command:

$ go build

### Configure the server

#### VSS tree configuration
The server has a copy of the VSS tree that it uses to verify that client requsts are valid -
that there is a node in the tree that corresponds to the path in a request, if a node requires an access control token, etc.
The tree parser that is used expects the tre to have the 'binary format' that one of the VSS-Tools genertes from the vspec files.
To generate this the [VSS repo](https://github.com/COVESA/vehicle_signal_specification) must be cloned including the VSS-Tools submodule,
and a file containing the binary representation must be created, which is done with the following command issued in the root directory.

$ make binary

This generates a file with a name like 'vss_rel_4.1-dev.binary',
which then needs to be renamed to 'vss_vissv2.binary' and stored in the vissr/server/vissv2server directory.

If you want to configure the tree to include access control, access control tags as described in the
[VISSv2 - Access Control Selection chapter](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#access-control-selection) needs to be added to appropriate tree nodes.
This can either be done by editing vspec files directly, or using the [VSS-Tools](https://github.com/covesa/vss-tools) overlay mechanism.

#### Command line configuration
The server has the following command line configurations:
* Data storage implementation. Select either to use an SQLite implementation (-s sqlite) or a Redis implementation (-s redis). Default is SQLite.
* Data storage file name (--dbfile 'file-name'). Only relevant for SQLite configuration. Default is "serviceMgr/statestorage.db".
* Request the server to generate a pathlist file, then terminate (--dryrun). Default is not to terminate after generating it.
* Pathlist file name (--vssjson 'file-name'. Default is "../vsspathlist.json".
* UDS path for history control (--uds 'file-name'). Name of the Unix domain socket file. Default is "/var/tmp/vissv2/histctrlserver.sock".
* Level of logging (--loglevel levelx). Levelx is one of [trace, debug, info, warn, error, fatal, panic]. Default is "info".
* Whether logging should end up in standard output (false) or in a log file (true) (--logfile false/true). The default is 'false'.

#### Data storage configuration
Currently the server supports two different databases, SQLite and Redis, which one to use is selected in the command line configuration.
However, to get it up and running there are other preparations lso needed, please see the [VISSv2 Data Storage](/vissr/datastore) chapter.

#### Protocol support configuration

The server supports the following protocols:
* HTTP
* Websockets
* MQTT (with the VISSv2 specific application protocol on top)
* gRPC

The message payload is identical for all protocols at the client application level (after protocol specific payload modifications are restored).
HTTP differs in that it does not support subscribe requests.

The code is structured to make it reasonably easy to remove any of the protocols if that is desired for reducing the code footprint.
Similarly it should be reasonably straight forward to add new protocols, given that the payload format transformation is not too complicated.

The Websocket protocol manager terminates subscriptions if a client terminats the session without first terminating its ongoing subscriptions.

##### TLS configuration
The server, and several of the clients, can be configured to apply TLS to the protocols (MQTT uses it integrated model for this).
The first step in applying TLS is to generate the credentials needed, which is done by running the testCredGen.sh script found [here](https://github.com/covesa/vissr/tree/master/testCredGen/).

For details about it, please look at the README in that directory.
As described there, the generated credentials must then be copied into the appropriate directories for both the server and the client.
And the key-value "transportSec" in the transportSec.json file must be set to "yes" on both sides.

Reverting to non-TLS use only requires the "yes" to be changed to "no",
on both the server and the client side.
Clients must also change to the non-TLS port number according to the list below.
| Protocol  | Port number: No TLS | Port number: TLS |
|-----------|---------|---------|
| HTTP      |   8888  |   443   |
| WebSocket |   8080  |   6443  |
| MQTT      |   1883  |   8883  |
| gRPC      |   5000  |   5443  |

### Pathlist file generation
Some software components that are used in the overall context to setup and run a VISSv2 based communication tech stack needs a list of all the leaf node paths of the VSS tree being used y the server.
The server generates such a list at startup, in the form of a sorted list in JSON format, having a default name "vsspathlist.json".
As this file may need to be copied and used in other preparations before starting the entire tech stack, it is possible to run the server to only generate this file and then terminate.
SwCs that use this file:
* SQLite state storage manager.
* The server itself if started to apply path encoding using some of the experimental compression schemes, and the corresponding client.
* The protobuf encoding scheme.
* The live simulator.

### History control
The VISSv2 specification provides a capability for clients to issue a request for [historic data](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#history-filter-operation).
This server supports temporary recording of data that can then be requested by a client using a history filter.
The model used in the implementation of this is that it is not the server that decides when to start or stop a recording, or how long to keep the recorded data,
but it is controlled by some other vehicle system via a Unix domain socket based API.
For more information, please see the [service manager](https://github.com/covesa/vissr/tree/master/server/vissv2server/serviceMgr) README.

To test this functionality there is a rudimentary [history control client](https://github.com/covesa/vissr/blob/master/server/hist_ctrl_client.go)
that can be used to instruct the server to start/stop/delete recording of signals.
To reduce the amount of data that is recorded the server only saves a data value if it has changed compared to the latest captured,
so to record more than a start and stop value the signals should be dynamic during a test.

### Ignition life cycle
Dynamic data handled by the server, such as subscriptions, and access token caching, does not survive between ignition cycles (restart of the server).

### Experimental compression
VISSv2 uses JSON as the payload format, and as JSON is a textbased format there is a potential to reduce the payload size by using compression.

A first attempt on applying compression built on a proprietary algorithm that took advantage of knowing the VISSv2 payload format.
This yielded compressions rates around 5 times (500%), but due to its strong dependence on the payload format it was hard to keep stable when the payload format evolved.
The [compression client](/vissr/client#compression-client) can be used to test it out, but some payoads will likely crash it.

A later compression solution was built on protobuf, using the VISSv2messages.proto file found [here](https://github.com/covesa/vissr/tree/master/protobuf).
For more details, see the  [compression client](/vissr/client#compression-client).

The gRPC protocol implementation, which requires that payloads are protobuf encoded, uses the VISSv2.proto file found [here](https://github.com/covesa/vissr/tree/master/grpc_pb).
