---
title: "VISSR Feederv3"
---

If feederv3 is started with the CLI parameter -h the following is presented:
```
usage: print [-h|--help] [-m|--mapfile "<value>"] [-s|--scldatafile "<value>"]
             [-t|--tripdatafile "<value>"] [--logfile] [--loglevel
             (trace|debug|info|warn|error|fatal|panic)] [-i|--simsource
             (vssjson|internal)] [-d|--statestorage
             (sqlite|redis|memcache|none)] [-f|--dbfile "<value>"]

             Data feeder template version 3

Arguments:

  -h  --help          Print help information
  -m  --mapfile       VSS-Vehicle mapping data filename. Default:
                      VssVehicle.cvt
  -s  --scldatafile   VSS-Vehicle scaling data filename. Default:
                      VssVehicleScaling.json
  -t  --tripdatafile  Filename for simulated trip data. Default: tripdata.json
      --logfile       outputs to logfile in ./logs folder
      --loglevel      changes log output level. Default: info
  -i  --simsource     Simulator source must be either vssjson, or internal.
                      Default: internal
  -d  --statestorage  Statestorage must be either sqlite, redis, memcache, or
                      none. Default: redis
  -f  --dbfile        statestorage database filename. Default:
                      ../../server/vissv2server/serviceMgr/statestorage.db
```

* The -m command line option is used to set which mapfile that the feeder will use if the selected simsource is 'internal'.
The map file is created using the [Domain Conversion Tool](/vissr/tools/#domain-conversion-tool).
The default file VssVehicle.cvt is an example file containing a small set of signals:
  * - VSS: Vehicle.LowVoltageSystemState <-> CAN: LVoltSysSt
  * - VSS: Vehicle.CurrentLocation.GNSSReceiver.FixType <-> CAN: GpsFxTy
  * - VSS: Vehicle.CurrentLocation.Longitude <-> CAN: GpsLong
  * - VSS: Vehicle.TripMeterReading <-> CAN: TrMetRead
  * - VSS: Vehicle.Speed <-> CAN: VehSpd
* The -s command line option selects the file containing instructions for the feeder on enum value mapping and linear scaling.
The scaling file is created using the [Domain Conversion Tool](/vissr/tools/#domain-conversion-tool).
The default file VssVehicleScaling.json is an example file containing instructions used together with the default map file.
* The -t command line option selects a file containing simulated trip data that the feder will use if the selected simsource is 'vssjson'.
More information about the format of this file is found in "Simulated Trip Data" below.
* The -i command line option selects which simulation source that shall be used.
  * If 'internal' is selected then data from the map file and scaling file are used.
  * If 'vssjson' is selected then data from the tripdata file is used.
* The -d command line option selects the interface that the feeder shall use in its interaction with the statestorage database.
The alternatives are:
  * sqlite
  * redis
  * memcache

Default is redis. The server must be CLI configured for the same database. The database must be installed on the computer.
If SQLite is seleted then the database must be preconfigured. See [VISSR Data Storage](/vissr/datastore) for more information.
* The -f command line option selects the SQLite database file to use if that was selected as statestorage type.
A default file is available, stored in the vissr/server/vissv2server/serviceMgr directory.
More information on the SQLite preconfiguration can be found [here](https://github.com/COVESA/ccs-components/tree/master/statestorage/sqlImpl).

## Simulated Trip Data
When the feederv3 is configured to replay simulated trip data the format of this file must be as described here.
The feeder reads the JSON formatted file at startup and then it starts to read the first sample for all the signals represented in the file
and write them into the statestorage. It then waits for one scond before it reads the second sample of all the signals and write them.
This pattern is repeated until the last sample after which the feeder start from the begining again to replay the first samples.

The data associated to a sample is a path, a value, and a timestamp.

The conversion information in a map file is not used with trip data simulations so the path
must be present in a binary tree that the server is configured with or else it cannot be read by a client.

The value must be in string format and the content of the string must be compatibe with the datatype that is
associated with the signal in a tree that the server manages.

The timestamp is currently not used so it can be set to an empty string.
However, it may be that coming improvements o the feeder simulation capability starts to use it,
so to be 'future proof' it is recommended to set it to a relevant time.

The example file tripdata.json in the feederv3 directory is shown below.
```
[
{ "path": "Vehicle.TraveledDistance", "dp": [ { "ts": "2020-04-15T13:37:00Z", "value": "1000.0" }, { "ts": "2020-04-15T13:37:05Z", "value": "1000.0" }, { "ts": "2020-04-15T13:37:10Z", "value": "1001.0" }, { "ts": "2020-04-15T13:37:15Z", "value": "1001.0" } ]},

{ "path": "Vehicle.CurrentLocation.Longitude", "dp": [ { "ts": "2020-04-15T13:37:00Z", "value": "56.02024719364729" }, { "ts": "2020-04-15T13:37:00Z", "value": "56.02233748493814" }, { "ts": "2020-04-15T13:37:00Z", "value": "56.02565421151008" }, { "ts": "2020-04-15T13:37:05Z", "value": "56.02823595803967" } ]},

{ "path": "Vehicle.CurrentLocation.Latitude", "dp": [ { "ts": "2020-04-15T13:37:00Z", "value": "12.599927977070497" }, { "ts": "2020-04-15T13:37:00Z", "value": "12.601058355542794" }, { "ts": "2020-04-15T13:37:00Z", "value": "12.602554268256588" }, { "ts": "2020-04-15T13:37:05Z", "value": "12.603616368784676" } ]}
]
```

The number of samples for each signal must be the same, so for signals that do not change every second their value must be repeated.
Boolean values must be represented as 'true' or 'false'.

### Simulated actuation
For signals written to the server, if the value is of either integer or float datatypes,
then the feederv3 will simulate that it takes some time for the underlying vehicle system to change the state to this new value.
The server will in 10 steps linearly update the signal value starting from the value zero.
After each step it waits one second so the final value is reached after 10 seconds.
