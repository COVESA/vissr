# Integrating Vehicle Information Service Specification(VISS) towards Android service/application 

## What is this application or service enables?
This application or service runs in a native Android Automotive OS(AAOS) & uses COVESA VISS APIs(gRPC & HTTP) to securly access set of vehicle sensor datasets that empowers
application to calculate safety score for driver.
Ambition here is not to make this service or application great, but mostly focusing on how could an OEM securely enable certain datasets(read or write or both) to respective client/consumer & 
restrict access of rest other datasets by client by using Open Vehicle data standards such as VSS(being Vehicle data model) & VISS(service to access vehicle data)
Android Automotive being a standard Infotainment system, purpose was to use Android application to consume these APis 

## How does this concept empowers Digital services for Automotive ?
Vehicle data generally gains value the more it is used & when it is open and if security & privacy factors not compromised, this fosters innovation for digital service providers
By having a well-defined Vehicle data consent & privacy framework along open Vehicle APIs like VISS could empower Digital services in automotive space

## Why not use Android Automotive Car API/Sensor API which is standard for same purpose ?
Purpose of this demo is not to entirely replace AAOS stack but try to address some of limitations that AAOS system have with vehicle data access which is limited to system applications
VISS/gRPC was also used towards Vehicle HAL that populate entire AAOS Vehicle properties 


Following guide provides step-by-step instructions on integrating your Android application with COVESA/VISS gRPC APIs. By following these steps, you'll be able to leverage the functionalities provided by COVESA/VISS in your Android app seamlessly.


## Steps to Integrate COVESA/VISS gRPC APIs into Your Android App
<TODO>

### Step 1: Configure RemotiveLabs virtual cloud endpoints towards feeder component
<TODO>

### Step 2: Configure Security policy files for access control in Access token server
<TODO>

### Step 3: Enabled access-control tagging to each VSS nodes & Generate vss_vissv2.binary
<TODO>

### Step 4: Integrate VISS proto towards Android Application
1. Use the protobuf files shared here : https://github.com/COVESA/vissr/tree/master/grpc_pb
2. Use HTTP APis to retrive token from AGT & AT server

