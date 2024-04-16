# Integrating VISS towards Android service/application 

## What is this application or service enables?
This application/service operates within the native environment of Android Automotive OS (AAOS) and leverages COVESA VISS APIs, employing both gRPC and HTTP protocols, to securely access a predefined set of vehicle sensor datasets. These datasets empower the application to compute a safety score for the driver, aiding in real-time assessment of driving behavior.The primary aim of this service is not to merely excel as an application, but rather to address a prevalent industry challenge: how original equipment manufacturers (OEMs) can securely grant access to specific datasets—be it for reading, writing, or both—to their respective clients or consumers. This is achieved by adhering to Open Vehicle data standards, with VSS (Vehicle Signal Specification) serving as the vehicle data model, and VISS (Vehicle Interface Service Specification) acting as the service for accessing vehicle data.
Android Automotive serves as the standard infotainment system, offering a platform for seamless integration and utilization of these APIs. The purpose of the Android application is to consume these APIs and visualize the data for the audience, facilitating a comprehensive demonstration of the capabilities enabled by the integration of COVESA VISS APIs within the automotive ecosystem.

## How does this concept empowers Digital services for Automotive?
The value of vehicle data inherently increases with its usage, especially when accessible in an open environment without compromising security and privacy. This openness cultivates innovation for digital service providers operating within the automotive sector. Establishing a robust framework for vehicle data consent and privacy, coupled with the adoption of open Vehicle APIs like VISS, holds the potential to empower digital services in the automotive space.

## Why not use Android Automotive Car API/Sensor API which is standard for same purpose?
Purpose of this demo is not to entirely replace AAOS stack but try to address some of limitations that AAOS system have with vehicle data access which is limited to system applications.VISS/gRPC was also used towards Vehicle HAL that populate entire AAOS Vehicle properties 

Following guide provides step-by-step instructions on setting up containerized environment & integrating your Android application with COVESA/VISS gRPC APIs. 
By following these steps, you'll be able to leverage the functionalities provided by COVESA/VISS in your Android app seamlessly.

## Steps to setup containerized ecosystem environment

### Step 1: Configure RemotiveLabs virtual cloud endpoints towards feeder component
    Configure feeder to use RemotiveLabs virtual sensor cloud for Vehicle drive playback support
    https://github.com/COVESA/vissr/blob/master/feeder/feeder-rl/README.md
    Free access to cloud console : https://cloud.remotivelabs.com
   
### Step 2: Configure Security policy files for access control in Access token server
    Populate purpose list with VSS data points & access control
    https://github.com/COVESA/vissr/blob/master/server/vissv2server/atServer/purposelist.json

    Use following purpose 'ubi-sensor-status'

       {"purposes":
            [{"short": "ubi-sensor-status", 
            "long": "Sensor data for insurance provider to enable Usage Based Insurance premium", 
            "contexts":[ {"user":"Independent", "app":"OEM", "device":"Vehicle"}], 
            "signal_access":
                [{"path": "Vehicle.Speed", "access_permission": "read-only"}, 
                {"path": "Vehicle.CurrentLocation.Heading", "access_permission": "read-only"},
                {"path": "Vehicle.CurrentLocation.Latitude", "access_permission": "read-only"},
                {"path": "Vehicle.CurrentLocation.Longitude", "access_permission": "read-only"},
                {"path": "Vehicle.Chassis.SteeringWheel.Angle", "access_permission": "read-only"},
                {"path": "Vehicle.Driver.IsHandsOnWheel", "access_permission": "read-only"},
                {"path": "Vehicle.ADAS.ActiveAutonomyLevel", "access_permission": "read-only"},
                {"path": "Vehicle.ADAS.CruiseControl.IsActive", "access_permission": "read-only"},
                {"path": "Vehicle.ADAS.LaneDepartureDetection.IsWarning", "access_permission": "read-only"},
                {"path": "Vehicle.TraveledDistance", "access_permission": "read-only"},
                {"path": "Vehicle.Powertrain.Transmission.CurrentGear", "access_permission": "read-only"},
                {"path": "Vehicle.Cabin.Seat.Row1.DriverSide.IsBelted", "access_permission": "read-only"}] 
            }] 
        }

### Step 3: Enabled access-control tagging to each VSS nodes & Generate vss_vissv2.binary
    For VISS server to support access control, it is essential to tag VSS datapoints with access control mode with 'validate' attribute.
    Access control tagging at https://github.com/renjithrajagopal-sudo/vehicle_signal_specification/commit/ccd0475327b057f75fa4a796b9Should 67315bdb6620db
    After tagging vss_vissv2.binary shall be generated with tagged VSS by executing $make binary from https://github.com/renjithrajagopal-sudo/vehicle_signal_specification
    Generated vss_vissv2.binary to location https://github.com/COVESA/vissr/tree/master/server/vissv2server

### Step 4: Build docker for AGT/AT/VISS server
    Build AGT docker : https://github.com/COVESA/vissr/blob/master/docker/agt-docker/Readme.md
    Build VISS/AT docker : https://github.com/COVESA/vissr/blob/master/docker/README.md

### Step 5: Integrate VISS proto towards Android Application
    Download any Android Automotive OS Emulator. E.g Snapp Automotive Emulator available : https://github.com/snappautomotive/README
    Android application could leverage VISS protobuf files shared at https://github.com/COVESA/vissr/tree/master/grpc_pb
    Use following APIs
        1. Request for Access Grant Token(AGT) using HTTP POST request : https://github.com/COVESA/vissr/blob/master/server/agt_server/README.md
        2. Request for Access Token(AT) using HTTP POST request with AGT as input : https://github.com/COVESA/vissr/blob/master/server/vissv2server/atServer/README.md
        3. Subscribe for Multiple VSS datapoints with Access Token(AT) : https://www.w3.org/TR/viss2-core/#multiple-signals-request

## Steps to run programs

### Step 1 : Start RemotiveLabs vehicle sensor data drive recording
### Step 2 : Run AGT docker & VISS server docker in local host PC hosted at 127.0.0.1
### Step 3 : Starts Android Emulator & do reverse port forwarding via adb that establish TCP communication between Android device & local host PC
             $adb reverse tcp:8887 tcp:8887 -> VISS server
             $adb reverse tcp:7500 tcp:7500 -> AGT server
             $adb reverse tcp:8600 tcp:8600 -> AT server

### Step 4 : Run the application
### Step 5 : Hit PLAY button in recording
### Step 6 : Application shall get updates of VSS datapoints subscribed

