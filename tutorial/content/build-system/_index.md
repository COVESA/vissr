---
title: "VISSR Build System"
---

## Installing Golang

Most of the code at this repository is written in Golang, so in order to use this repo Golang must be installed on the computer.

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

### Transport protocols
Besides the transport protocols
* HTTP
* Websocke
* MQTT
that the specification lists, this server also supports
* gRPC
They are all except MQTT activated by the server per default.
To enable MQTT, or disable any other, the string array serverComponents in the vissv2server.go file contains a list of the components that are spawned on
separate threads by the main server process, and these can be commented in or out before building the server.
For the server to be operationable, the service manager, and at least one transport protocol must not be commented out.

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


