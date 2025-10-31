# Access Token Server (ATS)

  The access token server is deployed in the vehicle, and has two major tasks:
  
1. Providing access tokens to requesting clients.

2. Support the VISSv2 server with validation of client requests to access restricted VSS nodes.

The first task is supported by the ATS through that it exposes a service to clients over the transports:

- HTTP
- MQTT


## HTTP AT Requests
 
A client using HTTP shall issue a POST message with the path "/ats", on the port number 8600.

The POST message, in case of using a Short Term Access Grant Token should contain the following:

```
POST /ats HTTP/1.1
...
{
"purpose":"pay-as-you-drive",
"token":"eyJhbGciON . . . J4OjAKsltT7x"
}
```

In case of being using a Long Term Access Grant Token (the difference is that the long term includes the client key), a proof of possession of the key must be issued.

```
POST /ats HTTP/1.1
...
{
"purpose":"pay-as-you-drive",
"token":"eyJhbGciON . . . J4OjAKsltT7x",
"pop": "eyJ0eXAiOiJ. . . GAdLinsCffKKEA"
}
```

## MQTT AT Requests

A client using the MQTT transport must apply the application level protocol which is described in the <a  href="https://github.com/COVESA/vissr/tree/master/server/mqtt_mgr">MQTT manager directory</a>, with the difference that the ATS is subscribing to the following topic:

VIN/access-control

> Note: Not developed yet.

## AT Request Validation
An AT Request must contains the following claims that must be validated:

- **Purpose**: It must be on the Purposelist file holded by the AT server. The purpose represents a set of signals that a client can access and its permissions.
- **Token**: The Access Grant Token that authenticates the client and allows to check the requested purpose with its context.  The following claims are validated:
	- Expiration times
	- Client context: must be valid with the purpose
	- Audience claim: must be set to "w3org/gen2"
	- JWT Identifier: must be valid
	- Public Key: Included in case of long term. Must match the key in the Proof of Possession.
	- Token Signature: The AGT must be signed by the AGT Server.
- **Proof of Possession**: The proof of possession must match the public key in the AGT received. The PoP token must be valid.

## Token Validation

The VISS Server can send requests to the Access Token Server in order to validate Access Tokens using HTTP. The POST message has the following structure:

```
POST /ats HTTP/1.1
...

{
	"Action":"read",
	"Token":"eyJhbGciON . . . J4OjAKsltT7x"
	"Paths":"Vehicle.Speed"
}
```


The Access Token Server will reply to the request telling if the Access Token received is valid or not. In case it is valid, the code 0 is included in the response:

```
{
	"validation":"0"
}
```
The response may in one special case contain a token handle that a client may use instead of sending the entire ccess token in subsequent requests,
see the chapter "Protected Resource Request" in the VISSv2 Core specification.

If the response was successfully validated, then it may have one more key-value pair as shown in the example below, where its value is the token handle to be returned to the client.
```
{
	"validation":"0"
	"handle":"dyt0CoTl4WoVjAHI9Q_CwSKhl6d_9rhM3NrXuJttkao"
}
```


In case it is not valid, a set of error codes has been defined:

#### 1 - 4: Syntax error codes
	- 1: AT is not valid: Wrong format: can not be decoded or unmarshalled.
	- 2: AT not included in the request.
#### 5 - 9: Signature error codes
	- 5: Invalid Signature.
	- 6: Invalid Signature Algorithm.
#### 10 - 19: Time Claim error codes
	- 10: Invalid IAT claim. Syntax error.
	- 11: Invalid IAT claim. Future time not allowed.
	- 15: Invalid EXP claim. Syntax error.
	- 16: Invalid EXP claim. Token expired.
#### 20-29 Claim error codes
	- 20: Invalid aud claim.
	- 21: Invalid context.
#### 30-39 Revocation error codes
 	- 30: Invalid jti: token revoked. 
#### 40-59 Internal Errors
	- 40: Internal error: no connection with AT
	- 41: Internal error: fail reading AT response
	- 42: Internal error: fail generating AT validation request
#### 60-69 Permission Errors
	- 60: Permission error: no access allowed with that purpose
	- 61: Permission error: read-only access trying to write


**Tests, access_control_test.go**

Testing the Access Grant Token server and the Access Token server can be done running access_control_test.go
The default feeder can be used but also a rl-feeder with recorded data playback. The AGT server must currently
be started manually. Test cases can also be built and run,debugged individually. Recommend to use an IDE with debugger
for this. 

To run the tests, configure the ***config.json*** file for the ***feeder-rl*** to point to the demo broker.
See: https://demo.remotivelabs.com/orgs/remotidemo. 


Commandline

```
0. Build and run the AGTServer, using the docker/agt-docker/docker-compose-agt.yml
1. $ docker compose -f docker-compose-agt.yml build
2. $ docker compose -f docker-compose-agt.yml up
3. From the Remotive labs cloud demo Open the Turning Torso Drive Cycle and select  configuration_vss.
4. Select play
5. Start the playback
7. Copy the broker url and api key and enter into the config.json for the feeder-rl
8. $ docker compose -f docker-compose-rl.yml build
9. $ docker compose -f docker-compose-rl.yml up
10. navigate to server/vissv2server/atServer
11. $ go test -v
```



