**(C) 2021 Geotab Inc**<br>

All files and artifacts in this repository are licensed under the provisions of the license provided by the LICENSE file in this repository.

# VISSv2 over MQTT
To realize this an application specific protocol is realized on top of MQTT, see the sequence diagram below.<br>
The VISSv2 server starts with subscribing to the topic "VINXXX/Vehicle", where VINXXX is an identity of the vehicle, a pseudo-VIN or the like. 
The MQTT cloud client that is about to issue a VISSv2 request start with subscribing to a topic which should be unique. It can be informative about the request, but also a random string.<br>
After the subscription, the cloud client publishes a JSON payload containing the unique topic, and the VISSv2 request, to the topic VINXXX/Vehicle. For this to work, the cloud client must know the vehicle identity, which it is supposed to have retrieved out-of-band from this communication.<br>
The broker then pushes this publication to the VISSv2 MQTT client, as it previously subscribed to this topic. This client then forwards the request part of the payload to the VISSv2 server, that then serves the request, and returns the response to the client. The client then publishes the response as the payload, to the topic it received in the payload from the broker, and finally the broker pushes this message to the cloud client that is a subscriber of this topic. The payload contains the VISSv2 response to the VISSv2 request that it pushed earlier.

![VISSv2 over MQTT sequence diagram](/pics/mqtt_vissv2_protocol.jpg?raw=true)<br>

The cloud client can repeat this sequence for further VISSv2 requests, using the same unique request topic. 

