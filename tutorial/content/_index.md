---
title: "COVESA Vehicle Information Service Specification version 3.1 Reference Implementation Tutorial"
---
## COVESA Vehicle Information Service Specification ver 3.1 Reference Implementation Tutorial

The COVESA VISSv3.1 specification development is not yet started at the [COVESA VISS Github](https://github.com/COVESA/vehicle-information-service-specification).
As the set of new features are quite limited, see below, it was deemed possible to develop  VISSv3.1 reference implementation before the specification is released.

A VISSv3.0 reference implementation in the form of a server that exposes an interface according to the specification is available on the master branch,
while the VISSv3.1 reference implementation is available on the v3.1 branch.
When the VISSv3.1 specification is released the v3.1 branch will be merged to the master branch.

This documentation covers the VISSv3.1 specification, which is backwards compatible with the VISSv3.0 specification.
The new features are listed below.

### VISSv3.1 new features
* Supports the [COVESA HIM](https://github.com/COVESA/hierarchical_information_model) Data profile.
* Supports client "forest discovery" request.
* Makes it mandatory to supplement data trees with a type definition tree defining all structs used in any of the data trees.
The server shall verify the struct metadata when a struct is referenced.

The VISSv3.1 reference implementation supports all of the new features.

### VISSR tech stack
Also found on this repo are implementations of other components that are needed to realize a communication tech stack that reaches from clients through the server and to the underlying vehicle system interface.

![VISSR tech stack](/vissr/images/WAII-tech-stack.jpg?width=40pc)

These software components (SwCs) can be categorized as follows:
* server
* clients
* data storage
* feeders
* tools

The tutorial describes each SwC category in a separate chapter.
It also contains a few Proof of concept (POC) examples, and information about installing,
building and running Golang based SwCs, a Docker containerization, and about some peripheral components.
