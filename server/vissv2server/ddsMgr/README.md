# VISS over DDS

Transport manager that exposes the VISS API over DDS (Data Distribution Service).

## Protocol

The wire envelope mirrors VISS over MQTT so that client code only needs to
swap the transport library:

```
Request  topic : /<VIN>/Vehicle
Payload        : {"replyTopic":"<unique>","request":{...VISS JSON...}}

Response topic : <unique>
Payload        : {...VISS JSON response...}
```

The vehicle subscribes to `/<VIN>/Vehicle`. Clients subscribe to their unique
`replyTopic`, then publish a request to `/<VIN>/Vehicle`. The server core
serves the request and publishes the response to the `replyTopic`.

## Starting the server with DDS

```bash
./vissv2server --ddsenable
```

Set `DDS_VIN` to bypass the automatic VIN lookup (useful in development):

```bash
DDS_VIN=VIN001 ./vissv2server --ddsenable
```

## DDS implementation

The package uses [go-DDS](https://github.com/SoundMatt/go-DDS) for the
transport layer. The default build uses the in-process mock (no system
library required). Switch to real CycloneDDS:

```bash
apt-get install -y libcyclonedds-dev
go build -tags cyclone ./...
```

## DDS domain

All VISS DDS traffic runs on **domain 0** by default. Change `ddsDomain` in
`ddsMgr.go` if your deployment uses a different domain.
