#!/bin/bash

usage() {
	echo "usage: $0 startme <feederConfigX.json> (name of config file for 2nd feeder)"
	echo "usage: $0 stopme <2> (if two feeders were started)"
}

startme() {
	echo "Building server..."
	cd server/vissv2server
	go build && mkdir -p logs
	cd ../../

	echo "Building feederv4..."
	cd feeder/feeder-template/feederv4
	go build && mkdir -p logs
	sleep 1s
	cd ../../../

	echo "Starting server"
	screen -S vissv2server -dm bash -c "pushd server/vissv2server && ./vissv2server -m -d &> ./logs/vissv2server-log.txt && popd"

	sleep 1s
	echo "Starting feederv4"
	screen -S feederv4 -dm bash -c "pushd feeder/feeder-template/feederv4 && ./feederv4 &> ./logs/feederv4-log.txt && popd"

	sleep 1s
	if [ $# -eq 1 ];
	then
		echo "Starting second feederv4"
		screen -S feederv4_2 -dm bash -c "pushd feeder/feeder-template/feederv4 && ./feederv4 -c $1 &> ./logs/feederv4-log-2.txt && popd"
	fi

	screen -list
}

stopme() {
	echo "Stopping feederv4"
	screen -X -S feederv4 quit

	if [ $# -eq 1 ];
	then
		echo "Stopping second feederv4"
		screen -X -S feederv4_2 quit
	fi
	echo "Stopping vissv2server"
	screen -X -S vissv2server quit

	sleep 1s
	screen -wipe
}

if [ $# -ne 1 ] && [ $# -ne 2 ];
then
	usage $0
	exit 1
fi

case "$1" in 
	startme)
		stopme
		startme $2
		;;
	stopme)
		stopme $2
		;;
	*)
		usage
		exit 1
		;;
esac
