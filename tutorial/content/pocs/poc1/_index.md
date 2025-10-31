---
title: "Test run of the different VISSR supported protocols"
---

Here follows a minimal example that goes through all the steps needed to set up and run a few test requests on the VISSR tech stack.

It is assumed that the development environment has Golang installed.
If not, then instruction for installing Golang is found [here](/vissr/build-system/).

If this repo is not cloned to the development environment, this is done by the command:

$ git clone https://github.com/covesa/vissr.git

The shell script runtest.sh that is found in the VISSR root directory will build the server, the feederv3 and the testClient,
and then start them.

The test client will then issue a few commands to the server over each of the transport protocols HTTP, Websocket, gRPC, and MQTT, respectively.
The issued requests and the received responses are shown in the terminal window.
After each set of requests over one of the transport protocols the UI asks for the Return key to be pushed before next transport protocol is tested.

After building these from scratch the first time the script ometimes fails to start the feeder so if the responses all return error messages,
running the script again should fix it.

To start the script, issue the command:

$ ./runtest.sh startme

The server and feeder processes are not automatically terminated after the test, to trminate them issue the command:

$ ./runtest.sh stopme
