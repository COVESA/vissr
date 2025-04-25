#!/bin/bash

usage() {
	echo "usage: $0 startme|stopme" >&2
}

startme() {
	echo "Building server..."
	cd server/vissv2server
	go build && mkdir -p logs
	cd ../../
	echo "Starting server"
	screen -S vissv2server -dm bash -c "pushd server/vissv2server && ./vissv2server -m &> ./logs/vissv2server-log.txt && popd"

	echo "Building feederv3..."
	cd feeder/feeder-template/feederv3
	go build && mkdir -p logs
	cd ../../../
	echo "Starting feederv3"
	screen -S feederv3 -dm bash -c "pushd feeder/feeder-template/feederv3 && ./feederv3 -i vssjson -t speed-sawtooth.json &> ./logs/feederv3-log.txt && popd"

	sleep 2s
	echo "Building testClient..."
	cd client/client-1.0/testClient
	go build
	cd ../../../
	echo "Starting testClient"
	screen -S testClient bash -c "pushd client/client-1.0/testClient && go build && ./testClient -m 4 && popd"

	screen -list
}

stopme() {
	echo "Stopping feederv3"
	screen -X -S feederv3 quit

	echo "Stopping vissv2server"
	screen -X -S vissv2server quit

	sleep 1s
	screen -wipe
}

if [ $# -ne 1 ] && [ $# -ne 2 ]
then
	usage $0
	exit 1
fi

case "$1" in 
	startme)
		stopme
		startme $# $2;;
	stopme)
		stopme
		;;
	*)
		usage
		exit 1
		;;
esac
