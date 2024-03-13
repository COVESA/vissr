---
title: "WAII Apache IoTDB"
---

## Introduction
Description of Apache IoTDB from https://iotdb.apache.org/:

*"Apache IoTDB (Database for Internet of Things) is an IoT native database with high performance for data management and analysis, deployable on the edge and the cloud. Due to its light-weight architecture, high performance and rich feature set together with its deep integration with Apache Hadoop, Spark and Flink, Apache IoTDB can meet the requirements of massive data storage, high-speed data ingestion and complex data analysis in the IoT industrial fields."*

## Scope
Support for Apache IoTDB as the WAII data store is implemented by connector code in the WAII service manager, which connects WAII to an external Apache IoTDB server. This code uses the IoTDB Go client to maintain a connection session to the IoTDB server, which is then used to get/set vehicle data from the database.

As WAII and the IoTDB server are separate processes WAII needs to be told where to find the IoTDB server and which storage prefix to use to access the data. The administration of the Apache IoTDB server itself, including startup and shutdown, is out of scope and is handled externally to WAII.

## Runtime notes

### Assumptions
Runtime assumptions:
1. IoTDB server lifecycle (e.g. startup and shutdown) is handled externally to WAII
2. Management (e.g. creation/deletion) of the IoTDB timeseries containing VSS data is handled externally to WAII.
3. Configuration of the connector code is specified in the config file iotdb-config.json. If the config file is not found then build-time defaults are used.

Handling of IoTDB server and timeseries management is placed outside of WAII to allow flexible deployment through loosely coupled connections.
### Database schema
The connector assumes a simple key/value pair schema for accessing VSS data in an IoTDB timeseries:

1. VSS node names (keys) are backtick quoted when stored as measurement keys in the database e.g. `` `Vehicle.CurrentLocation.Longitude` ``. This avoids IoTDB interpreting the VSS tree path, here `Vehicle.CurrentLocation.`, as part of its storage path which also uses a dot notation.

2. VSS data is stored using native (IoTDB) data types rather than strings.

3. That the timeseries containing VSS nodes can be found using the prefix path specified in the config file.

Example timeseries:
```
+------------------------+-----------------------------------------------------+--------+--------+
|                    Time|                                           Timeseries|   Value|DataType|
+------------------------+-----------------------------------------------------+--------+--------+
|2024-03-07T17:55:24.514Z|  root.test2.dev1.`Vehicle.CurrentLocation.Longitude`|-42.4567|   FLOAT|
+------------------------+-----------------------------------------------------+--------+--------+
```

### Configuration
The connection code reads its runtime configuration from the JSON formatted file `iotdb-config.json` located in the vissv2server directory. All values should be specified.

#### Configuration file format

| Key name | Type | Description |
| --- | --- | --- 
|`host`|String|Hostname or IP address of the IoTDB server|
|`port`|String|RPC port of the server. Default is 6667|
|`username`|String|Username to access the server. Default is root|
|`password`|String|Password to access the server. Default is root|
|`queryPrefixPath`|String|Timeseries prefix path of VSS data in the database|
|`queryTimeout(ms)`|Int| Query timeout in milliseconds|


Example `iotdb-config.json`:
```
{
	"host": "127.0.0.1",
	"port": "6667",
	"username": "root",
	"password": "root",
	"queryPrefixPath": "root.test2.dev1",
	"queryTimeout(ms)": 5000
}
```
### Logging
The connector writes information, warning and error messages to the WAII server log with the prefix ``IoTDB``. Grepping in the log for that prefix string can help you quickly identify connector messages.

## Quick start notes
The following notes are intended to help you quickly try out using Apache IoTDB as a data store.

