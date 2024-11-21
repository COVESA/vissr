**(C) 2021 Geotab Inc**<br>

# Compression client

The compression client is a client for testing the experimental compression solutions that are supported by the VISSv2 server. 
If the client is started without any command argument the following is shown:<br>
<pre>[-v|--vissv2Url] is required
usage: print [-h|--help] -v|--vissv2Url &quot;&lt;value&gt;&quot; [-p|--protocol (http|ws)]
             [-c|--compression (prop|pbl1|pbl2)] [--logfile] [--loglevel
             (trace|debug|info|warn|error|fatal|panic)]

             Prints provided string to stdout

Arguments:

  -h  --help         Print help information
  -v  --vissv2Url    IP/url to VISSv2 server
  -p  --protocol     Protocol must be either http or websocket. Default: ws
  -c  --compression  Compression must be either none or protobuf. Default: proto
      --logfile      outputs to logfile in ./logs folder
      --loglevel     changes log output level. Default: info
</pre>
Depending on the -c argument the client will run in one of the three compression variants supported (see README in utils directory), 
and it will set the websocket subprotocol accordingly to signal to the server which variant to use.<br>

The file requests.json contains the requests that the client have available for sending to the server. This file can be updated with additional requests.<br>
The client presents the requests on the terminal window with an integer index in front. Enter the index for the request to be issued, and the client will then present the JSON payload sizes, the compressed sizes, and the compression ratio=(len(json)*100)/len(compressed) for the request and response.<br>
If a subscribe request is issued, the client will continue to show the received notification information as above, until it receives a RETURN key user input, which will lead to that it issues an unsubscribe request, and then waits for new user input.<br>
The user input zero (0) terminates the client.<br>
Protobuf level 2 compression requres that the file vsspathlist.json is present in the startup directory.
It is created by the VISSv2 server at startup, and can be found in the ./server directory.
