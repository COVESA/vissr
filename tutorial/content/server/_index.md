---
title: "VISSR Server"
---

The VISSR server is the Sw component that implements the interface that is exposed to the clients, and that must conform to the COVESA VISSv2 specification.

## Build the server
Please check the chapter [VISSv2 Build System](/vissr/build-system) for general Golang information.

To build the server, open a erminal and go to the vissr/server/vissv2 directory and issue the command:

$ go build

## Configure the server

#### VSS tree configuration
The server has a copy of the VSS tree that it uses to verify that client requsts are valid -
that there is a node in the tree that corresponds to the path in a request, if a node requires an access control token and consent permission, etc.
The tree parser that is used expects the tree to have the 'binary format' that the binary exporter of the VSS-Tools generates from the vspec files.
More information about tree configuration [here](/vissr/server/binary-tree-config/).

## Command line configuration
Starting the server with the command line option -h will show the screen below.
```
usage: print [-h|--help] [--logfile] [--loglevel
             (trace|debug|info|warn|error|fatal|panic)] [-d|--dpop]
             [-p|--pathlist] [--pListPath "<value>"] [-s|--statestorage
             (sqlite|redis|memcache|apache-iotdb|none)] [-j|--history]
             [--dbfile "<value>"] [-c|--consentsupport]

             VISS v3.0 Server

Arguments:

  -h  --help            Print help information
      --logfile         outputs to logfile in ./logs folder
      --loglevel        changes log output level. Default: info
  -d  --dpop            Populate tree defaults. Default: false
  -p  --pathlist        Generate pathlist file. Default: false
      --pListPath       path to pathlist file. Default: ../
  -s  --statestorage    Statestorage must be either sqlite, redis, memcache,
                        apache-iotdb, or none. Default: redis
  -j  --history         Support for historic data requests. Default: false
      --dbfile          statestorage database filename. Default:
                        serviceMgr/statestorage.db
  -c  --consentsupport  try to connect to ECF. Default: false
```
More information for some of the options:
* -p: Whether pathlist files should be generated or not at startup. True if is set, false if not set.
* --pListPath 'path: Path to where "pathlistX.json" file(s) are stored. X=[1..] Default is "../".
* -d: Whether default values defined in the tree(s) should be copied to the data store or not at startup. True if is set, false if not set.
* --loglevel levelx: Levelx is one of [trace, debug, info, warn, error, fatal, panic]. Default is "info".
* --logfile: Whether logging should end up in standard output (false) or in a log file (true). True if is set, false if not set.
* --dbfile filepath: Only relevant if SQLite is configured via "-s sqlite".
* -j: Starts up an internal server thread if true. Currently not supported even if set to true. True if is set, false if not set.
* -c: If set to true an ECF SwC must be available to connect to the server. True if is set, false if not set.

## Data storage configuration
Currently the server supports two different databases, SQLite and Redis, which one to use is selected in the command line configuration.
However, to get it up and running there might be other preparations also needed, please see the [VISSv2 Data Storage](/vissr/datastore) chapter.

## Protocol support configuration

The server supports the following protocols:
* HTTP
* Websockets
* MQTT (with the VISSv2 specific application protocol on top)
* gRPC

The message payload is identical for all protocols at the client application level (after protocol specific payload modifications are restored).
HTTP differs in that it does not support subscribe requests.

The code is structured to make it reasonably easy to remove any of the protocols if that is desired for reducing the code footprint.
Similarly it should be reasonably straight forward to add new protocols, given that the payload format transformation is not too complicated.

The Websocket protocol manager terminates subscriptions if a client terminates the session without first terminating its ongoing subscriptions.

Each of the transport protocol managers runs on a separate thread.
If a transport protocol is of no interest to have listening for clients then it can be prevented from starting by commenting out
the string element with its name in the serverComponents string array variable in the vissv2server.go file before compiling it.

### TLS configuration
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

### Feeder interface
As described in the [VISSR Feeders](/vissr/feeder/) chapter there are two template versions of feeders.
Version 2 only supports reception of Set requests from the server, while version 3 can also act on subscribe/unsubscribe requests from the server,
and then issue an event to the server when it has updated a subscribed signal in the data store.
The figure below shows the communication network that implements this.
![Network for feeder event communication](/vissr/images/feeder-event-comm.jpg?width=50pc)
* Figure 1. Network for feeder event communication

The feeder, running in a separate process, is communicating with the server over a Unix domain socket interface.
This interface is on the server side managed by the "feeder front end" thread.
The "service manager" thread of the server receives set/subscribe/unsubscribe requests from clients (get requests do not affect this network)
that it passes on to the feeder front end, which then analyzes the requests and decides to which other entities this should be forwarded.
The subscribe request types that benefit from switching from polling to an event based paradigm are change, range, curvelog, and historic data capture.
This solution supports events for the change, range, and curvelog type.
The historic data capture may later also be updated to support this.
The message formats for the messages passed over the UDS interface are shown below.
For the message formats over the other Golang channel based interfaces, please read the code.

Feeder front end to Feeder:
* {”action”: ”set”, "data": {"path":"x", "dp":{"value":"y", "ts":"z"}}}
* {”action”: ”subscribe”, ”path”: [”p1”, ..., ”pN”]}
* {”action”: ”unsubscribe”, ”path”: [”p1”, ..., ”pN”]}

Feeder to Feeder front end:
* {”action”: ”subscribe”, ”status”: “ok/nok”}
* {”action”: ”subscription”, ”path”: ”p”}

A feeder implementing version 2 may discard messages from the Feeder front end that have the "action" set to either "subcribe" or "unsubscribe",
while a feeder implementing version 3 must respond to a subscribe request with "status" set to "ok".

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