The Apache IoTDB project [website](https://iotdb.apache.org/) has extensive documentation on the IoTDB server.

### Apache IoTDB images
The Apache IoTDB community maintains pre-built server images upstream to [download](https://iotdb.apache.org/Download/). Including containerised Docker images in [Docker Hub](https://hub.docker.com/r/apache/iotdb). IoTDB is available in both standalone (edge) and cluster (cloud) versions. Standalone is suggested as a starting point.

The [COVESA Central Data Service Playground](https://github.com/COVESA/cdsp) provides a Docker deployment that combines an Apache IoTDB server (data store) with the WAII VISS data server.

### Seeding the database with VSS data
To seed the database with VSS data the typical steps are:
1. Create the database in the server
2. Create a timeseries in the database populated with the VSS nodes (keys) you are interested in.
3. Add some example data so the VISS Data Server can successfully get data.

IoTDB has a very extensive collection of integrations, tools, clients and APIs that could be used to achieve this.

#### Example using IoTDB CLI client
 The following tutorial shows an example using the [IoTDB CLI client](https://iotdb.apache.org/UserGuide/latest/Tools-System/CLI.html), using two methods. Firstly, in interactive mode where you type the commands and then sending the same commands in batch command mode.


1. Connect to the CLI client from your host:
```
$ bash <iotdb path>/sbin/start-cli.sh -h <server hostname/ip>
```
2. Create database from CLI command line:
```
IoTDB > create database root.test2.dev1
```
3. Create timeseries from CLI command line:
```
IoTDB > CREATE ALIGNED TIMESERIES root.test2.dev1(`Vehicle.CurrentLocation.Longitude` FLOAT, `Vehicle.CurrentLocation.Latitude` FLOAT, `Vehicle.Cabin.Infotainment.HMI.DistanceUnit` TEXT)
```
4 Add some data into the timeseries:
```
IoTDB> insert into root.test2.dev1(`Vehicle.CurrentLocation.Longitude`, `Vehicle.CurrentLocation.Latitude`, `Vehicle.Cabin.Infotainment.HMI.DistanceUnit`) values(-42.4567, 22.1234, 'MILES')
```
5. Display the data just added as a sanity check:
```
IoTDB> select last * from root.test2.dev1
+------------------------+-------------------------------------------------------------+--------+--------+
|                    Time|                                                   Timeseries|   Value|DataType|
+------------------------+-------------------------------------------------------------+--------+--------+
|2024-03-07T17:55:24.514Z|          root.test2.dev1.`Vehicle.CurrentLocation.Longitude`|-42.4567|   FLOAT|
|2024-03-07T17:55:24.514Z|root.test2.dev1.`Vehicle.Cabin.Infotainment.HMI.DistanceUnit`|   MILES|    TEXT|
|2024-03-07T17:55:24.514Z|           root.test2.dev1.`Vehicle.CurrentLocation.Latitude`| 22.1234|   FLOAT|
+------------------------+-------------------------------------------------------------+--------+--------+
```
You have now seeded the database with some initial VSS data and can use WAII to query it.

The CLI client startup script accepts SQL commands using the `-e` parameter. We can therefore use this to codify the above in a bash script. So the VSS node names (keys) are passed correctly on the command line the backticks must be escaped.

For example:
```
# !/bin/bash

host=127.0.0.1
rpcPort=6667
user=root
pass=root

bash ./sbin/start-cli.sh -h ${host} -p ${rpcPort} -u ${user} -pw ${pass} -e "create database root.test2.dev1"
bash ./sbin/start-cli.sh -h ${host} -p ${rpcPort} -u ${user} -pw ${pass} -e "CREATE ALIGNED TIMESERIES root.test2.dev1(\`Vehicle.CurrentLocation.Longitude\` FLOAT, \`Vehicle.CurrentLocation.Latitude\` FLOAT, \`Vehicle.Cabin.Infotainment.HMI.DistanceUnit\` TEXT)"
bash ./sbin/start-cli.sh -h ${host} -p ${rpcPort} -u ${user} -pw ${pass} -e "insert into root.test2.dev1(\`Vehicle.CurrentLocation.Longitude\`, \`Vehicle.CurrentLocation.Latitude\`, \`Vehicle.Cabin.Infotainment.HMI.DistanceUnit\`) values(-42.4567, 22.1234, 'MILES')"
bash ./sbin/start-cli.sh -h ${host} -p ${rpcPort} -u ${user} -pw ${pass} -e "select last * from root.test2.dev1"
```

Of course any of the programming language clients provided by IoTDB, e.g. go, python, C++, Rust, can also be used to achieve the same result.

## Development notes
Please see the notes in the source commit messages and related Github pull requests for the history of the development of the Apache IoTDB connection code and its integration into the WAII Service Manager component.

Development followed the patterns set by the existing support for Redis and SQLite.

The connection code was first developed with Apache IoTDB v1.2.2, using the upstream standalone pre-built image and Apache IoTDB Go Client v1.1.7.