# Project: VISSR: server/server-1.0

Functionality: <br>
	Long term: Server shall support all features specified in the W3C VISSv2 standard.<br>
	Short term limitations: <br>
		- Max twenty parallel app-clients for the Websocket protocol. <br>
		- The access control solution does not include a proper authentication process. <br>
		- Responses for error cases may not follow VISSv2 in all cases.<br>
		- The service manager only persistently updates values for set if a state storage DB exists in its directory.<br>

Implementation language: Go for server, JS for app-clients.


## Build instructions:
The most convenient way to run the server is to use the script file W3CServer.sh, which can facilitate both starting and stopping all executables that together realize the server.
To start:
$ ./W3CServer.sh startme
To stop:
$ ./W3CServer.sh stopme

To build manually, have look in *Dockerfile.rlserver* in the project root. The server runs in one single process. If access
control is to be used the access grant token server must be running. The access grant token server is a separate process,
see *Dockerfile.agtserver* for build instructions. 

At startup the VISSv2 server reads the vss_vissv2.binary file, which contains the VSS tree in binary format. 
It then generates the file vsspathlist.json in the server parent directory. 
Binary files containing the latest VSS tree on the VSS repo can be generated after cloning the VSS repo, and then issuing the 'make binary' command.

After starting the server, one or more clients can be started. There are basic Javascript based clients available for both HTTP and Websocket communication with the server in the webclients directory. These clients can either be run from the same machine as the server is running on, or from a different machine, provided the machines can connect over TCP/IP.
To run a client, just open the HTML-file in a browser. Then first input the IP address to the server, and after that requests can be sent to the server, and responses will be displayed.
Example requests can be found in the file appclient_commands.txt, which can be copied into the client UI. The server, which must have access to a copy of the complete VSS tree from the VSS repository, will accept example requests that are modified for accessing any path within this tree. 
There are also clients implemented in golang available, where some are found at this Github project: https://github.com/COVESA/ccs-components

## Software implementation
Figures 1 and 2 shows the design of the core server and the Websocket transport manager, respectively. The design is based on the high level Sw Architecture description found in the README of the root directory.<br>
The drawings to the left in the two figures show a high level view where cases of possible multiple instances of components are shown, while the drawings to the right show a more detailed view, but where for simplicity only a single instance of components are shown.<br>
The core server is partitioned in the following logical components:<br>
- Core server hub - the manager, tying it all together,<br>
- The transport manager registration server, managing the registration of transport protocol managers over HTTP,<br>
- Transport data channel server, exist in multiple instances, one for each registered transport manager, managing the data communiction,<br>
- The service manager registration server, managing the registration of service managers over HTTP,<br>
- Service data channel client, exist in multiple instances, one for each registered service manager, managing the data communiction,<br>
- The tree manager, providing access to the tree, abstracting the actual format of the tree, and more complex operations such as tree search, tree initiation and termination.<br>
The core server hub, running in the main context, spawns the following Go routines:<br>
- The transport manager registration server.<br>
- Transport data channel servers, each having separate frontend and a backend go routine.<br>
- The service manager registration server.<br>
- Service data channel servers, each having separate frontend and a backend go routines.<br>
The Go routines communicate in between using Go channels.<br>
The communication with the transport protocol and service managers is realized using the Websocket protocol.<br>
![Core server design](../pics/Core_server_SwA.jpg)<br>
* Fig. 1 Core server design<br><br>
The Websocket transport protocol manager is partitioned in the following logical components:<br>
- The Websocket manager hub, responsible for registration with the core server, spawning of Websocket servers for connecting app-clients, and routing of messages to/from app-clients, etc.,<br>
- The Websocket server,  exist in multiple instances, one for each app-client that connects to it.<br>
![Transport manager design](../pics/WS_manager_SwA.jpg)<br>
* Fig. 2 Websocket transport manager design<br><br>
The Websocket hub and WS servers run in separate Go routines, each having separate frontend and a backend go routine, and communicate with each other via Go channels.<br>
The data communication with the core server uses the Websocket protocol, as well as its communication with the app-clients.<br>
The HTTP manager has the same architecture as the WS manager. It converts the request data from the HTTP call into the Websocket format before sending it to the core server, and it converts the Websocket response from the core server into the HTTP response before sending it back to the app-client.<br>
The HTTP manager supports the same functional set of requests as the Websocket manager, except for subscription.<br>

## History control client
The VISS version 2 specification supports that a client may request "historic" data, i. e. data that for some reason has been recorded by the server. What data to record ,and when is controlled by the vehicle system ,using the history control interface. The "hist_ctrl_client.go" is a client implementation using this interface. For more info, see the README in the service manager directory.

