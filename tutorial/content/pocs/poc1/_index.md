---
title: "Vehicle.Speed subscription example"
---

Here follows a minimal example that goes through all the steps nneeded to set up a VISSv2 tech stack
where a client subscribes to Vehicle.Speed, and receives simulated values in return.

It is assumed that the development environment has Golang installed.
If not, then instruction for installing Golang is found [here](/vissr/build-system/).

If this repo is not cloned to the development environment, this is done by the command:

$ git clone https://github.com/covesa/vissr.git

The POC will be based on the feeder found at the feeder branch, so switching to this branch is done by the command:

$ git checkout feeder

As we will use the Redis based statestorage, we will have to start the Redis daemon.
A code snippet that does this is available [here](https://github.com/COVESA/ccs-components/blob/master/statestorage/redisImpl/redisInit.go).
If you do not already have cloned this repo, then it is done by the command:

$ git clone https://github.com/COVESA/ccs-components

Check out the README [here](https://github.com/COVESA/ccs-components/tree/master/statestorage/redisImpl) for details on how to set this up.


Next is to build the server, which is done in a terminal window by the command (with the working directory moved to the WAII/server/vissv2server):

$ go build

The server uses a binary formatted copy of the VSS tree to verify client requests, so if the signal of interest, Vehicle.Speed,
is not in this tree, i is necessary to create a new binary tree containing this signal.
For informtion on how to get that done, check out [VSS tree configuration](/vissr/server#vss-tree-configuration).

Then start the server with the command line configuration for using Redis as statestorage:

$ ./vissv2server -s redis

To build the feeder, open a new terminal and move the working directory to WAII/feeder and then apply the command:

$ go build

Before strtign it, it needs to be configured for mapping of the Vehicle.Speed signal, and to generate simulated values for it.
To do this the file [VehicleVssMapData.json](https://github.com/covesa/vissr/blob/feeder/feeder/VehicleVssMapData.json) needs to be edited
so that it only contains a mapping for Vehicle.Speed.

[{"vssdata":"Vehicle.Speed","vehicledata":"CurrSpd"}]

Replace the content of the file with the above. The name 'CurrSdp' is the (imaginary) name of the speed signal used in the vehicle interface.
After the feeder is configured it is started:

$ ./feeder

What is left now is to start a client and issue the subscribe request.
One solution to this is to write a client, but a quicker solution is to use any of the [existing clients](/vissr/client).

We will here use the [Javascript based client that uses the Websocket protocol](https://github.com/covesa/vissr/blob/feeder/client/client-1.0/Javascript/wsclient_uncompressed.html).

Start it by navigating to the directory using a file browser, then just click on it.

As the first field to populate is the field requesting the Ip address / URL of the server,
it is necessary to find this for the computer it runs on. This can on Ubuntu be done with the command:

$ ip addr show

then search for an IP address shown after the word "inet".

Copy the address into the field and push the Server IP button.

The client should then be connected to the server, which is verified b a printout in this browser tab saying "Connected".
If that is not shown, either the server is not up and running, or the IP address is not the corrt one.

Assuming it got connected, the only thing left is to issue a subscribe request.
The [appclient_commands.txt](https://github.com/covesa/vissr/blob/feeder/client/client-1.0/Javascript/appclient_commands.txt) contains many examples of client requests

From this file, copy the request payload:

{"action":"subscribe","path":"Vehicle/Cabin/Door/Row1/Right/IsOpen","filter":{"type":"timebased","parameter":{"period":"3000"}},"requestId":"246"}

And then edit the path to become "path":"Vehicle.Speed", copy this updated payload, paste it into the payload field, and push the Send button.

After about three seconds an event message should be received from the server and printed into the browser tab, looking something like:
{
        "action": "subscription",
        "subscriptionId": "1",
        “data”: {“path”: ”Vehicle.Speed”, “dp”: {“value”: ”50.0”, “ts”: ”2023-04-15T13:37:00Z”}},
        "ts": "2023-04-15T13:37:00Z"
}
This should every three seconds be followed by a new event message. If the feeder was configured to update this signal with simulated values,
the value shown should vary accordingly, else it will be the same in every event message.
If there has not been any value written into the Redis statestorage for this signal, then the value will be "Data-not-available".

The server will continue to send these event messages every third second until it receives an unsubscribe request containing the subscriptionId it associated to the subscription.
To send an unsubscribe request, search for it in the appclient_commands.txt file, check that the subscriptionId is correct, paste it into the payload field and push the Send button.
The event message printouts should then stop, and the POC is successfully ended.
