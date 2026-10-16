# datacollection-mqtt
mosquitto mqtt broker. Docker image which pulls down and builds the broker. Configured to run with self signed certificates.

## Two brokers

This directory contains the setup for two different Mosquitto brokers:

| | Used by | Port | TLS / auth | Files |
|---|---|---|---|---|
| Local test broker | `runtest.sh`, `runtest-service.sh`, `runstack.sh` (VISSR server and test clients) | 1883 | none, bound to 127.0.0.1 | `local-broker.sh`, `mosquitto-local.conf` |
| TLS test broker | `go test ./paho-mqtt/...` and the CI `mqtt-tests` job | 8883 | self signed certificates, password file | `Dockerfile`, `mosquitto.conf`, `generate-certs.sh`, `pwd_file` |

The VISSR server (mqttMgr) and the test clients connect with plain MQTT (`tcp://<MQTT_BROKER_ADDR>:1883`), which is why the TLS broker
can not be used by the scripts, and a separate plain broker is used.

### Local broker for runtest.sh, runtest-service.sh and runstack.sh

The scripts call `local-broker.sh` to start the broker in a Docker container (`vissr-mosquitto-local`, image `eclipse-mosquitto:2`),
and to stop it again at `stopme`. It can also be used manually:
```
./paho-mqtt/local-broker.sh start|stop|status
```
The broker is only started if the environment variable `MQTT_BROKER_ADDR` is unset or points to the local machine
(127.0.0.1 or localhost), and if nothing is already listening on port 1883.
To use the public broker instead:
```
MQTT_BROKER_ADDR=test.mosquitto.org ./runtest.sh startme
```

### TLS broker for the paho-mqtt Go test

```
./paho-mqtt/generate-certs.sh                           # ephemeral certificates in paho-mqtt/cert (git ignored)
docker build -t vissr-mosquitto ./paho-mqtt
docker run -d --rm --name mosquitto -p 8883:8883 vissr-mosquitto
go test -v -count=1 ./paho-mqtt/...
docker stop mosquitto
```
The test connects to `ssl://localhost:8883`, so the CA certificate (`paho-mqtt/cert/ca.crt`) must be trusted by the operating system.
## Building and running the TLS broker manually
To build and run locally.
```
./generate-certs.sh
docker build -t paho-mqtt .
docker run -ti -p 8883:8883 -p 9001:9001 paho-mqtt:latest
```

Installing mosquitto on mac osx issues. Run these commands before
```
sudo mkdir /usr/local/sbin
sudo Chown -R $(whoami) $(brew --prefix) /sbin
```

```
brew install mosquitto
```

To generate Ca.crt you open ```/etc/ssl/openssl.cnf```
and add:

```
[req]
  ...
  req_extensions    = req_text 
  ...
...
[ v3_ca ]
basicConstraints = critical,CA:TRUE
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer:always

[ req_text ]
subjectAltName = @alt_names

[alt_names]
DNS.1 = <server host name>
```

Follow  the steps:https://mosquitto.org/man/mosquitto-tls-7.html to generate new keys.  However, if you need subject alt
names to follow into server and client crt follow below:

```
openssl req -new -out server.csr     -key server.key     -config ssl.conf 
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 10000 -extensions req_ext -extfile ssl.conf

openssl req -new -out client.csr     -key client.key     -config ssl.conf 
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 10000 -extensions req_ext -extfile ssl.conf
```

**Note** Client key should not be encrypted:
```
openssl genrsa -out client.key 2048
```

Building the image:
```
docker build -t datacollection-mqtt:<tag> .
```
