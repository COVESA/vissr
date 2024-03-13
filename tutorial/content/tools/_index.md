---
title: "VISSR Tools"
---

SwCs categorized as tools are used "off-line" to create artefacts that can then be used by the "on-line" SwCs such as the server or by feeders.

## Domain Conversion Tool
The DCT creates the following:
* Two files used by a feeder for instructions on how to convert signals between the northbound domain and a southbound domain.
* A file containing a YAML representation of the northbound domain, to be usd as input to the VSS-Tools binary exporter.
The input to the DCT are three files:
* A YAML representation of signals of a northbound domain.
* A YAML representation of signals of a southbound domain.
* A YAML representation of pairs of signals from respective domain that the feeder should convert between.
For more information plese see the README in the [DCT](https://github.com/covesa/vissr/tree/master/tools/DomainConversionTool) directory.


## VSS-Tools Binary Exporter
The VSS-Tools binary exporter is one of the exporters of the [COVESA/VSS-Tools](https://github.com/COVESA/vss-tools) repo.

However, running the tool is easiest done from the [COVESA/VSS](https://github.com/COVESA/vehicle_signal_specification/tree/master) repo.
After cloning the repo (make sure the VSS-Tools is included as a submodule) the binary exporter is run by issuing the command:
```
make binary
```
in the root directory.

However, that will create a binary format representation of the VSS standard tree.
If the binary exporter is to take another data model as input the make file will need to be modified.
The make file line related to the binary exporter that points to the input file (the last line below):
```
binary:
	gcc -shared -o ${TOOLSDIR}/binary/binarytool.so -fPIC ${TOOLSDIR}/binary/binarytool.c
	${TOOLSDIR}/vspec2binary.py --uuid -u ./spec/units.yaml ./spec/VehicleSignalSpecification.vspec vss_rel_$$(cat VERSION).binary
```
needs to be modified to point to the desired file, i.e. the part "./spec/VehicleSignalSpecification.vspec" needs to be changed.

The tool output, a file with the extension ".binary" will then have to be copied to the VISSR server directory, and renamed to "vss_vissv2.binary".
