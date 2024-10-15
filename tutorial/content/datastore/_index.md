---
title: "VISSR Data Storage"
---

The VISSR tech stack architecture contains a data storage component located between the server and the feeder(s).
This data store provides a decoupling between the server data access operations and the data access operations of a feeder.
Feeders are expected to keep the data store updated with the latest available value of the signals defined in the VSS tree,
and for client read/subscribe requests the server reads from what is available in the data store.
This leads to that for all client read/subscribe requests the underlying vehicle system does not get involved by instantaneously
having to provide a signal value when asked for by a client.
Client write requests are not passed through the data store (except for the soon to be deprecated version 1 client template type),
but are instead communicated over an Unix Domain Socket IPC channel directly to the feeder by the server.

There are currently four plugin compatible data stores available, based on the following data base solutions.
* SQLite
* Redis
* Memcached
* Apache IotDB

The server is at startup configured via a CLI parameter which DB solution to use, default is Redis.
A feeder must be configured to use the same DB, and implement the common interface for that DB.
An example of this is e. g. found in the feeder/feeder-template/feederv3/feederv3.go in the method
statestorageSet(path, val, ts) which implements SQLite, Redis and Memcached.

It may be a bit confusing that sometimes this is referred to as "data store/storage" and sometimes "state storage".
The latter name is legacy from a previous COVESA project, the Cloud & Connected Services project, while the former has emerged later in the COVESA architecture group work.
An argument for keeping both could be to say that the state storage refers to a storage that only keeps the latest value of a signal,
while the data store refers to a more general database that can also keep time series of values of a signal.
There are two scenarios where the VISSv2 server operates on time series data, [curve logging](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#curvelog-filter-operation),
and [historic data](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#history-filter-operation),
but in this server implementation these data series are temporarily stored within the server, so a "state storage" functionality is sufficient for its needs.
