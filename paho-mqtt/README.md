# datacollection-mqtt
mosquitto mqtt broker. Docker image which pulls down and builds the broker. Configured to run with self signed certificates.

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
