module github.com/w3c/automotive-viss2

go 1.21

//example on how to use replace to point to fork or local path
//replace github.com/w3c/automotive-viss2/utils => github.com/MagnusGun/WAII/utils master
replace github.com/w3c/automotive-viss2/utils => ./utils

replace (
	//	github.com/COVESA/vss-tools/binary/go_parser/datamodel => github.com/UlfBj/vss-tools/binary/go_parser/datamodel v0.0.0-20231219145220-847ed46699dc
	//	github.com/COVESA/vss-tools/binary/go_parser/parserlib => github.com/UlfBj/vss-tools/binary/go_parser/parserlib v0.0.0-20231219145220-847ed46699dc
	github.com/w3c/automotive-viss2/grpc_pb => ./grpc_pb
	github.com/w3c/automotive-viss2/server/vissv2server/atServer => ./server/vissv2server/atServer
	github.com/w3c/automotive-viss2/server/vissv2server/grpcMgr => ./server/vissv2server/grpcMgr
	github.com/w3c/automotive-viss2/server/vissv2server/httpMgr => ./server/vissv2server/httpMgr
	github.com/w3c/automotive-viss2/server/vissv2server/mqttMgr => ./server/vissv2server/mqttMgr
	github.com/w3c/automotive-viss2/server/vissv2server/serviceMgr => ./server/vissv2server/serviceMgr
	github.com/w3c/automotive-viss2/server/vissv2server/wsMgr => ./server/vissv2server/wsMgr
)

//replace github.com/w3c/automotive-viss2/protobuf/protoc-out => ./protobuf/protoc-out

require (
	github.com/COVESA/vss-tools/binary/go_parser/datamodel v0.0.0-20231222140420-5343e9c0884d
	github.com/COVESA/vss-tools/binary/go_parser/parserlib v0.0.0-20231222140420-5343e9c0884d
	github.com/akamensky/argparse v1.4.0
	github.com/apache/iotdb-client-go v1.1.7
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/go-redis/redis/v8 v8.11.5
	github.com/golang/protobuf v1.5.3
	github.com/google/uuid v1.5.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.1
	github.com/mattn/go-sqlite3 v1.14.19
	github.com/petervolvowinz/viss-rl-interfaces v0.0.8
	github.com/sirupsen/logrus v1.9.3
	google.golang.org/grpc v1.60.1
	google.golang.org/protobuf v1.31.0
)

require (
	github.com/apache/thrift v0.15.0 // indirect
	github.com/bradfitz/gomemcache v0.0.0-20230905024940-24af94b03874 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	golang.org/x/net v0.19.0 // indirect
	golang.org/x/sync v0.5.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20231212172506-995d672761c0 // indirect
)
