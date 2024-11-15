---
title: "VISSR binary tree configuration"
---

## Using the VSS project to generate the binary file
This requires that the [VSS](https://github.com/COVESA/vehicle_signal_specification) repo is cloned and configured, for th latter see instructions on the repo.
To generate the binary file the make file in the root directory of the repo is used,
which requires that a Python virtual environment is configured before it is used for the first time.
This is done by entering the VSS root directory, then issuing a command to configure the environment,
and then activating it, installing the vss-tools, and deactivate it.
```
$ cd vehicle_signal_specification
$ python3 -m venv ~/.venv
$ source ~/.venv/bin/activate
(.venv)$ pip install --pre vss-tools
(.venv)$ deactivate
```
The above is only needed to be done once.
It might be necessary to install both python and pip if that is not already installed on the computer, see instructions in the repo documentation.
If the exporter complains when used to make the binary file also after following the instructions above, then adding the command
```
(.venv)$ pip install -e .
```
may fix it. If problems prevail it is probably necessary to create an issue at the VSS repo.

To then generate the VSS tree binary file the environment is activated, the make file is called to generate the binary file,
and then the environment can be deactivated.
```
$ source ~/.venv/bin/activate
(.venv)$ make binary
(.venv)$ deactivate
```
This generates a file with a name like 'vss.binary',
which then needs to be possibly renamed to a more descriptive name and then copied to the vissr/server/vissv2server/forest directory.
It must also be added to the viss.him file in the same directory for the server to include it at startup.

## Using the CVIS project to generate the binary file
Another alternative for generating the binary file is to use the HIM configurator tool in the
[Commercial Vehicle Information Specifications](https://github.com/COVESA/commercial-vehicle-information-specifications) repo.
The CVIS project is aiming at creating signal trees tailored to the needs of other vehicle types than the passenger cars that the VSS tree is focusing on.
Development is ongoing for the vehicle types Truck, Trailer, and Bus, but the project is open for development initiatives for other vehicle types.
Following the patterns and rules described on the repo it is reasonably straight forward to create your own tree on your local computer.

The generation of a binary tree from the vspec files is here done by using the HIM configurator tool.
It uses the VSS-tools exporters for the final step of generating the files,
providing extended tree configuration options, see the [CVIS](https://covesa.github.io/commercial-vehicle-information-specifications/) documentation.
There it is also described how the same Python virtual environment as is used in the VSS alternative is configured (if not already done so in a VSS context)
and activated before using the HIM configurator.
ust as in the oher alternative the binary file needs to be copied to the vissr/server/vissv2server/forest directory,
and the viss.him file edited to include it.

## Tagging the tree for access control and consent management
If you want to configure the tree to include access control, access control tags as described in the
[VISSv2 - Access Control Selection chapter](https://raw.githack.com/covesa/vehicle-information-service-specification/main/spec/VISSv2_Core.html#access-control-selection) needs to be added to appropriate tree nodes.
This can either be done by editing vspec files directly (example below), or using the [VSS-Tools](https://github.com/covesa/vss-tools) overlay mechanism.

Adding read-write access control and consent to the entire VSS tree can be done by modifying the root node in the spec/VehicleSignalSpecification.vspec file as shown below.
If consent should not be included then the commented line should be used instead.
```
Vehicle:
  type: branch
  validate: read-write+consent
#  validate: read-write
  description: High-level vehicle data.
```
The above validate statement is inherited by all of the descendants of the node.
It can be applied to any node in the tree to allow for some nodes to not be access controlled while others will be access controlled.
Changing read-write to write-only leads to that the server will allow reading of the data without a token,
but requiring a valid token for write requests to the data.

If the HIM configurator in the CVIS project is used to generate the binary tree that has been tagged as described a binary tree with the tagging data will be generated.
In the case that it is the alternative using the VSS support that is used then it is necessary to also manually edit the make file to add '-e validate'
in the calls to the exporters. This should be added just before the output file name in the command, c. f. how it is added in the
[overlay example](https://covesa.github.io/vehicle_signal_specification/rule_set/overlay/index.html).

The AT server uses the purposelist.json file to validate that a client request to access controlled data is permitted by the access token included in the request.
It therefore necessary to ensure that this file contains purpose(s) that includes the data that is access controlled tagged in the tree.

## Using a modified make file
The make file in the [VSS](https://github.com/COVESA/vehicle_signal_specification) repo does not provide
CLI support for inserting validate tags or using struct datatypes that refer to a separate Type definition tree,
which leads to that the make file must be manually edited.
To avoid having to enter into such daring endeavours a modified make file can be found in the resource directory.
If this file replaces the existing VSS make file then it can be used as explained below when using the VSS as decribed above.
The CVIS project is currently not updated to use this file.

## Inserting 'validate' tags
Adding the CLI parameter NEWKEY=validate will instruct the binary exporter that it shall accept any lines with this keyword in the vspec files.
Before running the command below these lines must be manually edited into the desired nodes.
```
(.venv)$ make binary NEWKEY=validate
```

## Using the struct datatype
The [VSS rule set](https://covesa.github.io/vehicle_signal_specification/rule_set/data_entry/data_types_struct/index.html#general-idea-and-basic-semantics)
defines that struct datatypes shall be defined in a separate type definition tree that the 'datatype' key references, see example below.
```
DownloadFile:
  type: actuator
  datatype: Types.Resources.FileDescriptor
#  default: {"name":"downloadfile.txt", "hash":"20e87e71b6948d6e6dd11d776e9be79c374751bb", "uid":"1d878212")
  description: File to be used by the vehicle. Default contains internal filesystem path.
```
The type tree for the example above is found in the resources directory. 
If the type tree is stored in the root directory of the VSS tree, then the make file can be called with the command
below to generate binary representations of both trees.
```
(.venv)$ make binary TYPETREE=DataTypes.vspec
```
