---
title: "VISSR Docker"
---

## Intro

This is a guide to setup and run the vissv2 server together with the remotiveLabs broker with recorded vehicle data.
The data set is residing the remotiveLabs cloud - <LINK>. There are some configuration files that needs to be set and 
pushed to the docker images. The docker-compose-rl.yml is depending on the Dockerfile(for building) also located in the 
project root. A docker directory is reserved for adding different docker setups with different data providers.

**1. feeder-rl**

The feeder-rl has two basic commands which lets you switch dataprovider. We have --dataprovider remotive or 
--dataprovider sim. This guide will focus on using --dataprovider remotive.

Visit [remotive labs cloud demo](https://console.demo.remotivelabs.com/p/demo/recordings)
- Select the ***Turning Torso Drivecycle***, and the vss configuration.
- Select ***Play***, and choose ***select broker***, choose ***My personal broker*** and then ***Upload***.
- Select ***Go to broker***
- In the left down corner select ***Broker details*** and copy the url and the api-key, we need them for the feeder 
configuration
- keep the page open in the browser  (we will not press start yet)
- Edit the ***config.json*** in the ***feeder-rl*** directory, replace url and api key.
```json
{
  "tls": "yes",
  "cert_path_name": "certificate.pem",
  "name_spaces": ["vss"],
  "broker_url": "<broker url>",
  "port":"443",
  "client_id" : "volvo-go-client",
  "api_key": "<api key>",
  "vss_tree_path": "../vss/vss-flat-json/normalized-json/vss_n.json",
  "signalfilter": ["Vehicle.Speed","Vehicle.Body.Lights.IsLeftIndicatorOn","Vehicle.VehicleIdentification.VIN","Vehicle.CurrentLocation.Latitude","Vehicle.CurrentLocation.Longitude","Vehicle.Chassis.Accelerator.PedalPosition"]
}
```
- The ***signalfilter*** element contains the signals that we would like the broker to filter our for us.
- Save the file.

**NOTE:**

The feeder uses the redis state storage database to handle incoming datapoints. The location of the database and its
server socket communication file.

running feeder-rl example: 
```
feeder-rl --dataprovider remotive --rdb /tmp/docker/redisDB.sock --fch /tmp/docker/server-feeder-channel.sock
```
For further details view the ***docker-compose-rl.yml*** located in the project root.

**2. server configuration**

The server needs to know where it should forward its requests for writing or reading datapoints.

- Edit the file **feeder-registration.json**
```json
[
  {
    "root":"Vehicle",
    "fname":"/tmp/docker/server-feeder-channel.sock",
    "db": "/tmp/docker/redisDB.sock"
  }

]
```

- set the **fname** element to where the feeder channel socket file should reside. 
- set the **db** element to where the redis database should reside. 

NOTE:
The above file paths and file names are correct. Changing the location of these will require changes in the 
docker-compose yml.

**3. build and run**

- locate the ***docker-compose-rl.yml*** file in the project root
- build the docker containers:
```json
docker compose -f docker-compose-rl.yml build 
```
- revisit the browser from step 1, press the start button, the recorded data playback starts.
- run the docker-compose:
```json
docker compose -f docker-compose-rl.yml up
```

If you get the following output to the docker console you have succesfully connected and started the vissv2server:
```
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Speed, Value=10.240000000000002","time":"2023-11-01T11:36:13Z"}
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Speed, Value=10.32","time":"2023-11-01T11:36:14Z"}
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Chassis.Accelerator.PedalPosition, Value=15.600000000000001","time":"2023-11-01T11:36:14Z"}
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Speed, Value=10.399999999999999","time":"2023-11-01T11:36:14Z"}
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Speed, Value=10.480000000000004","time":"2023-11-01T11:36:14Z"}
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Speed, Value=10.560000000000002","time":"2023-11-01T11:36:14Z"}
feeder             | {"file":"feeder-rl.go:65","level":"info","msg":"Data written to statestorage: Name=Vehicle.Speed, Value=10.64","time":"2023-11-01T11:36:14Z"}
```










