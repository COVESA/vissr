**DOCKER**

**(C) 2023 Volvo Cars**<br>

The server can also be built and launched using docker and docker-compose please follow below links to install docker and docker-compose

https://docs.docker.com/install/linux/docker-ce/ubuntu/
https://docs.docker.com/compose/install/


**Build and run**

*viss-docker-rl*

The file docker-compose-rl.yml builds and runs  a variant of the feeder(feeder-rl, see Readme in feeder/feeder-rl for'
more details) - which is configured and built to interface the remotive labs cloud.
The Remotive cloud have recorded vehicle data which we can play back to a cloud version of their data broker. We have an
interface written in Go - https://github.com/petervolvowinz/viss-rl-interfaces -  that we have integrated into the VISSR feeder application. The docker compose version should be from 3.8.

The *docker-compose-rl.yml* is located in the docker/viss-docker-rl folder. The docker file *Dockerfile.rlserver* is located in the project root. 
Placing the dockerfile which is used to build the image in the root is done for
practical reasons. See: https://www.baeldung.com/ops/docker-include-files-outside-build-context


To build and run the docker example see below, please note that the docker-compose-rl.yml file is located in 
the: _viss-docker-rl_ folder



```bash
$ docker compose -f docker-compose-rl.yml build 
...
 => => exporting layers                                                                                                                                                                                              0.1s
 => => writing image sha256:f392918448ece4b26b5c430ffc53c5f2da8872d030c11a22b42360dbf9676934                                                                                                                         0.0s
 => => naming to docker.io/library/automotive-viss2-feeder  
```

```bash
$ docker compose -f docker-compose-rl.yml up
  ✔ Container container_volumes  Created                                                                                                                                                                              0.0s 
 ✔ Container vissv2server       Created                                                                                                                                                                              0.0s 
 ✔ Container app_redis          Created                                                                                                                                                                              0.0s 
 ✔ Container feeder             Recreated                                                                                                                                                                            0.1s 
Attaching to app_redis, container_volumes, feeder, vissv2server  
```
to stop use

```bash
$ docker-compose down
✔ Container vissv2server        Stopped                                                                                                                                                                              0.2s 
 ✔ Container app_redis          Stopped                                                                                                                                                                              0.2s 
 ✔ Container feeder             Stopped                                                                                                                                                                              0.1s 
 ✔ Container container_volumes  Stopped 
```

if server needs to be rebuilt due to src code modifications

```bash
$ docker-compose up -d --force-recreate --build

```

If you want to run the server with access control, we need to copy the access grant token server's public key and make
the key available in the container. These keys will be generated at the AGT server startup if not present.
If we are not using access control servers comment this row in the _vissv2server_ section of the Dockerfile in the project
root.

```
COPY --from=builder /build/server/agt_server/agt_public_key.rsa .
```

**Access control**

*agt-docker*

The access grant token server can be built a run in a separate docker container. The typical place for the agt server
would be in the cloud, but is not further specified. The agt server is, however, a prerequisite for the viss server to 
be able to run with access control. The *docker-compose-agt.yml* can be used to
build and run the agt server. The docker file *Dockerfile.agt* is also located in the project root.

```
$ docker compose -f docker-compose-agt.yml build
$ docker compose -f docker-compose-agt.yml up
```

