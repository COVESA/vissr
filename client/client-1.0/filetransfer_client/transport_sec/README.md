The client can be configured to use the secure HTTPS and WSS transport protocols in its communication with the VISSv2 server. 
This is achieved by setting the "transportSec" parameter to "yes" in the transportSec.json file, 
and copying the CA cert and client cert and key from your local repo of https://github.com/MEAE-GOT/W3C_VehicleSignalInterfaceImpl, 
where they are supposedly previously generated. 
This shall be copied into directories named transport_sec/ca, and transport_sec/client, respectively, 
if the existing path parameters in the transportSec.json file are used. These can be updated to point to other directories.
For more information about the credentials generation, see https://github.com/MEAE-GOT/W3C_VehicleSignalInterfaceImpl/testCredGen/README.md.

