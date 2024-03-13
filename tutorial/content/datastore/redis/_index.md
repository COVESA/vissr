---
title: "VISSR Redis"
---

## Redis state storage
When a Redis database is used as the state storage then there is no explicit database file to handle as the database is managed in-memory by the Redis daemon.
Instead it is necessary to configure and launch the daemon.
This is already configured in the redis/redisNative.conf that is used as input in the bash command in the server/viss2server/redisNativeInit.sh file that is called at server startup.

To avoid multiple daemons being started, the server checks if the daemon is already running before starting an instance of it.
If there is a need to stop a running daemon, first find the daemon pid with the command

$ ps -A | grep "redis"

then remove it with the command

$ kill pid

where pid comes from the result of the first command.

Communication with the Redis daemon is for security reasons configured to use Unix domain sockets. This requires that the socket file, and the directory it is stored in exist.
If not then create it with the commands

$ makedir path-to-socket-file-directory

$ touch socket-file-name

### Alternative Redis server initiation
If there is a need to start the Redis server a different way than what is described above then the [redisInit.go](https://github.com/COVESA/ccs-components/tree/master/statestorage/redisImpl) file on this link will configure and launch it.
The server code starting the daemon would first need to be commented out to avoid multiple instantiations.

