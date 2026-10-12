**(C) 2026 Ford Motor Company**<br>

# VISSR feeder template version 4

This version supports multiple feeders to be used together with the server / state storage.
The server exposes  a feede registration interface over UDS using the sock file "/var/tmp/vissv2/feederReg.sock".
The feeder reads by default the file "feederConfig.json" at startup and uses the information in it to register with the server.
It is possible to configure a different file to be read by the CLI parameter "-c <filename>".
This can be used when starting multiple feeders.

A feeder registers by issuing the following request:
```
Reg-request:{"action": "reg", "name": "xxxx"}
```
If successful the server rsponds with
```
Reg-response:{"action": "reg"", "name": "xxxx", "sockfile": "/x/y/z.sock"}
```
where the sockfile shall be used by the feeder to set up a UDS server listening to requests from the server.
The communication over this UDS channel is identical to the server-feederv3 communication.

A feeder can also at any point de-register with the server by issung the request:
```
Dereg-request:{"action": "dereg", "name": "xxxx"}
```
which if successful get the following response:
```
Dereg-response:{"action": "dereg", "name": "xxxx"}
```
Non-successful requests gets the following error response from the server:
```
Error-response:{"action": "error"}
```
The server does not hold information on which Set request shall be sent to which feeder that is responsible for forwarding it to the underlying vehicle system.
It therefore sends the request to all registered feeders, and then it is the responsibility of the feeders to evaluate which Set requests it shall act on, or just drop.

The current logic used by the feederv4 supports any tree/branch to be defined as the feeder scope,
but it does not support the exclusion of parts of the tree/branch.
This will be added at some later point.

The runstack.sh bash script is updated to support registration of or or two feeders.
The case wih two feeders is showing a scenario where a truck has Vehicle and Trailer1 trees and a corresponding Vehicle and Trailer1 feeders.

## MongoDB sink
The feeder can additionally store the Set requests it receives from the server in a MongoDB collection.
The sink is disabled by default. Only Set requests that are in the feeder scope are stored.
```
MONGODB_URI=<connection string> ./feederv4 --mongosink [--mongodb <database>] [--mongocoll <collection>]
```
The database and collection names default to "vissr" and "feederSets".

The server delivers a Set request with multiple data points to the feeder as one message per data point.
Data points arriving within 200 ms of each other are treated as the same Set request and are upserted into one document,
with the timestamp of its first data point as _id. The dot-separated path becomes a nested document,
and numeric and boolean values are stored with their native type, also inside array values such as ["118", "95", "75"], e.g. Vehicle.Speed=50 is stored as
```
{"_id": "2026-10-05T23:02:44.611Z", "ts": "2026-10-05T23:02:44.611Z", "Vehicle": {"Speed": 50}, "updatedAt": <date>}
```
Writes are asynchronous, a slow or unreachable database does not block the feeder. If the write queue is full, data points are dropped and an error is logged.
