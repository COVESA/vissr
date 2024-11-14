---
title: "VISSR Hello World example"
---

Two Hello World alternatives are available:
* [Native build](/vissr/build-system/hello-world/#native-build-based-hello-world-example) based example
* [Docker](/vissr/build-system/hello-world/#docker-based-hello-world-example) based example

## Native build based Hello World example

Building the server and many other software components require that the Golang build system is installed on the computer,
please see [here](/vissr/build-system/#installing-golang) how to get that done.
The next step is to clone the [VISSR](https://github.com/COVESA/vissr) repo.
```
$ git clone https://github.com/COVESA/vissr.git
```
This is followed by going to the directory where the main server code is located, and build the server.
```
$ cd vissr/server/vissv2server/
$ go build
```
This could be followed by directly starting a client to issue requests to the server,
but the server would then try to read data from an empty data store, so all responses would say 'Data-not-found'.
To be able to get some real data back we will therefore start a feeder that will write simulated data into the data store.

To do this, open a new terminal window, go to the feederv3 directory, and build the feeder.
```
$ cd vissr/feeder/feeder-template/feederv3/
$ go build
```
Now we can start the server and the feeder in respective terminal window.
The server will connect to the feeder, so to avoid server logs complaining it cannot find the feeder it is preferred to start the feeder first.
```
$ ./feederv3
$ ./vissv2server
```
Both the server and the feeder will by default be configured to use Redis for the data store.
This requires that Redis has been installed on the computer, how to do this can be read [here](https://redis.io/docs/latest/operate/oss_and_stack/install/install-redis/).
Other data stores can be configured, more info can be found [here](/vissr/datastore/).

Now it is time to start a client, there are many clients to be found in the client directory and subdirectories.
We will here use the Javascript based client that connects via the Websocket protocol.
But first we need to find out the IP address of the computer as the client connects to the server over a socket based on this address.
How to do that can e. g. be found [here](https://www.wikihow.com/Check-a-Computer-IP-Address), on Ubuntu the command
```
$ ifconfig
```
can be used. You will then have to search through a lot of information for an address that likely starts with '192.168' followed by two more segments.
Please copy this and use the file browser on the computer to go to the client/client-1.0/Javascript directory.
There you click on the file 'wsclient_uncompressed.html', which leads to that it starts up in the browser.
You will there see a field whch says 'host ip', please paste the IP address in there and click on the button to the right that says 'Server IP'.
The client will then print 'Status: Connected' in the area below if it succeeds to connect.
Now we can start to issue client requests to the server by pasting or writing them into the field where it says 'JSON payload,
followed by clicking on the button 'Send'.
Try with the command
```
{"action":"get","path":"Vehicle.Cabin.Door.Row1.DriverSide.IsOpen","requestId":"232"}
```
which should return a response like the below.
```
Server: {"action":"get","requestId":"232","ts":"2024-11-12T11:26:44.546855082Z", "data":{"path":"Vehicle.Cabin.Door.Row1.DriverSide.IsOpen", "dp":{"value":"Data-not-found", "ts":"2024-11-12T11:26:44.548180993Z"}}}
```
The value is set to 'Data-not-found' which is due to that the federv3 is not instructed to create simulated values for this signal.
At startup the feederv3 reads the file VssVehicle.cvt which has been created by the [Domain Conversion Tool](/vissr/tools/).
This file contains the instructions for how signals are mapped and scaled when they traverse between the 'vehicle domain' and the 'VSS domain',
but the feederv3 also uses this information to select which signals to create simulated values for.
The cvt-file that comes with the repo only contains the five signals that can be read in the tools/DomainConversionTool/Map-VSSv0.1-CANv0.1.yaml file.
So if we want to get a response with a simulated value a client request to any of these signals must be issued, e. g. like below.
An alternative would be to create a new cvt-file with more signals first.
```
{"action":"get","path":"Vehicle.Speed","requestId":"232"}
```
Please issue this request, se the response, wait about 30 secs and issue it again. 
The values returned in the two responses should differ as the feeder randomly generates new values. If not wait another 30 secs and issue it again.
This could more easily be seen if a time-based subscribe request is issued:
```
{"action":"subscribe","path":"Vehicle.Speed","filter":{"variant":"timebased","parameter":{"period":"10000"}},"requestId":"246"}
```
The server will issue event messages every ten seconds, and it can after a number of events has been received be seen that the value is randomly changed.
To unsubscribe, issue the request:
```
{"action":"unsubscribe","subscriptionId":"1","requestId":"240"}
```
The file client/client-1.0/Javascript/appclient_commands.txt contains examples of different requests that can be used, either as is or modified.

The feederv3 can at startup be configured to read simulated tripdata from files like 'tripdata.json' or 'speed-sawtooth.json',
the latter suitable if the curve log filter subscription is to be tested, maybe with a request like this:
```
{"action":"subscribe","path":"Vehicle.Speed","filter":{"variant":"curvelog","parameter":{"maxerr":"2","bufsize":"18"}},"requestId":"275"}
```
More simulation info can be found [here](/vissr/feeder/#simulated-vehicle-data-sources).
These files can easily be modified and extended with data for more signals and longer trips.

## Docker based Hello World example

The [README](https://github.com/COVESA/cdsp/blob/main/docker/README.md) describes how to use a Docker image to get the VISSR server up and running.
