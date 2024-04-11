---
title: "VISSv2 Access Control Servers"
---

The [VISSv2 access control model](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#access-control-model) specifies two authorization servers:
* Access Grant server
* Access Token server

### Access Grant server
This server is in a typical scenario running in the cloud. It is built as a separate executable in the vissr/server/agt_server directory

$ go build

and run by

$ ./agt_server

It exposes an HTTP API according to the VISSv2 specification. However it is currently not TLS protected (which is a must in non-development scenario).
What is also missing in the AGS implementation is authentication of the client, which according to the specification should be an AGT task.

### Access Token server

This server runs as a thread within the vissv2 server, so it is built by the vissv2 build command.
For it to be built, it is necessary to make sure that the "atServer" line in the serverComponents array in the vissv2server.go code is uncommented:
```
var serverComponents []string = []string{
	"serviceMgr",
	"httpMgr",
	"wsMgr",
	"mqttMgr",
	"grpcMgr",
	"atServer",
}
```
If it is part of the vissv2server build, and if a VSS node is access control tagged,
the server will then forward the access token received in the client request to the ATS for validation.

The ATS will as part of the validation also use the VISSv2 specified policy documents if they are found in the working directory.

The ATS supports caching of access tokens, and returning a token handle to the client if cached. The cache is configured to hold max 10 tokens.
If the cache is full, caching of one more is rejected until a cached token becomes expired, or pre-emptied by other reasons.

For the AT server to verify the AGT signature it needs to have access to the AGTS public key. To facilitate that it is necessary to copy the server/agt_server/agt_public_key.rsa file
to the server/vissv2server directory.

### Server configuration
The configuration available for the servers is whether the protocols that they implement to expose their APIS are TLS protected or not.
The same framework that is used for generating credentials for the client-server communication described [here](https://github.com/covesa/vissr/tree/master/testCredGen/)
can be used in this case also.
These credentials should however to follow good security practises be separate from what is used in the client-server communication.
The different port number for the respective servers are shown below.
| Server  | Port number: No TLS | Port number: TLS |
|-----|---------|---------|
| AGTS|   7500  |   7443  |
| ATS |   8600  |   8443  |

### VISS web client submodule

This submodule implements a [VISSv2 web client](https://github.com/nicslabdev/viss-web-client/)
that exposes a UI that is considerably more sophisticated than what other clients on the VISSR repo exposes,
and it is particularly helpful when it comes to the client interactions with access control involved.
Check out the README on both repos for more information.

### Consent support
The VISSv2 specification provides support for requesting consent from a data owner before allowing a client to access the data.
The model for this is that the process for obtaining the owner consent is delegated to an External Consent framework (ECF),
and the details ofthis process is out-of-scope in relation to the VISSv2 specification.
What is in scope is a high-level description of the protocol between the VISSv2 server and the ECF, see the
[VISSv2 consent support](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#consent-support) chapter.
To configure the VISSv2 server to try to connect to an ECF, it must be started with the command parameter -c".

The figure below shows the the different steps in the dataflow that is necessary when a client wants to initiate a subscription of data that is
access controlled and require consent from the data owner.
![VISSv2 consent subscribe dataflow](/vissr/images/VISSv2-consent-subscribe-dataflow.jpg?width=40pc)
The dataflow describes a scenario when the client successfully subscribes to data the require both access control and consent by the data owner.
Consent can only be required in combination with requiring access control, please see the
[Access Control Model](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#access-control-model) chapter.

1. The client issues a request to the Access Grant Token server (AGTS).

1.1 The AGTS verifies the client request and returns an Access Grant Token (AGT).

2. The client issues a request to the Access Token server (ATS).

2.1 The ATS writes the data in the client request to the Pending list, associating a reference Id to it.

2.2 The ATS issues a request to obtain consent to the ECF, including the reference Id.

2.3 The ATS sends a response to the client containing a reference to the entry in the Pending list.

2.4 The ECF has obtained (a positive) consent and issues a message to the ATS containing the consent and the reference Id.
The ATS updates the consent info in the Pending list (initially set to NOT_SET).

2.5 The client issues an inquiry request to the ATS containing the reference Id.

2.6 The entry on the Pending list is used to generate the AT, is then deleted, and a new entry containing the AT, keeping the same reference Id, is created on the Active list.

2.7 A response containing the AT is returned to the client. If the inquiry request 2.5 happened before 2.4 then the ATS returns the same reference Id without executing 2.6,
but if 2.4 happened before, so that the consent no longer has the value NOT_SET, then if the consent was set to YES,
the ATS generates the Access Token (AT), and returns it to the client. If the consent was set to NO, the consent data is returned without the AT.

3. The client issues the subscribe request to the VISSv2 server containing the AT. The AT in the client request my be represented by a handle instead of the entire AT,
see the [Protected Resource Request](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#protected-resource-request) chapter.

3.1 The VISSv2 server issues an AT validation request to the ATS. The ATS finds it on the Active list, and validates the AT.

3.2 The ATS returns the validation result to the VISSv2 server, and the reference Id from the matching entry on the Active list.

3.3 Assuming a positive AT validation, the VISSv server forwards the client subscribe request, and the reference Id, to the service manager.

3.4 The service manager creates an entry on the subscription list containing the required data for being able to issue event messages to the client containing the
requested signals. The reference Id is also saved.

3.5 and 3.6 The service manager creates the response message associated to the request in 3.

From this point on the servicemanager will when the event described in the filter data from the client request is triggered issue event messges to the client.
This will continue until any of the following happens:

A. The client issues an unsubscribe request.

B. The ECF issues a consent cancellation request to the ATS.

C. The AT expiry time is reached.

Alternative A:
The service manager will delete the entry on the subscription list. The corresponding entry on the Active list will remain.

Alternative B:
The ATS will delete the entry on the Active list, and issue a request to the service manager to delete the entry on the subscription list corresponding to the
reference Id from its deleted entry.

Alternative C:
The ATS will delete the entry on the Active list correspnding to the reference Id received from the ECF,
and issue a request to the service manager to delete the entry on the subscription list corresponding to the reference Id.

#### Payload syntax for the messages in the client-to-ATGS communication:

AGT request: {“action”: “agt-request”, "vin":"pseudo-vin", "context":"triplet-sub-roles-see-spec", "proof":"ABC", "key":"DEF"}

AGT response: {“action”: “agt-request”, "token":"x.y.z"}  // if successful validation

Error response: {“action”: “agt-request”, "error":"error-reason"}  // if unsuccessful validation

#### Payload syntax for the messages in the client-to-ATS communication:

AT request: {“action”: “at-request”, "agToken":"x.y.z", "purpose":"purpose-description", "pop":""}  // pop shall be an empty string for access control short term flow.

AT response 1: {“action”: “at-request”, "aToken":"x.y.z"} ATS->Client // Consent is not required.

AT response 2: {“action”: “at-request”, "sessionId ":"reference-Id", “consent”:”NOT_SET”} // Consent is required, and consent reply not obtained yet from ECF.

AT inquiry request: {“action”: “at-inquiry”, "sessionId":"reference-Id"}  // may need to be issued multiple times until consent is provided by ECF.

AT inquiry response 1: {“action”: “at-inquiry”, "aToken":"x.y.z", “consent”:”YES”} ATS->Client // ECF has provided a positive consent.

AT inquiry response 2: {“action”: “at-inquiry”, "sessionId ":"reference-Id", “consent”:”NOT_SET”} // Reply is not obtained yet from ECF.

AT inquiry response 3: {“action”: “at-inquiry”, “consent”:”NO”} // ECF has provided a negative consent.

Error response: {“action”: “same-as-in-request”, "error":"error-reason"}


#### Payload syntax for the messages in the ATS-to-ECF communication:

Consent request: {“action”: “consent-ask”, “user-roles”: “triplet-sub-roles-see-spec”, “purpose”: “purpose-description”, "signal_access": [{},..{}], “messageId”: ”reference-Id”}

Consent reply request: {“action”: “consent-reply”, “consent”: “YES/NO”, “messageId”: ”reference-Id”}

Consent cancellation request: {“action”: “consent-cancel”, “messageId”: ”reference-Id”}

Response to above requests: {“action”: “same-as-in-request”, “status”: “200-OK/404-Not found/401-Bad request”} // Status is one of the three examples shown.

See the specification chapter[Purpose list](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#purpose-list) for the definition of the signal_access object.
