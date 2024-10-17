---
title: "VISSR peripheral components"
---

A few other software components that can be useful when setting up a VISSv2 communication tech stack exists:
* Authorization servers for access control and consent models.
* Open Vehicle Data Set, a relational database with a table configuration that enables it to store time series of VSS data from multiple vehicles.
* A "live simulator" that can read vehicle trip data stored in an OVDS database , and replay it so that it appears as live data from the recorded trip.

## Access control authorization servers
The VISS2 specification describes an access control model involving two authorization servers:
* Access Grant Token server
* Access Token server
For details please read the [VISSv2: Access Control](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#access-control-model),
and the [Consent Model]() chapters.

To trigger the access control and consent functionality it is necessary to tag the corresponding VSS nodes as described in the spec.
This can either be done by editing of the actual vspec files from the [VSS/spec](https://github.com/COVESA/vehicle_signal_specification/tree/master/spec) directory,
or by creating overlay files and include them as described in [VSS-tools](https://github.com/COVESA/vss-tools),
and then generate the VSS tree in binary format as described in [VSS tree configuration](https://covesa.github.io/vissr/server/#vss-tree-configuration).

### Access Grant Token server (AGTS)
The [AGTS](https://github.com/covesa/vissr/tree/master/server/agt_server),
which typically will be deployed off-vehicle, in the cloud, is separately built and deployed.
The file agt_public_key.rsa is generated at startup, which must be copied to the [AT server](https://github.com/covesa/vissr/tree/master/server/vissv2server/atServer) directory.

### Access Token server (ATS)
The [ATS](https://github.com/covesa/vissr/tree/master/server/vissv2server/atServer) is deployed on a separate thread within the VISSv2 server,
to include it make sure it is uncommented in the serverComponents string array in [viss2server.go](https://github.com/covesa/vissr/blob/master/server/vissv2server/vissv2server.go).
The ATS uses the [policy documents](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#policy-documents) described in the spec when validating an access token,
examples of these are available in the purposelist.json and scopelist.json files.

## Open Vehicle Data Set (OVDS)
The code to realize an OVDS database is found [here](https://github.com/COVESA/ccs-components/tree/master/ovds).
The database is realized using SQLite, so it is possible to use the SQLite API to read and write from it.

However, an [OVDS server](https://github.com/COVESA/ccs-components/tree/master/ovds/server) is available that exposes a small set of methods for this, over HTTP.
For more details, please check the README on the link.

There is as well an [OVDS client](https://github.com/COVESA/ccs-components/tree/master/ovds/client)
available that connects to a VISSv2 server to fetch data that it then writes into the OVDS using the OVDS server interface.

## Live simulator
The [live simulator](https://github.com/COVESA/ccs-components/tree/master/livesim) reads data from an OVDS containing recorded trip data,
and then writes it into a state storage timed by the data time stamps so that it appears timing wise as when it was recorded.
For more details, please check the README on the link.

The the test_vehicles.db file in the [OVDS server](https://github.com/COVESA/ccs-components/tree/master/ovds/server)
directory contains trip data generously provided by Geotab Inc.
It can be used as input to the live simulator, as well as the sawtooth_trip.db for simple testing.

The live simulator needs a copy of the list of leaf node paths (vsspathlist.json),
which needs to contain at least all the paths that are to be replayed from the OVDS, and are also to be found in the VSS tree that the VISSv2 server uses.
