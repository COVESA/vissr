module github.com/covesa/vissr

go 1.25.0

//replace github.com/COVESA/vss-tools/binary/go_parser/datamodel => /home/ubjorken/Proj/covesa/vss-tools/binary/go_parser/datamodel
//replace github.com/COVESA/vss-tools/binary/go_parser/parserlib => /home/ubjorken/Proj/covesa/vss-tools/binary/go_parser/parserlib

require (
	github.com/SoundMatt/go-DDS v0.39.0
	github.com/akamensky/argparse v1.4.0
	github.com/apache/iotdb-client-go v1.1.7
	github.com/bradfitz/gomemcache v0.0.0-20230905024940-24af94b03874
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/go-redis/redis v6.15.9+incompatible
	github.com/golang/protobuf v1.5.4
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.1
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/petervolvowinz/viss-rl-interfaces v0.1.0
	github.com/qri-io/jsonschema v0.2.1
	github.com/sirupsen/logrus v1.9.3
	google.golang.org/grpc v1.81.1
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/apache/thrift v0.15.0 // indirect
	github.com/onsi/ginkgo v1.16.5 // indirect
	github.com/onsi/gomega v1.32.0 // indirect
	github.com/qri-io/jsonpointer v0.1.1 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
)
