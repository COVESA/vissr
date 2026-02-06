---
title: "VISSR Build System"
---

For those who prefer to directly dive into running code instead of reading a lot of documentation before geting to run it
there is a [Hello World example](/vissr/build-system/hello-world) to start with.

## Installing Golang

The server and most of the other software components at this repository is written in Golang, so in order to use this repo Golang must be installed on the computer.

Searching for "install golang" gives many hits, of which this is one:

[How to install Go (golang) on Ubuntu Linux](https://www.cyberciti.biz/faq/how-to-install-gol-ang-on-ubuntu-linux/).

For other operating systems [this](https://go.dev/doc/install) may be helpful.

This project requires Go version 1.13 or above, make sure your GOROOT and GOPATH are correctly configured.
Since this project uses Go modules all dependencies will automatically download when building the project the first time.

## Building and running

As several of the Golang based Sw components on this repo can be started with command line input to configure its behavior,
it is suitable to first build it (in the directory of the source code)

$ go build

If the image is to be run on another platform, golang has ample cross-compilation capabilities, more can be learned [here](https://opensource.com/article/21/1/go-cross-compiling). 
To cross-compile, the command could look like the below.

env GOOS=linux GOARCH=arm64 go build -o vissv2server

To run it the command looks like:

$ ./'name-of-executable' 'any-command-line-config-input'

If the SwC supports command line configuration input it is possible to add "-h" (no quotes) on the command line, which will then show a help text.
Checking the first lines of the main() function in the source code is another possibility to find out.
If there is any calls to the "github.com/akamensky/argparse" lib then it is used.

As the configurations have a default it is always possible to run it without adding any comand line configuration input.
The configuration possibilities of the different SwCs are described in the respective chapters of this tutorial.

The server consists of several "actors", see the [README](https://github.com/covesa/vissr) Overview chapter.
These used to be built as separate processes that communicated over the Websockets protocol.
To simplify the building process of thesesoftware components the script W3CServer.sh was created.
After the refactoring of these SwCs into one process with ech actor running as a separate thread,
it became more convenient to build without this script, but it is still [avaliable](https://github.com/covesa/vissr/blob/master/W3CServer.sh).
For more details, see the "Multi-process vs single-process server implementation" chapter in the README.

There are multiple Software components on this repo, such as feeders, simulators, the DCT tool that are to be built as separate excutables.
If it is forgotten to be mentiond in the README, one way of determining whether a separate build is needed or not is to check the package statement in the source code.
If it says "package main" it is a separate executable and shall then be built and run as described above.

### Logging
Logging can be command line configured at startup.
* logging level can be set to either of [trace, debug, info, warn, error, fatal, panic].
* logging output destination. It can either be written to file, or directed to standard output.

The levels currently used are mainly info, warning, error. Info is appropriate during testing and debugging, while error is appropriate when performance is important.

### Testing
If modifications of the server code base is done it is recommended that the following test routine is performed before pushing
any commits to this repo.
To test the "VISSR tech stack", the server together with data store and feeder, the runtest.sh is available in the root directory.
This bash script starts the server and the feedervx (x is the latest of existing feeder templates) in the background,
and then the testClient is started in the terminal window.
The testClient reads the testRequests.json file for the requests it will issue to the different transport protocols,
and then it prints the requests and the responses in the terminal window.
After each set of test requests for the same transport protocol it will wait until the keyboard return key is received,
allowing the operator to gett time to inspect the request/response logs before continuing.
When the MQTT protocol is run, the test.mosquitto.org broker is used which may lead to latencies between request and response.
In that case the testClient prompt for reading the return key may be found mixed into the log prints,
but hitting the return key will still make it continue.
If some logdata looks incorrect the operator can hit ctrl-C to terminate the testClient
and instead inspect the log files for the server and the feeder, found in the log directory of their respective start directories.
It is up to the operator to evaluate whether the logs look correct or not,
so it might be a good idea to run the script before doing any modifications lo learn how correct logs looks like.
All subscribe sessions are automatically unsubscribed after 4 events have been received.
This number is set in runTest.sh by the CLI parameter "-m 4". Other numbers of event can be set by editing this number.

Issue the following command in the root directory to run the tests:
```
$ ./runtest.sh startme
```
When the testing is completed the script should be run to terminate the server and feeder processes:
```
$ ./runtest.sh stopme
```
The file testRequests.json can be edited to add or remove test cases.
It is up to the operator to ensure that subscribe trigger conditions will be met by the simulated data that the feeder injects.
This data can be modified in the tripdata.json file in the feederv3 directory.

To get the VISSR stack running to enable testing using e. g. a client under development,
or any of the clients found in the [client directories](https://github.com/COVESA/vissr/tree/master/client),
there is the runstack.sh script that builds and starts the server and the feederv3, with the feeder generating simulated input.

### Transport protocols
The following transport protocols are supported
* HTTP
* Websocket
* MQTT
* gRPC
* Unix domain sockets

They are all except MQTT enabled by the server per default.

To enable MQTT add the CLI command "-m" when starting the VISS server.

To disable any other protocol, the string array serverComponents in the vissv2server.go file contains a list of the components that are spawned on
separate threads by the main server process, and these can be commented out before building the server.
For the server to be operationable, the service manager, and at least one transport protocol must not be commented out.

### Git tags and releases
The project has finally started to create releases with release v1.0 beig the first.
A few more tags has also been created but not publicized as releases.
These can be found on the [View all tags](https://github.com/COVESA/vissr/tags) page.
On the page for each tags there is a short description of it and asset files with the source code of the commit the tag is pointing to.
Reasons for these tags are:
* The last commit for a VISS specification version, before features related to the next version begins to appear.
* The last commit where the VISSR server supports the communication protocol with a specific feeder template version.
The versions of this protocol is not backwards compatible in most cases.

### Go modules
Go modules are used in multiple places in this project, below follows some commands that may be helpful in managing  this.

```bash
$ go mod tidy
```
To update the dependencies to latest run
```bash
$ go get -u ./...
```

If working with a fix or investigating something in a dependency, you can have a local fork by adding a replace directive in the go.mod file, see below examples. 

```
replace example.com/some/dependency => example.com/some/dependency v1.2.3 
replace example.com/original/import/path => /your/forked/import/path
replace example.com/project/foo => ../foo
```
For more information see https://github.com/golang/go/wiki/Modules#when-should-i-use-the-replace-directive

## Docker

The server can also be built and launched using docker and docker-compose, see the [Docker README](https://github.com/covesa/vissr/tree/master/docker).
Current example builds and runs using the redis state storage together with an implementation of the feeder interfacing 
the remotiveLabs broker in the cloud.[feeder-rl](https://github.com/covesa/vissr/tree/master/feeder/feeder-rl) .


