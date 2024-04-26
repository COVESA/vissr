package com.vissr.safetyscore.ui.mapper

import com.vissr.safetyscore.domain.model.VISSv2DataPackage
import com.vissr.safetyscore.domain.model.VISSv2DataPackages
import com.vissr.safetyscore.domain.model.VISSv2DataPoint
import com.vissr.safetyscore.ui.model.ViewDataPackage
import com.vissr.safetyscore.ui.model.ViewDataPackages
import com.vissr.safetyscore.ui.model.ViewDataPoint
import com.vissr.safetyscore.utils.mapper.Mapper

fun interface DomainViewDataPackageMapper :
    Mapper<VISSv2DataPackages, ViewDataPackages> {

    companion object Factory
}

internal fun DomainViewDataPackageMapper.Factory.build(): DomainViewDataPackageMapper =
    DomainViewDataPackageMapperImpl()

private class DomainViewDataPackageMapperImpl : DomainViewDataPackageMapper {

    override fun map(value: VISSv2DataPackages): ViewDataPackages =
        ViewDataPackages(
            dataPackage = value.dataPackage.map { it.toViewDataPackage() }
        )

    private fun VISSv2DataPackage.toViewDataPackage() =
        ViewDataPackage(
            path = path.toFormattedPath(),
            name = path.toName(),
            dataPoints = dataPoints.map { it.toViewDataPoint(path) }
        )

    private fun VISSv2DataPoint.toViewDataPoint(path: String) =
        ViewDataPoint(
            value = value.toFormattedDataPointValue(path),
            timestamp = timestamp
        )

    private fun String.toFormattedPath() = removePrefix("Vehicle.")

    private fun String.toName() =
        when(this) {
            "Vehicle.Speed" -> "SPEED"
            "Vehicle.CurrentLocation.Heading" -> "GPS DIRECTION"
            "Vehicle.CurrentLocation.Latitude" -> "GPS LATITUDE"
            "Vehicle.CurrentLocation.Longitude" -> "GPS LONGITUDE"
            "Vehicle.Driver.IsHandsOnWheel" -> "HANDS ON WHEEL"
            "Vehicle.Chassis.SteeringWheel.Angle" -> "STEERING-WHEEL ANGLE"
            "Vehicle.ADAS.ActiveAutonomyLevel" -> "AUTONOMY LEVEL"
            "Vehicle.ADAS.CruiseControl.IsActive" -> "ACC"
            "Vehicle.ADAS.LaneDepartureDetection.IsWarning" -> "LDW"
            "Vehicle.TraveledDistance" -> "ODOMETER"
            "Vehicle.Powertrain.Transmission.CurrentGear" -> "GEAR STATUS"
            "Vehicle.Cabin.Seat.Row1.DriverSide.IsBelted" -> "SEAT BELT STATUS"
            else -> ""
        }

    private fun String.toFormattedDataPointValue(path: String) =
        when(path) {
            "Vehicle.ADAS.CruiseControl.IsActive",
            "Vehicle.ADAS.LaneDepartureDetection.IsWarning" -> {
                when(this) {
                    "1" -> "ACTIVE"
                    else -> "INACTIVE"
                }
            }
            "Vehicle.Cabin.Seat.Row1.DriverSide.IsBelted" -> {
                if (this == "1") "BUCKLED" else "UNBUCKLED"
            }
            else -> this
        }
}

