---
title: "WAII Data Storage"
---

Two realizations of data storage are available on the [COVESA/CCS-components Github](https://github.com/COVESA/ccs-components),
one using an SQLite database, and the other a Redis database. In addition WAII also supports connection to an Apache IoTDB server.

The server implements the APIs to these databases, which to use is selected by its command line configuration.

The same support should be available on the other SwC that accesses the data storage,
however the current feeder implementation only implements Redis support, hence it is not merged into the master branch but resides on the feeder branch.

It may be a bit confusing that sometimes this is referred to as "data store/storage" and sometimes "state storage".
The latter name is legacy from a previous COVESA project, the Cloud & Connected Services project, while the former has emerged later in the COVESA architecture group work.
An argument for keeping both could be to say that the state storage refers to a storage that only keeps the latest value of a signal,
while the data store refers to a more general database that can also keep time series of values of a signal.
There are two scenarios where the VISSv2 server operates on time series data, [curve logging](https://raw.githack.com/w3c/automotive/gh-pages/spec/VISSv2_Core.html#curvelog-filter-operation),
and [historic data](https://raw.githack.com/w3c/automotive/gh-pages/spec/VISSv2_Core.html#history-filter-operation),
but in this server implementation these data series is temporarily stored within the server, so a "state storage" functionality is sufficient for its needs.
