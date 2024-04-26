package com.vissr.safetyscore.domain.model

data class VISSv2DataPackages(
    val dataPackage: List<VISSv2DataPackage>
)

data class VISSv2DataPackage(
    val path: String,
    val dataPoints: List<VISSv2DataPoint>
)

data class VISSv2DataPoint(
    val value: String,
    val timestamp: String,
)

val dataPointList = listOf(
    "Speed",
    "CurrentLocation.Heading",
    "CurrentLocation.Latitude",
    "CurrentLocation.Longitude",
    "Chassis.SteeringWheel.Angle",
    "Driver.IsHandsOnWheel",
    "ADAS.ActiveAutonomyLevel",
    "ADAS.CruiseControl.IsActive",
    "ADAS.LaneDepartureDetection.IsWarning",
    "TraveledDistance",
    "Powertrain.Transmission.CurrentGear",
    "Cabin.Seat.Row1.DriverSide.IsBelted"
)
