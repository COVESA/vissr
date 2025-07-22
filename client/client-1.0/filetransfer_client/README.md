# File transfer client setup
The following must be prepared before the file transfer client can successfully be executed:
* A binary file format tree from a vehicle tree containing the nodes below must have been added to the vissr forest directory and added to the viss.him file
```
Vehicle.DownloadFile:
  type: actuator
  datatype: Types.Struct.FileDescriptor
  description: Download file configuration.

Vehicle.UploadFile:
  type: sensor
  datatype: Types.Struct.FileDescriptor
  description: Upload file configuration.
```
* A Typedefinition tree containing the following struct definitions must have been added to the vissr forest directory and added to the viss.him file
```
Types.Struct.FileDescriptor:
  type: struct
  description: File descriptor struct.

Types.Struct.FileDescriptor.Name:
  type: property
  datatype: string
  description: File descriptor name struct member.

Types.Struct.FileDescriptor.Hash:
  type: property
  datatype: string
  description: File descriptor hash struct member.

Types.Struct.FileDescriptor.Uid:
  type: property
  datatype: string
  description: File descriptor uid struct member.
```
* The dlFile.txt must be stored in the vissr/client/client-1.0/filetransfer_cient directory
* The upload.txt file must be stored in the vissr/server/vissv2server directory

The example files dlFile.txt and upload.txt can be replaced by other files.
For the upload scenario the vissv2server.go:getInternalFileName() method must be updated if the filename changes.


