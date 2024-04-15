package com.vissr.safetyscore.data.repository

import android.util.Log
import com.vissr.safetyscore.data.datasource.remote.VissApiRemoteDataSource
import com.vissr.safetyscore.data.datasource.remote.RetrofitRemoteDataSource
import com.vissr.safetyscore.data.datasource.remote.retrofit.AGTRequest
import com.vissr.safetyscore.data.datasource.remote.retrofit.ATRequest
import com.vissr.safetyscore.data.mapper.RemoteToDomainDataPackageMapper
import com.vissr.safetyscore.domain.model.VISSv2DataPackages
import com.vissr.safetyscore.domain.repository.VissApiRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch

internal fun VissApiRepository.Factory.build(
    remoteDataSource: VissApiRemoteDataSource,
    retrofitRemoteDataSource: RetrofitRemoteDataSource,
    remoteToDomainDataPackageMapper: RemoteToDomainDataPackageMapper,
    coroutineScope: CoroutineScope
): VissApiRepository = VissApiRepositoryImpl(
    remoteDataSource = remoteDataSource,
    retrofitRemoteDataSource = retrofitRemoteDataSource,
    remoteToDomainDataPackageMapper = remoteToDomainDataPackageMapper,
    coroutineScope = coroutineScope
)

class VissApiRepositoryImpl(
    private val remoteDataSource: VissApiRemoteDataSource,
    private val retrofitRemoteDataSource: RetrofitRemoteDataSource,
    private val remoteToDomainDataPackageMapper: RemoteToDomainDataPackageMapper,
    private val coroutineScope: CoroutineScope,
) : VissApiRepository {

    private var response = MutableStateFlow<VISSv2DataPackages?>(null)

    private var isConnectionReady = MutableStateFlow<Boolean>(false)

    override fun subscribeRequest(): Flow<VISSv2DataPackages?> {
        coroutineScope.launch {
            // Request for AGT token
            sendAGTRequest().collect { agToken ->
                Log.d("CarApiRepositoryImpl", "sendAGTRequest: $agToken")
                // Valid AGT
                if (agToken.isNotEmpty()) {
                    // Request for Access Token
                    sendATRequest(agToken).collect { aToken ->
                        Log.d("CarApiRepositoryImpl", "sendATRequest: $aToken")
                        // Valid AT
                        if (aToken.isNotEmpty()) {
                            remoteDataSource.subscribeRequest(aToken).collect {
                                remoteToDomainDataPackageMapper
                                    .map(it)
                                    ?.let { dataPackages ->
                                        Log.d(
                                            "CarApiRepositoryImpl",
                                            "subscribeRequest: $dataPackages"
                                        )
                                        response.value = dataPackages
                                    }
                            }
                        } else {
                            Log.e("CarApiRepositoryImpl", "sendATRequest token empty")
                        }
                    }
                } else {
                    Log.e("CarApiRepositoryImpl", "sendAGTRequest token empty")
                }
            }
        }
        return response
    }

    override fun getConnectionStatus(): Flow<Boolean> {
        coroutineScope.launch {
            remoteDataSource.notifyConnectionStatus {
                Log.d("CarApiRepositoryImpl", "getConnectionStatus: true")
                isConnectionReady.value = true
            }
        }
        return isConnectionReady
    }

    override fun sendAGTRequest(): Flow<String> {
        // TODO: Add AGTRequest parameters
        val request = AGTRequest(
            action = "agt-request",
            vin = "GEO001",
            context = "Independent+OEM+Vehicle",
            proof = "ABC",
            key = "DEF"
        )
        return retrofitRemoteDataSource.sendAGTRequest(request)
    }

    override fun sendATRequest(agttoken:String): Flow<String> {
        // TODO: Add ATRequest parameters
        val request = ATRequest(
            action = "at-request",
            agToken = agttoken,
            purpose = "ubi-sensor-status",
            pop = ""
        )
        return retrofitRemoteDataSource.sendATRequest(request)
    }
}
