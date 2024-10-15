---
title: "COVESA Vehicle Information Service Specification ver 3 Reference Implementation Tutorial"
---
## COVESA Vehicle Information Service Specification ver 3 Reference Implementation Tutorial

The COVESA VISSv3 specification is under development at the [COVESA VISS Github](https://github.com/COVESA/vehicle-information-service-specification).
A VISSv3.0 reference implementation in the form of a server that exposes an interface according to the specification is developed on the master branch,
while a VISSv2.0 reference implementation is available on the v2.0 branch.

This documentation covers the VISSv3.0 specification.
The new features are listed below.
It is with a few small exceptions backwards compatible with VISSv2.0. The exceptions are listed below.

### VISSv3.0 new features
* Multiple tree support. The server can be configured to manage multiple trees that a client can access.
* Server capabilities documented and client accessible in the Server tree.
* (File transfer.) !!Part of VISSv3.0 spec but not yet implemented!!
* gRPC support. This wa already available on an experimental level in VISSR @v2.0.
* Any further new features added to the VISSv3.0 specification will become implemented.

### Non-backwards compatible changes from VISSv2.0
* The filter keyname "type" is changed to "variant".
* The filter variants "static-metadata" and "dynamic-metadata" are replaced by the variant "metadata".
* The "subscriptionId" parameter in unsubscribe response messages is deleted.

Also found on this repo are implementations of other components that are needed to realize a communication tech stack that reaches from clients through the server and to the underlying vehicle system interface.

![VISSv2 communication tech stack](/vissr/images/WAII-tech-stack.jpg?width=40pc)

These software components (SwCs) can be categorized as follows:
* server
* clients
* data storage
* feeders
* tools

The tutorial describes each SwC category in a separate chapter.
It also contains a few Proof of concept (POC) examples, and information about installing,
building and running Golang based SwCs, a Docker containerization, and about some peripheral components.
