#!/bin/bash

usage() {
	echo "usage: $0 startme|stopme" >&2
}

startme() {
	echo "Starting server"
	screen -S vissv2server -dm bash -c "pushd server/vissv2server && go build && mkdir -p logs && ./vissv2server -m &> ./logs/vissv2server-log.txt && popd"

	echo "Starting feederv3"
	screen -S feederv3 -dm bash -c "pushd feeder/feeder-template/feederv3 && go build && mkdir -p logs && ./feederv3 -i vssjson &> ./logs/feederv3-log.txt && popd"

	echo "Starting testClient"
	screen -S testClient bash -c "pushd client/client-1.0/testClient && go build && ./testClient && popd"

	screen -list
}

stopme() {
	echo "Stopping feederv3"
	screen -X -S feederv3 quit

	echo "Stopping vissv2server"
	screen -X -S vissv2server quit

	sleep 1
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
