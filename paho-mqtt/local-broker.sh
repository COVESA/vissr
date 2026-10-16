#!/bin/bash
#
# Starts/stops a local Mosquitto MQTT broker (plain MQTT, port 1883, anonymous)
# in a Docker container. Used by runtest.sh and runstack.sh so that the VISSR
# MQTT testing does not depend on the public test.mosquitto.org broker.
#
# usage: local-broker.sh start|stop|status
#
# The broker address used by the VISSR server and clients is selected by the
# MQTT_BROKER_ADDR environment variable (default 127.0.0.1). "start" only
# starts a container when that variable is unset/empty or points to the local
# machine (127.0.0.1, localhost). If it points anywhere else, e.g.
# test.mosquitto.org, nothing is started.
#
# If something is already listening on port 1883 it is assumed to be a local
# broker that the user has started, and it is used as is (and never stopped).

CONTAINER=vissr-mosquitto-local
IMAGE=eclipse-mosquitto:2
PORT=1883
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF="$SCRIPT_DIR/mosquitto-local.conf"

usage() {
	echo "usage: $0 start|stop|status"
}

port_open() {
	(exec 3<>/dev/tcp/127.0.0.1/$PORT) 2>/dev/null
}

container_running() {
	command -v docker >/dev/null 2>&1 && \
		[ -n "$(docker ps -q -f "name=^${CONTAINER}$" 2>/dev/null)" ]
}

broker_is_local() {
	case "${MQTT_BROKER_ADDR:-}" in
		""|127.0.0.1|localhost) return 0 ;;
		*) return 1 ;;
	esac
}

startbroker() {
	if ! broker_is_local; then
		echo "MQTT_BROKER_ADDR=$MQTT_BROKER_ADDR: using external broker, not starting a local one"
		return 0
	fi

	if container_running; then
		echo "Local MQTT broker container '$CONTAINER' is already running"
		return 0
	fi

	if port_open; then
		echo "Port $PORT is already in use, assuming a local MQTT broker is running. Using it."
		return 0
	fi

	if ! command -v docker >/dev/null 2>&1; then
		echo "ERROR: Docker is needed to start the local MQTT broker but was not found." >&2
		echo "Either install Docker, start your own broker listening on port $PORT," >&2
		echo "or use the public broker: MQTT_BROKER_ADDR=test.mosquitto.org $0" >&2
		return 1
	fi

	echo "Starting local MQTT broker (Docker container '$CONTAINER')"
	if ! docker run -d --rm --name "$CONTAINER" \
		-p 127.0.0.1:$PORT:$PORT \
		-v "$CONF":/mosquitto/config/mosquitto.conf:ro \
		"$IMAGE" >/dev/null; then
		echo "ERROR: failed to start the local MQTT broker container" >&2
		return 1
	fi

	# Docker publishes the port before Mosquitto is ready, so probe the broker
	# itself with a test publish from inside the container.
	for i in $(seq 1 30); do
		if docker exec "$CONTAINER" mosquitto_pub -h 127.0.0.1 -p $PORT -t vissr/probe -m probe >/dev/null 2>&1; then
			echo "Local MQTT broker is ready on port $PORT"
			return 0
		fi
		sleep 1
	done
	echo "ERROR: local MQTT broker did not become ready within 30s" >&2
	docker logs "$CONTAINER" >&2
	docker stop "$CONTAINER" >/dev/null 2>&1
	return 1
}

stopbroker() {
	if container_running; then
		echo "Stopping local MQTT broker"
		docker stop "$CONTAINER" >/dev/null
	fi
}

case "$1" in
	start)
		startbroker ;;
	stop)
		stopbroker ;;
	status)
		if container_running; then
			echo "running (container '$CONTAINER')"
		elif port_open; then
			echo "a broker not started by this script is listening on port $PORT"
		else
			echo "not running"
			exit 1
		fi ;;
	*)
		usage
		exit 1 ;;
esac
