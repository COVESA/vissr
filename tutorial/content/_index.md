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

### Client deployment options
As is shown in the figure above the VISS interface is well suited to serve clients deployed in different environments:
* Cloud deployment. Typically connected via Internet connectivity.
* Proximity deployment. Typcially in a mobile phone connected via any short range connectivity such as Bluetooth or WiFi.
* In-vehicle deployment. Typically as an app in the infotainment environment.

The payloads handled by the clients at any of these deployments are identical.

The MQTT transport protocol option, with the broker deployed in the cloud,
is well suited for the client cloud deployment as the communication can traverse across subnets.
The thin application layer protocol on top of MQTT that VISS defines makes it possible
to keep the client-server communication, and payload compatibility.
