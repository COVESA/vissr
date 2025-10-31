# External Vehicle Interface Client Simulator
This simulator is mainly meant to be used to verify that communication can be established with the EVIC feeder. It is hardcoded to simulate the signals defined in the variable canSignals,
which are the set of signals defined in the vissr/tools/DomainConversionTool/CAN-v0.1.yaml file. 
To use it with other Vehicle domain signals, the array elements of this variable has to be changed before building the simulator.
When started, the simulator tries to connect to the EVIC feeder 15 times sleeping 2 secs in between. If unsuccesful it terminates.
The values it assigns to the randomly selected signals are one of the integer values zero or one.

