---
title: "VISSR Memcached"
---

## Memcached state storage
Quoting from the [Memcached](https://memcached.org/) site, "Memcached is an in-memory key-value store for small chunks of arbitrary data (strings, objects)".
The key is in this context the path, and the value is a JSON string containing the value and the associated time stamp.

The memcached store is started as a daemon, which is not automatically terminated when the server terminates.
The commands below can be used to manually terminate the memcached daemon.

$ ps -A | grep "memcached"

then remove it with the command

$ kill pid

where pid comes from the result of the first command.

Communication with the Memcached daemon is for security reasons configured to use Unix domain sockets. This requires that the socket file, and the directory it is stored in exist.
If not then create it with the commands

$ makedir path-to-socket-file-directory

$ touch socket-file-name
