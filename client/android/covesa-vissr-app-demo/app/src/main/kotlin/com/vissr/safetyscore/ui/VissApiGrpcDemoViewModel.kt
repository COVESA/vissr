package com.vissr.safetyscore.ui

import android.util.Log
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.vissr.safetyscore.domain.GetConnectionStatusUseCase
import com.vissr.safetyscore.domain.SubscribeRequestUseCase
import com.vissr.safetyscore.domain.model.VISSv2DataPackages
import com.vissr.safetyscore.domain.model.dataPointList
import com.vissr.safetyscore.ui.mapper.DomainViewDataPackageMapper
import com.vissr.safetyscore.ui.model.ViewDataPackage
import com.vissr.safetyscore.ui.model.ViewDataPoint
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.stateIn

class VissApiGrpcDemoViewModel(
    private val domainViewDataPackageMapper: DomainViewDataPackageMapper,
    private val subscribeRequestUseCase: SubscribeRequestUseCase,
    private val getConnectionStatusUseCase: GetConnectionStatusUseCase,
) : ViewModel(), DefaultLifecycleObserver {

    private val subscribeResponse: MutableStateFlow<VISSv2DataPackages?> = MutableStateFlow(null)

    private val _connectionStatus: MutableStateFlow<Boolean> = MutableStateFlow(false)
    val connectionStatus: StateFlow<Boolean> = _connectionStatus

    val subscribeResponseMap: StateFlow<Map<String, ViewDataPackage>> =
        subscribeResponse
            .filterNotNull()
            .map { it.toResponseMap() }
            .combine(initialResponseMapFlow()) { responseMap, initialMap ->
                initialMap + responseMap
            }
            .stateIn(
                viewModelScope,
                SharingStarted.WhileSubscribed(),
                getInitialResponseMap()
            )

    private fun initialResponseMapFlow() = flowOf(getInitialResponseMap())

    override fun onCreate(owner: LifecycleOwner) {
        super.onCreate(owner)
        Log.d("VissApiGrpcDemoViewModel", "onCreate: ")
        getConnectionStatus()
        subscribeRequest()
    }

    private fun subscribeRequest() {
        Log.d("VissApiGrpcDemoViewModel", "subscribeRequest: subscribeRequestUseCase called")
        subscribeRequestUseCase()
            .onEach {
                subscribeResponse.value = it
                //Log.d("CarApiGrpcDemoViewModel", "subscribeRequest: $it")
            }
            .launchIn(viewModelScope)
    }

    private fun getConnectionStatus() {
        getConnectionStatusUseCase()
            .onEach {
                Log.d("VissApiGrpcDemoViewModel", "getConnectionStatus: $it")
                _connectionStatus.value = it
            }
            .launchIn(viewModelScope)
    }

    private fun VISSv2DataPackages.toResponseMap(): Map<String, ViewDataPackage> =
        domainViewDataPackageMapper.map(this).let {
            it.dataPackage.associateBy { dataPackage ->
                dataPackage.path
            }
        }

    private fun getInitialResponseMap(): Map<String, ViewDataPackage> =
        dataPointList.associateWith {
            ViewDataPackage(
                path = "",
                name = it.toName(),
                dataPoints = listOf(INITIAL_DATA_POINT)
            )
        }

    private companion object {
        val INITIAL_DATA_POINT = ViewDataPoint(
            value = "NA",
            timestamp = ""
        )

        fun String.toName() =
            when(this) {
                "Speed" -> "SPEED"
                "CurrentLocation.Heading" -> "GPS DIRECTION"
                "CurrentLocation.Latitude" -> "GPS LATITUDE"
                "CurrentLocation.Longitude" -> "GPS LONGITUDE"
                "Chassis.SteeringWheel.Angle" -> "STEERING-WHEEL ANGLE"
                "Driver.IsHandsOnWheel" -> "HANDS ON WHEEL"
                "ADAS.ActiveAutonomyLevel" -> "AUTONOMY LEVEL"
                "ADAS.CruiseControl.IsActive" -> "ACC"
                "ADAS.LaneDepartureDetection.IsWarning" -> "LDW"
                "TraveledDistance" -> "ODOMETER"
                "Powertrain.Transmission.CurrentGear" -> "GEAR STATUS"
                "Cabin.Seat.Row1.DriverSide.IsBelted" -> "SEAT BELT STATUS"
                else -> ""
            }
    }
}
